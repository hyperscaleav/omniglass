package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
)

// keptConversation is a conversation as its owner reads it back.
type keptConversation struct {
	Pending  json.RawMessage `json:"pending"`
	Messages []struct {
		Role string           `json:"role"`
		Body provider.Message `json:"body"`
	} `json:"messages"`
}

func readConversation(t *testing.T, server, tok, name string) keptConversation {
	t.Helper()
	var c keptConversation
	if err := json.Unmarshal(call(t, server, tok, http.MethodGet, "/assistant/conversations/"+name, nil, http.StatusOK), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func recordingOf(t *testing.T, yaml string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rec.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func runOmnibot(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := Root("test")
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"omnibot"}, args...))
	err := root.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

// --resume keeps a conversation on the server and picks it up later (#2113):
// a second run with the same name sends the model the kept transcript before
// its own prompt, and both turns are kept in order. Skipped under -short.
func TestOmnibotResumeCarriesAConversationOn(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	handler, tok := omnibotTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	base := []string{"--server", srv.URL, "--token", tok, "--resume", "triage"}

	first := recordingOf(t, "turns:\n  - text: nothing is down.\n")
	if out, errOut, err := runOmnibot(t, "", append(base, "--replay", first, "what is down")...); err != nil || !strings.Contains(out, "nothing is down") {
		t.Fatalf("first run = %q, %v; stderr %s", out, err, errOut)
	}

	var model *heard
	defer func(prev func(string) (provider.Provider, error)) { openModel = prev }(openModel)
	openModel = func(path string) (provider.Provider, error) {
		r, err := provider.LoadReplay(path)
		model = &heard{Provider: r}
		return model, err
	}
	second := recordingOf(t, "turns:\n  - text: still nothing.\n")
	_, errOut, err := runOmnibot(t, "", append(base, "--replay", second, "and now")...)
	if err != nil {
		t.Fatalf("second run: %v; stderr %s", err, errOut)
	}
	if !strings.Contains(errOut, "conversation triage, 2 messages kept") {
		t.Errorf("the second run did not say it picked the conversation up; stderr %s", errOut)
	}
	if model == nil || len(model.msgs) < 3 || model.msgs[0].Text != "what is down" || model.msgs[1].Text != "nothing is down." || model.msgs[2].Text != "and now" {
		t.Fatalf("the model was sent %+v, want the kept turn then the new prompt", model)
	}

	c := readConversation(t, srv.URL, tok, "triage")
	var said []string
	for _, m := range c.Messages {
		said = append(said, m.Body.Text)
	}
	if got := strings.Join(said, "|"); got != "what is down|nothing is down.|and now|still nothing." {
		t.Fatalf("kept transcript = %q, want both turns in order", got)
	}
}

// A conversation a run paused in, waiting on a write, continues from that
// call (#2113): --resume puts the transcript's open call to the person, runs
// it on a yes, and the kept pending clears. A pending that names no open call
// in the transcript is refused, never acted on. Skipped under -short.
func TestOmnibotResumeDecidesTheWaitingCall(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	handler, tok := omnibotTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	paused := func(name, pendingID string) {
		call(t, srv.URL, tok, http.MethodPost, "/assistant/conversations", map[string]any{"name": name}, http.StatusCreated)
		call(t, srv.URL, tok, http.MethodPost, "/assistant/conversations/"+name+":append", map[string]any{
			"after": 0,
			"messages": []map[string]any{
				{"role": "user", "body": provider.Message{Role: provider.RoleUser, Text: "add amp-7"}},
				{"role": "assistant", "body": provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
					{ID: "call-1", Name: "create-component", Arguments: map[string]any{"name": "amp-7", "product": "generic-device"}},
				}}},
			},
			"pending": map[string]any{"call_id": pendingID},
		}, http.StatusOK)
	}

	paused("refit", "call-1")
	rec := recordingOf(t, "turns:\n  - text: amp-7 is added.\n")
	out, errOut, err := runOmnibot(t, "y\n", "--server", srv.URL, "--token", tok, "--resume", "refit", "--replay", rec)
	if err != nil || !strings.Contains(out, "amp-7 is added") {
		t.Fatalf("resume = %q, %v; stderr %s", out, err, errOut)
	}
	call(t, srv.URL, tok, http.MethodGet, "/components/amp-7", nil, http.StatusOK)
	c := readConversation(t, srv.URL, tok, "refit")
	if len(c.Pending) != 0 && string(c.Pending) != "null" {
		t.Errorf("pending after the answer = %s, want cleared", c.Pending)
	}
	if len(c.Messages) != 4 || c.Messages[2].Body.ToolCallID != "call-1" || c.Messages[3].Body.Text != "amp-7 is added." {
		t.Errorf("kept transcript = %+v, want the call's result and the answer after it", c.Messages)
	}

	paused("forged", "call-9")
	_, _, err = runOmnibot(t, "y\n", "--server", srv.URL, "--token", tok, "--resume", "forged", "--replay", rec)
	if err == nil || !strings.Contains(err.Error(), "no such open call") {
		t.Fatalf("a pending naming no open call = %v, want refused", err)
	}
	if n := len(auditOf(t, srv.URL, tok, "component")); n != 1 {
		t.Errorf("%d component writes, want only the approved one", n)
	}
}
