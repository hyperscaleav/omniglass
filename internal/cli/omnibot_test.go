package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/assistant"
)

// `omniglass omnibot` runs on a recording until a live model lands (#1166
// slice 1): with no --replay it refuses before it connects anywhere.
func TestOmnibotNeedsARecording(t *testing.T) {
	t.Setenv("OMNIGLASS_TOKEN", "ogp_00000000_unused")
	out, err := runCLI(t, "omnibot", "--server", "http://127.0.0.1:9", "add a display")
	if err == nil || !strings.Contains(err.Error(), "--replay") {
		t.Fatalf("omnibot without --replay = %v (%s), want a refusal naming --replay", err, out)
	}
}

// The person answers each ask on the terminal: only a yes runs the call, an
// empty line or the end of input is a no, and the question names the tool,
// what it does and its arguments, flagging a destructive one.
func TestOmnibotConfirmerAsksThePerson(t *testing.T) {
	p := assistant.Proposal{Tool: "delete-component", Title: "Delete a component", Arguments: map[string]any{"name": "lobby"}, Destructive: true}
	for _, tc := range []struct {
		in   string
		want bool
	}{{"y\n", true}, {"YES\n", true}, {"n\n", false}, {"\n", false}, {"", false}, {"maybe\n", false}} {
		var asked bytes.Buffer
		got, err := terminalConfirmer(strings.NewReader(tc.in), &asked)(context.Background(), p)
		if err != nil || got != tc.want {
			t.Errorf("answer %q = %v, %v; want %v", tc.in, got, err, tc.want)
		}
		for _, want := range []string{"delete-component", "Delete a component", `"name":"lobby"`, "destructive"} {
			if !strings.Contains(asked.String(), want) {
				t.Errorf("the question %q does not name %q", asked.String(), want)
			}
		}
	}
}
