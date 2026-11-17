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
	"errors"
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
	// DecisionWaiting: the call is an ask and the run paused for its owner.
	DecisionWaiting Decision = "waiting"
)

// Call is one call the model asked for and what became of it.
type Call struct {
	Tool     string
	Decision Decision
	IsError  bool
	Text     string
}

// EventKind is which step of a run an Event reports.
type EventKind string

const (
	// EventText: the model's text for a turn.
	EventText EventKind = "text"
	// EventProposed: the model asked for a call (CallID, Tool, Arguments,
	// Policy), before anything is decided about it.
	EventProposed EventKind = "proposed"
	// EventDecided: what the loop decided for the call (Decision).
	EventDecided EventKind = "decided"
	// EventResult: what the model is told about the call (Text, IsError).
	EventResult EventKind = "result"
)

// Event is one step of a run, reported as it happens. A call's events carry
// the provider's tool-call id, so a client can tie its proposal, decision and
// result together. Policy is the tool's agent policy, empty for a tool the
// model was not offered.
type Event struct {
	Kind      EventKind
	Text      string
	CallID    string
	Tool      string
	Arguments map[string]any
	Policy    string
	Decision  Decision
	IsError   bool
}

// Pending is the ask a paused run waits on: the call by its provider id, as
// it would be proposed to the person.
type Pending struct {
	CallID string
	Proposal
}

// Outcome is where a run got to: the model's last text, every call this run
// answered, the conversation so far, and whether it was stopped at the step
// ceiling, finished on its own, or paused at an ask (Pending).
type Outcome struct {
	Text     string
	Steps    int
	Stopped  bool
	Calls    []Call
	Pending  *Pending
	Messages []provider.Message
}

// Loop runs one prompt to its answer.
type Loop struct {
	Provider provider.Provider
	Tools    Tools
	// Confirm is asked before an ask call; nil declines every ask.
	Confirm Confirmer
	// Pause, when set, stops the run at an ask instead of calling Confirm:
	// Run returns the pending call, and Continue resumes from the returned
	// conversation once its owner decides. Whoever holds the conversation
	// decides where it waits.
	Pause bool
	// MaxSteps bounds the model turns; zero is defaultMaxSteps.
	MaxSteps int
	// Events, when set, hears each step of the run as it happens.
	Events func(Event)
}

// Run sends prompt to the model and answers its calls until it replies with
// no calls, the step ceiling is reached, or (with Pause) a call waits on its
// owner. A call the transport cannot make ends the run with its error: the
// session is gone, not the tool refused.
func (l *Loop) Run(ctx context.Context, prompt string) (Outcome, error) {
	return l.drive(ctx, []provider.Message{{Role: provider.RoleUser, Text: prompt}}, nil)
}

// Continue resumes a paused run from its conversation (an Outcome's
// Messages) with its owner's decision on the pending call: approved runs it,
// otherwise it is declined and does not run. The rest of that turn's calls
// are answered in order, then the run goes on as Run does. history is not
// written to.
func (l *Loop) Continue(ctx context.Context, history []provider.Message, approved bool) (Outcome, error) {
	if _, ok := firstOpen(history); !ok {
		return Outcome{}, errors.New("continue: no call is waiting in this conversation")
	}
	return l.drive(ctx, append([]provider.Message(nil), history...), &approved)
}

// drive runs the conversation in msgs forward. A turn whose calls are not all
// answered is finished first, its first open call decided by decided when
// that is set.
func (l *Loop) drive(ctx context.Context, msgs []provider.Message, decided *bool) (Outcome, error) {
	listed, err := l.Tools.List(ctx)
	if err != nil {
		return Outcome{}, fmt.Errorf("list the tools: %w", err)
	}
	byName, offer := offerTools(listed)
	max := l.MaxSteps
	if max <= 0 {
		max = defaultMaxSteps
	}
	var out Outcome
	for _, m := range msgs {
		if m.Role == provider.RoleAssistant {
			out.Steps++
		}
	}
	for {
		if msgs, err = l.answerTurn(ctx, msgs, byName, &out, decided); err != nil || out.Pending != nil {
			out.Messages = msgs
			return out, err
		}
		decided = nil
		if out.Steps >= max {
			break
		}
		turn, err := l.Provider.Next(ctx, msgs, offer)
		if err != nil {
			out.Messages = msgs
			return out, fmt.Errorf("the model: %w", err)
		}
		out.Steps++
		msgs = append(msgs, provider.Message{Role: provider.RoleAssistant, Text: turn.Text, ToolCalls: turn.ToolCalls})
		if turn.Text != "" {
			l.emit(Event{Kind: EventText, Text: turn.Text})
		}
		if len(turn.ToolCalls) == 0 {
			out.Text = turn.Text
			out.Messages = msgs
			return out, nil
		}
	}
	out.Stopped = true
	out.Text = fmt.Sprintf("Stopped after %d steps without an answer.", max)
	out.Messages = msgs
	return out, nil
}

