package provider

import (
	"context"
	"strings"
	"testing"
)

const recording = `
turns:
  - tool_calls:
      - name: list-components
      - name: create-component
        arguments: {name: lobby-display, product: generic-device}
  - text: Created lobby-display.
`

// A replay plays its recorded turns in order, numbering every call, whatever
// the conversation so far, and says so when the recording runs out.
func TestReplayPlaysTurnsInOrder(t *testing.T) {
	t.Parallel()
	r, err := ParseReplay([]byte(recording))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := r.Next(ctx, []Message{{Role: RoleUser, Text: "add a display"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 2 || first.Text != "" {
		t.Fatalf("first turn = %+v, want two calls", first)
	}
	if c := first.ToolCalls[0]; c.ID != "call-1" || c.Name != "list-components" || len(c.Arguments) != 0 {
		t.Errorf("first call = %+v", c)
	}
	if c := first.ToolCalls[1]; c.ID != "call-2" || c.Arguments["name"] != "lobby-display" {
		t.Errorf("second call = %+v", c)
	}
	second, err := r.Next(ctx, nil, nil)
	if err != nil || second.Text != "Created lobby-display." || len(second.ToolCalls) != 0 {
		t.Fatalf("second turn = %+v, %v", second, err)
	}
	if _, err := r.Next(ctx, nil, nil); err == nil || !strings.Contains(err.Error(), "recording") {
		t.Errorf("past the end = %v, want an error naming the recording", err)
	}
}

// A recording that is empty, has a turn that is neither text nor calls, or
// names a call with no tool is refused when it is read, not mid-run.
func TestParseReplayRefusesMalformed(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"empty":       "turns: []",
		"blank turn":  "turns:\n  - {}",
		"nameless":    "turns:\n  - tool_calls:\n      - arguments: {a: 1}",
		"not yaml":    "turns: [",
		"unknown key": "turns:\n  - txt: hi",
	} {
		if _, err := ParseReplay([]byte(doc)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}

// A positioned replay plays the turn after the model turns the conversation
// already holds, its calls numbered after the calls already made, so a run
// picked up from a stored conversation plays on from there, and the same
// conversation always gets the same turn.
func TestPositionedReplayPlaysFromTheConversation(t *testing.T) {
	t.Parallel()
	r, err := ParseReplay([]byte(recording))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Positioned()
	ctx := context.Background()
	start := []Message{{Role: RoleUser, Text: "add a display"}}
	for range 2 {
		first, err := p.Next(ctx, start, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(first.ToolCalls) != 2 || first.ToolCalls[0].ID != "call-1" || first.ToolCalls[1].ID != "call-2" {
			t.Fatalf("first turn = %+v, want call-1 and call-2 every time", first)
		}
	}
	held := append(start,
		Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "list-components"}, {ID: "call-2", Name: "create-component"}}},
		Message{Role: RoleTool, ToolCallID: "call-1", Text: "[]"},
		Message{Role: RoleTool, ToolCallID: "call-2", Text: "{}"},
	)
	second, err := p.Next(ctx, held, nil)
	if err != nil || second.Text != "Created lobby-display." {
		t.Fatalf("second turn = %+v, %v", second, err)
	}
	if _, err := p.Next(ctx, append(held, Message{Role: RoleAssistant, Text: "Created lobby-display."}), nil); err == nil || !strings.Contains(err.Error(), "recording") {
		t.Errorf("past the end = %v, want an error naming the recording", err)
	}
}

const scripted = `
turns:
  - text: The default answer.
conversations:
  - match: Triage an alarm
    turns:
      - tool_calls:
          - name: get-component
            arguments: {name: lobby-display}
      - text: The alarm is real.
`

// A positioned replay plays the conversation whose match the person's latest
// message holds, and the default turns for any other message; the position
// counts from that message, so a second prompt starts its own script.
func TestPositionedReplaySelectsByTheLatestMessage(t *testing.T) {
	t.Parallel()
	r, err := ParseReplay([]byte(scripted))
	if err != nil {
		t.Fatal(err)
	}
	p := r.Positioned()
	ctx := context.Background()

	other := []Message{{Role: RoleUser, Text: "what is down"}}
	if got, err := p.Next(ctx, other, nil); err != nil || got.Text != "The default answer." {
		t.Fatalf("other input = %+v, %v, want the default", got, err)
	}

	triage := append(other,
		Message{Role: RoleAssistant, Text: "The default answer."},
		Message{Role: RoleUser, Text: "# Triage an alarm\n\nThe component: lobby-display"},
	)
	first, err := p.Next(ctx, triage, nil)
	if err != nil || len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != "get-component" || first.ToolCalls[0].ID != "call-1" {
		t.Fatalf("triage first turn = %+v, %v, want get-component as call-1", first, err)
	}
	held := append(triage,
		Message{Role: RoleAssistant, ToolCalls: first.ToolCalls},
		Message{Role: RoleTool, ToolCallID: "call-1", Text: "{}"},
	)
	if got, err := p.Next(ctx, held, nil); err != nil || got.Text != "The alarm is real." {
		t.Fatalf("triage second turn = %+v, %v", got, err)
	}
}

// A conversation with no match or no turns is refused when it is read.
func TestParseReplayRefusesMalformedConversations(t *testing.T) {
	t.Parallel()
	for name, doc := range map[string]string{
		"no match": "turns:\n  - text: hi\nconversations:\n  - turns:\n      - text: x",
		"no turns": "turns:\n  - text: hi\nconversations:\n  - match: x",
	} {
		if _, err := ParseReplay([]byte(doc)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}
