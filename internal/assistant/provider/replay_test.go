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