// answerTurn answers the open calls of the conversation's last turn in order.
// The first is decided by decided when that is set (it was proposed before
// the run paused); with Pause, an ask stops the turn and sets out.Pending.
func (l *Loop) answerTurn(ctx context.Context, msgs []provider.Message, byName map[string]*sdk.Tool, out *Outcome, decided *bool) ([]provider.Message, error) {
	for {
		i, ok := firstOpen(msgs)
		if !ok {
			return msgs, nil
		}
		tc := msgs[len(msgs)-1-countToolResults(msgs)].ToolCalls[i]
		t := byName[tc.Name]
		confirm := l.Confirm
		if decided != nil {
			yes := *decided
			confirm = func(context.Context, Proposal) (bool, error) { return yes, nil }
			decided = nil
		} else {
			l.emit(Event{Kind: EventProposed, CallID: tc.ID, Tool: tc.Name, Arguments: tc.Arguments, Policy: policyName(t)})
			if l.Pause && t != nil && policyOf(t) != "always" {
				l.emit(Event{Kind: EventDecided, CallID: tc.ID, Tool: tc.Name, Decision: DecisionWaiting})
				out.Pending = &Pending{CallID: tc.ID, Proposal: proposal(t, tc)}
				return msgs, nil
			}
		}
		call, err := l.answer(ctx, t, tc, confirm)
		if err != nil {
			return msgs, err
		}
		out.Calls = append(out.Calls, call)
		l.emit(Event{Kind: EventDecided, CallID: tc.ID, Tool: call.Tool, Decision: call.Decision})
		l.emit(Event{Kind: EventResult, CallID: tc.ID, Tool: call.Tool, Decision: call.Decision, IsError: call.IsError, Text: call.Text})
		msgs = append(msgs, provider.Message{Role: provider.RoleTool, ToolCallID: tc.ID, Text: call.Text, IsError: call.IsError})
	}
}

// firstOpen is the index, in the conversation's last model turn, of its first
// call with no result yet. The loop answers a turn's calls in order, so the
// results that follow the turn are its first calls'.
func firstOpen(msgs []provider.Message) (int, bool) {
	n := countToolResults(msgs)
	at := len(msgs) - 1 - n
	if at < 0 || msgs[at].Role != provider.RoleAssistant || n >= len(msgs[at].ToolCalls) {
		return 0, false
	}
	return n, true
}

// countToolResults is how many tool results end the conversation.
func countToolResults(msgs []provider.Message) int {
	n := 0
	for i := len(msgs) - 1; i >= 0 && msgs[i].Role == provider.RoleTool; i-- {
		n++
	}
	return n
}

func (l *Loop) emit(e Event) {
	if l.Events != nil {
		l.Events(e)
	}
}

// answer decides one call from its tool's policy, asking confirm for an ask,
// and makes it if it may run.
func (l *Loop) answer(ctx context.Context, t *sdk.Tool, tc provider.ToolCall, confirm Confirmer) (Call, error) {
	if t == nil {
		return Call{Tool: tc.Name, Decision: DecisionRefused, IsError: true, Text: "the tool " + tc.Name + " is not offered"}, nil
	}
	if policyOf(t) != "always" {
		yes := false
		if confirm != nil {
			var err error
			if yes, err = confirm(ctx, proposal(t, tc)); err != nil {
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

// proposal is a call as it is put to the person.
func proposal(t *sdk.Tool, tc provider.ToolCall) Proposal {
	return Proposal{Tool: t.Name, Title: t.Title, Arguments: tc.Arguments, Destructive: destructive(t)}
}

// policyName is an offered tool's policy, and empty for one not offered.
func policyName(t *sdk.Tool) string {
	if t == nil {
		return ""
	}
	return policyOf(t)
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
