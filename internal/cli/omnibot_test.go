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
		got, err := terminalConfirmer(newConsole(strings.NewReader(tc.in), &asked))(context.Background(), p)
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

// Each call the model asked for gets one line on stderr: a call that ran says
// whether it failed, while a declined or refused one never ran, so it carries
// no outcome to mistake for a failure.
func TestOmnibotObserveLine(t *testing.T) {
	for _, tc := range []struct {
		call assistant.Call
		want string
	}{
		{assistant.Call{Tool: "get-component", Decision: assistant.DecisionCalled}, "omnibot: called get-component (ok)"},
		{assistant.Call{Tool: "get-component", Decision: assistant.DecisionCalled, IsError: true}, "omnibot: called get-component (error)"},
		{assistant.Call{Tool: "set-component-tag", Decision: assistant.DecisionDeclined, IsError: true}, "omnibot: declined set-component-tag"},
		{assistant.Call{Tool: "delete-component", Decision: assistant.DecisionRefused, IsError: true}, "omnibot: refused delete-component"},
	} {
		if got := observeLine(tc.call); got != tc.want {
			t.Errorf("observeLine(%+v) = %q, want %q", tc.call, got, tc.want)
		}
	}
}

// /secret takes a name, a type, and an owner that is the platform unless a
// location or component is named (#2112).
func TestOmnibotParseCredential(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want assistant.Credential
		ok   bool
	}{
		{[]string{"lobby", "snmp-community"}, assistant.Credential{Name: "lobby", Type: "snmp-community", OwnerKind: "platform"}, true},
		{[]string{"lobby", "snmp-community", "location:hq"}, assistant.Credential{Name: "lobby", Type: "snmp-community", OwnerKind: "location", Owner: "hq"}, true},
		{[]string{"lobby", "snmp-community", "component:amp-9"}, assistant.Credential{Name: "lobby", Type: "snmp-community", OwnerKind: "component", Owner: "amp-9"}, true},
		{[]string{"lobby"}, assistant.Credential{}, false},
		{[]string{"lobby", "snmp-community", "hq"}, assistant.Credential{}, false},
		{[]string{"lobby", "snmp-community", "location:"}, assistant.Credential{}, false},
		{[]string{"lobby", "snmp-community", "platform:x"}, assistant.Credential{}, false},
		{[]string{"lobby", "snmp-community", "location:hq", "extra"}, assistant.Credential{}, false},
	} {
		got, err := parseCredential(tc.args)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("parseCredential(%q) = %+v, %v; want %+v, ok %v", tc.args, got, err, tc.want, tc.ok)
		}
	}
}
