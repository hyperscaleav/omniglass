package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Replay is a recorded model: it plays its turns in order, whatever the
// conversation, numbering every call it makes. It stands in for a live model
// wherever a run must be the same every time.
type Replay struct {
	turns []Turn
	next  int
	calls int
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

// ParseReplay reads a recording: a list of turns, each text, calls, or both.
func ParseReplay(b []byte) (*Replay, error) {
	var doc struct {
		Turns []recordedTurn `yaml:"turns"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("the recording: %w", err)
	}
	if len(doc.Turns) == 0 {
		return nil, errors.New("the recording has no turns")
	}
	r := &Replay{}
	for i, rt := range doc.Turns {
		if rt.Text == "" && len(rt.ToolCalls) == 0 {
			return nil, fmt.Errorf("the recording's turn %d has neither text nor tool calls", i+1)
		}
		turn := Turn{Text: rt.Text}
		for _, c := range rt.ToolCalls {
			if c.Name == "" {
				return nil, fmt.Errorf("the recording's turn %d calls a tool with no name", i+1)
			}
			turn.ToolCalls = append(turn.ToolCalls, ToolCall{Name: c.Name, Arguments: c.Arguments})
		}
		r.turns = append(r.turns, turn)
	}
	return r, nil
}

// Next plays the next recorded turn.
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
// how often it was asked: the turn after the model turns the conversation
// already holds, its calls numbered after the calls already made. It keeps
// no state, so the server can play one recording to every run, and a run
// picked up from a stored conversation plays on from there (#2121).
func (r *Replay) Positioned() Provider { return positioned{turns: r.turns} }

type positioned struct{ turns []Turn }

func (p positioned) Next(_ context.Context, messages []Message, _ []Tool) (Turn, error) {
	at, calls := 0, 0
	for _, m := range messages {
		if m.Role == RoleAssistant {
			at++
			calls += len(m.ToolCalls)
		}
	}
	if at >= len(p.turns) {
		return Turn{}, fmt.Errorf("the recording ran out after %d turns", len(p.turns))
	}
	t := p.turns[at]
	out := Turn{Text: t.Text, ToolCalls: make([]ToolCall, len(t.ToolCalls))}
	for i, c := range t.ToolCalls {
		calls++
		out.ToolCalls[i] = ToolCall{ID: "call-" + strconv.Itoa(calls), Name: c.Name, Arguments: c.Arguments}
	}
	return out, nil
}
