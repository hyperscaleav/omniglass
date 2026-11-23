package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Replay is a recorded model: it plays its turns in order, whatever the
// conversation, numbering every call it makes. It stands in for a live model
// wherever a run must be the same every time.
type Replay struct {
	turns         []Turn
	prompt        string
	conversations []conversation
	next          int
	calls         int
}

// conversation is a recorded script played instead of the default turns when
// the person's latest message holds its match (#2163).
type conversation struct {
	match  string
	prompt string
	turns  []Turn
}

// Scripted is a recorded model that knows which messages it answers (#2443):
// the prompts it was recorded for, to offer, and whether a message is one it
// plays, so a server can refuse the rest instead of playing an unrelated run.
type Scripted interface {
	// Prompts are the questions the recording answers, in recorded order.
	Prompts() []string
	// Plays is whether the recording has a script for the message.
	Plays(message string) bool
}

type recordedTurn struct {
	Text      string `yaml:"text"`
	ToolCalls []struct {
		Name      string         `yaml:"name"`
		Arguments map[string]any `yaml:"arguments"`
	} `yaml:"tool_calls"`
}

// LoadReplay reads a recording from a YAML (or JSON) file.
func LoadReplay(path string) (*Replay, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the recording: %w", err)
	}
	return ParseReplay(b)
}

// ParseReplay reads a recording: a list of turns, each text, calls, or both,
// and optionally conversations, each a match (what a message must hold) or a
// prompt (the question it answers, offered to the person) and its own turns.
// A recording whose top-level turns name their prompt answers only the
// prompts it names; without one, its turns answer any other message (#2443).
func ParseReplay(b []byte) (*Replay, error) {
	var doc struct {
		Prompt        string         `yaml:"prompt"`
		Turns         []recordedTurn `yaml:"turns"`
		Conversations []struct {
			Match  string         `yaml:"match"`
			Prompt string         `yaml:"prompt"`
			Turns  []recordedTurn `yaml:"turns"`
		} `yaml:"conversations"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("the recording: %w", err)
	}
	if len(doc.Turns) == 0 {
		return nil, errors.New("the recording has no turns")
	}
	turns, err := parseTurns(doc.Turns, "the recording")
	if err != nil {
		return nil, err
	}
	r := &Replay{turns: turns, prompt: strings.TrimSpace(doc.Prompt)}
	for i, c := range doc.Conversations {
		what := fmt.Sprintf("the recording's conversation %d", i+1)
		if c.Match == "" && strings.TrimSpace(c.Prompt) == "" {
			return nil, fmt.Errorf("%s has no match or prompt", what)
		}
		if len(c.Turns) == 0 {
			return nil, fmt.Errorf("%s has no turns", what)
		}
		ts, err := parseTurns(c.Turns, what)
		if err != nil {
			return nil, err
		}
		r.conversations = append(r.conversations, conversation{match: c.Match, prompt: strings.TrimSpace(c.Prompt), turns: ts})
	}
	return r, nil
}

func parseTurns(in []recordedTurn, what string) ([]Turn, error) {
	out := make([]Turn, 0, len(in))
	for i, rt := range in {
		if rt.Text == "" && len(rt.ToolCalls) == 0 {
			return nil, fmt.Errorf("%s's turn %d has neither text nor tool calls", what, i+1)
		}
		turn := Turn{Text: rt.Text}
		for _, c := range rt.ToolCalls {
			if c.Name == "" {
				return nil, fmt.Errorf("%s's turn %d calls a tool with no name", what, i+1)
			}
			turn.ToolCalls = append(turn.ToolCalls, ToolCall{Name: c.Name, Arguments: c.Arguments})
		}
		out = append(out, turn)
	}
	return out, nil
}

// Next plays the next of the default turns; the conversations are played
// only by Positioned, which reads the person's message to choose one.
func (r *Replay) Next(_ context.Context, _ []Message, _ []Tool) (Turn, error) {
	if r.next >= len(r.turns) {
		return Turn{}, fmt.Errorf("the recording ran out after %d turns", len(r.turns))
	}
	t := r.turns[r.next]
	r.next++
	out := Turn{Text: t.Text, ToolCalls: make([]ToolCall, len(t.ToolCalls))}
	for i, c := range t.ToolCalls {
		r.calls++
		out.ToolCalls[i] = ToolCall{ID: "call-" + strconv.Itoa(r.calls), Name: c.Name, Arguments: c.Arguments}
	}
	return out, nil
}

// Positioned is the recording played by where the conversation is, not by
// how often it was asked. The person's latest message chooses the script: the
// first conversation whose match it holds, else the default turns. The turn
// played is the one after the model turns since that message, its calls
// numbered after every call the conversation already made. It keeps no state,
// so the server can play one recording to every run, and a run picked up from
// a stored conversation plays on from there (#2121, #2163).
func (r *Replay) Positioned() Provider {
	return positioned{turns: r.turns, prompt: r.prompt, conversations: r.conversations}
}

type positioned struct {
	turns         []Turn
	prompt        string
	conversations []conversation
}

// normal is a message as a prompt is compared: lowercase, its words spaced
// singly, without the punctuation that ends a question.
func normal(s string) string {
	return strings.TrimRight(strings.Join(strings.Fields(strings.ToLower(s)), " "), "?.! ")
}

func (c conversation) holds(message string) bool {
	if c.prompt != "" && strings.Contains(normal(message), normal(c.prompt)) {
		return true
	}
	return c.match != "" && strings.Contains(message, c.match)
}

// script is the turns the message is played, and whether any are.
func (p positioned) script(message string) ([]Turn, bool) {
	for _, c := range p.conversations {
		if c.holds(message) {
			return c.turns, true
		}
	}
	if p.prompt == "" || strings.Contains(normal(message), normal(p.prompt)) {
		return p.turns, true
	}
	return nil, false
}

func (p positioned) Prompts() []string {
	var out []string
	if p.prompt != "" {
		out = append(out, p.prompt)
	}
	for _, c := range p.conversations {
		if c.prompt != "" {
			out = append(out, c.prompt)
		}
	}
	return out
}

func (p positioned) Plays(message string) bool {
	_, ok := p.script(message)
	return ok
}

func (p positioned) Next(_ context.Context, messages []Message, _ []Tool) (Turn, error) {
	at, calls, latest := 0, 0, ""
	for _, m := range messages {
		switch m.Role {
		case RoleUser:
			at, latest = 0, m.Text
		case RoleAssistant:
			at++
			calls += len(m.ToolCalls)
		}
	}
	turns, ok := p.script(latest)
	if !ok {
		return Turn{}, errors.New("the recording has no answer to that message")
	}
	if at >= len(turns) {
		return Turn{}, fmt.Errorf("the recording ran out after %d turns", len(turns))
	}
	t := turns[at]
	out := Turn{Text: t.Text, ToolCalls: make([]ToolCall, len(t.ToolCalls))}
	for i, c := range t.ToolCalls {
		calls++
		out.ToolCalls[i] = ToolCall{ID: "call-" + strconv.Itoa(calls), Name: c.Name, Arguments: c.Arguments}
	}
	return out, nil
}
