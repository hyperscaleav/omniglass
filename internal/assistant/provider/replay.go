package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Replay is a recorded model: it plays its turns in order, whatever the
// conversation, numbering every call it makes. It stands in for a live model
// wherever a run must be the same every time. A turn may record how long the
// model thought before it, so a capture of the console paces like live
// inference instead of answering at once.
type Replay struct {
	turns         []recorded
	conversations []conversation
	next          int
	calls         int
}

// conversation is a recorded script played instead of the default turns when
// the person's latest message holds its match (#2163).
type conversation struct {
	match string
	turns []recorded
}

// recorded is a turn and the pause the model takes before it.
type recorded struct {
	turn  Turn
	think time.Duration
}

// MaxThink is the longest pause a recorded turn may take.
const MaxThink = 30 * time.Second

type recordedTurn struct {
	// Think is how long the model works before this turn arrives, as a Go
	// duration ("1.8s"); none answers at once.
	Think string `yaml:"think"`
	// Reasoning is the model's chain of thought for the turn, which the
	// console shows as its thinking.
	Reasoning string `yaml:"reasoning"`
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
// and optionally conversations, each a match and its own turns.
func ParseReplay(b []byte) (*Replay, error) {
	var doc struct {
		Turns         []recordedTurn `yaml:"turns"`
		Conversations []struct {
			Match string         `yaml:"match"`
			Turns []recordedTurn `yaml:"turns"`
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
	r := &Replay{turns: turns}
	for i, c := range doc.Conversations {
		what := fmt.Sprintf("the recording's conversation %d", i+1)
		if c.Match == "" {
			return nil, fmt.Errorf("%s has no match", what)
		}
		if len(c.Turns) == 0 {
			return nil, fmt.Errorf("%s has no turns", what)
		}
		ts, err := parseTurns(c.Turns, what)
		if err != nil {
			return nil, err
		}
		r.conversations = append(r.conversations, conversation{match: c.Match, turns: ts})
	}
	return r, nil
}

func parseTurns(in []recordedTurn, what string) ([]recorded, error) {
	out := make([]recorded, 0, len(in))
	for i, rt := range in {
		if rt.Text == "" && len(rt.ToolCalls) == 0 {
			return nil, fmt.Errorf("%s's turn %d has neither text nor tool calls", what, i+1)
		}
		var think time.Duration
		if rt.Think != "" {
			d, err := time.ParseDuration(rt.Think)
			if err != nil || d < 0 || d > MaxThink {
				return nil, fmt.Errorf("%s's turn %d: think %q is not a duration from 0 to %s", what, i+1, rt.Think, MaxThink)
			}
			think = d
		}
		turn := Turn{Text: rt.Text, Reasoning: rt.Reasoning}
		for _, c := range rt.ToolCalls {
			if c.Name == "" {
				return nil, fmt.Errorf("%s's turn %d calls a tool with no name", what, i+1)
			}
			turn.ToolCalls = append(turn.ToolCalls, ToolCall{Name: c.Name, Arguments: c.Arguments})
		}
		out = append(out, recorded{turn: turn, think: think})
	}
	return out, nil
}

// pause waits out a turn's thinking, or until the run is cancelled.
func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Next plays the next of the default turns; the conversations are played
// only by Positioned, which reads the person's message to choose one.
func (r *Replay) Next(ctx context.Context, _ []Message, _ []Tool) (Turn, error) {
	if r.next >= len(r.turns) {
		return Turn{}, fmt.Errorf("the recording ran out after %d turns", len(r.turns))
	}
	rec := r.turns[r.next]
	r.next++
	if err := pause(ctx, rec.think); err != nil {
		return Turn{}, err
	}
	t := rec.turn
	out := Turn{Text: t.Text, Reasoning: t.Reasoning, ToolCalls: make([]ToolCall, len(t.ToolCalls))}
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
	return positioned{turns: r.turns, conversations: r.conversations}
}

type positioned struct {
	turns         []recorded
	conversations []conversation
}

func (p positioned) Next(ctx context.Context, messages []Message, _ []Tool) (Turn, error) {
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
	turns := p.turns
	for _, c := range p.conversations {
		if strings.Contains(latest, c.match) {
			turns = c.turns
			break
		}
	}
	if at >= len(turns) {
		return Turn{}, fmt.Errorf("the recording ran out after %d turns", len(turns))
	}
	rec := turns[at]
	if err := pause(ctx, rec.think); err != nil {
		return Turn{}, err
	}
	t := rec.turn
	out := Turn{Text: t.Text, Reasoning: t.Reasoning, ToolCalls: make([]ToolCall, len(t.ToolCalls))}
	for i, c := range t.ToolCalls {
		calls++
		out.ToolCalls[i] = ToolCall{ID: "call-" + strconv.Itoa(calls), Name: c.Name, Arguments: c.Arguments}
	}
	return out, nil
}
