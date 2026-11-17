// Package assistant is omnibot (#1166): the built-in assistant's loop. It
// offers a model the omniglass MCP tools, runs the calls the model asks for
// through the MCP server with an omnibot agent token, and confirms each one
// on the client from the tool's resolved agent policy: always runs, ask runs
// once the person says yes, never is not offered. Every call is the server's
// to authorize and audit; the loop decides only what to ask the person.
package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
)

// defaultMaxSteps bounds a run's model turns when the loop names none.
const defaultMaxSteps = 10

// agentPolicyMeta is the _meta key the MCP server marks each listed tool's
// agent policy under (internal/mcp/policy.go).
const agentPolicyMeta = "omniglass/agent_policy"

// Tools is the MCP server, as the loop sees it: the tools it lists and a call.
type Tools interface {
	List(ctx context.Context) ([]*sdk.Tool, error)
	Call(ctx context.Context, name string, args map[string]any) (*sdk.CallToolResult, error)
}

// Proposal is a call the loop asks the person to confirm before it runs.
type Proposal struct {
	Tool        string
	Title       string
	Arguments   map[string]any
	Destructive bool
}

// Confirmer asks the person whether a proposed call may run.
type Confirmer func(ctx context.Context, p Proposal) (bool, error)

// Decision is what became of a call the model asked for.
type Decision string

const (
	// DecisionCalled: the call ran (its result may still be an error).
	DecisionCalled Decision = "called"
	// DecisionDeclined: the person said no, or there was no one to ask.
	DecisionDeclined Decision = "declined"
	// DecisionRefused: the tool was not offered (never, or not served).
	DecisionRefused Decision = "refused"
)

// Call is one call the model asked for and what became of it.
type Call struct {
	Tool     string
	Decision Decision
	IsError  bool
	Text     string
}

// Outcome is a finished run: the model's last text, every call, and whether
// the run was stopped at the step ceiling rather than ending on its own.
type Outcome struct {
	Text    string
	Steps   int
	Stopped bool
	Calls   []Call
}

// Loop runs one prompt to its answer.
type Loop struct {
	Provider provider.Provider
	Tools    Tools
	// Confirm is asked before an ask call; nil declines every ask.
	Confirm Confirmer
	// MaxSteps bounds the model turns; zero is defaultMaxSteps.
	MaxSteps int
	// Observe, when set, sees each call as it is decided.
	Observe func(Call)

	// history is the conversation so far, so each Run continues the last.
	history []provider.Message
}

// Tell adds a note from the person to the conversation without asking the
// model anything; the model reads it with the next prompt. A credential's
// reference reaches the model this way, never its value.
func (l *Loop) Tell(text string) {
	l.history = append(l.history, provider.Message{Role: provider.RoleUser, Text: text})
}

// Run sends prompt to the model and answers its calls until it replies with
// no calls or the step ceiling is reached. Each Run continues the
// conversation the last one left, so a chat is a Run per prompt. A call the transport cannot make
// ends the run with its error: the session is gone, not the tool refused.
func (l *Loop) Run(ctx context.Context, prompt string) (Outcome, error) {
	listed, err := l.Tools.List(ctx)
	if err != nil {
		return Outcome{}, fmt.Errorf("list the tools: %w", err)
	}
	byName, offer := offerTools(listed)
	max := l.MaxSteps
	if max <= 0 {
		max = defaultMaxSteps
	}
	l.history = append(l.history, provider.Message{Role: provider.RoleUser, Text: prompt})
	var out Outcome
	for out.Steps < max {
		turn, err := l.Provider.Next(ctx, l.history, offer)
		if err != nil {
			return out, fmt.Errorf("the model: %w", err)
		}
		out.Steps++
		l.history = append(l.history, provider.Message{Role: provider.RoleAssistant, Text: turn.Text, ToolCalls: turn.ToolCalls})
		if len(turn.ToolCalls) == 0 {
			out.Text = turn.Text
			return out, nil
		}
		for _, tc := range turn.ToolCalls {
			call, err := l.answer(ctx, byName[tc.Name], tc)
			if err != nil {
				return out, err
			}
			out.Calls = append(out.Calls, call)
			if l.Observe != nil {
				l.Observe(call)
			}
			l.history = append(l.history, provider.Message{Role: provider.RoleTool, ToolCallID: tc.ID, Text: call.Text, IsError: call.IsError})
		}
	}
	out.Stopped = true
	out.Text = fmt.Sprintf("Stopped after %d steps without an answer.", max)
	return out, nil
}

// answer decides one call from its tool's policy and makes it if it may run.
func (l *Loop) answer(ctx context.Context, t *sdk.Tool, tc provider.ToolCall) (Call, error) {
	if t == nil {
		return Call{Tool: tc.Name, Decision: DecisionRefused, IsError: true, Text: "the tool " + tc.Name + " is not offered"}, nil
	}
	if policyOf(t) != "always" {
		yes := false
		if l.Confirm != nil {
			var err error
			if yes, err = l.Confirm(ctx, Proposal{Tool: t.Name, Title: t.Title, Arguments: tc.Arguments, Destructive: destructive(t)}); err != nil {
				return Call{}, fmt.Errorf("confirm %s: %w", t.Name, err)
			}
		}
		if !yes {
			return Call{Tool: t.Name, Decision: DecisionDeclined, IsError: true, Text: "the person declined this call; it did not run"}, nil
		}
	}
	res, err := l.Tools.Call(ctx, t.Name, tc.Arguments)
	if err != nil {
		return Call{}, fmt.Errorf("call %s: %w", t.Name, err)
	}
	return Call{Tool: t.Name, Decision: DecisionCalled, IsError: res.IsError, Text: resultText(res)}, nil
}

// offerTools is the listed tools a model may be offered, by name and in the
// provider's shape: every tool but a never one.
func offerTools(listed []*sdk.Tool) (map[string]*sdk.Tool, []provider.Tool) {
	byName := make(map[string]*sdk.Tool, len(listed))
	var offer []provider.Tool
	for _, t := range listed {
		if policyOf(t) == "never" {
			continue
		}
		byName[t.Name] = t
		desc := t.Description
		if desc == "" {
			desc = t.Title
		}
		offer = append(offer, provider.Tool{Name: t.Name, Description: desc, InputSchema: t.InputSchema})
	}
	return byName, offer
}

// policyOf is a listed tool's resolved agent policy; a tool the server did
// not mark is ask, so a policy the client cannot read never runs unasked.
func policyOf(t *sdk.Tool) string {
	var p string
	switch m := t.Meta[agentPolicyMeta].(type) {
	case map[string]any:
		p, _ = m["policy"].(string)
	case map[string]string:
		p = m["policy"]
	}
	if p == "always" || p == "never" {
		return p
	}
	return "ask"
}

// destructive reads the tool's hint; an unannotated tool may destroy.
func destructive(t *sdk.Tool) bool {
	return t.Annotations == nil || t.Annotations.DestructiveHint == nil || *t.Annotations.DestructiveHint
}

// resultText is a tool result as the model reads it: its text, else its
// structured content as JSON.
func resultText(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if b.Len() == 0 && res.StructuredContent != nil {
		j, _ := json.Marshal(res.StructuredContent)
		return string(j)
	}
	return b.String()
}
