package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
)

// liveRecording is the server's model in these tests: it looks, then asks to
// add a display (a call the owner's policy asks about), then answers.
const liveRecording = `turns:
  - text: Let me look.
    tool_calls: [{name: list-components}]
  - text: I will add it.
    tool_calls: [{name: create-component, arguments: {name: lobby-display, product: generic-device}}]
  - text: Done.
`

// With no --replay, omnibot runs on the server (#2109): the server's model
// answers, each call is a line on stderr, and a call the policy asks about is
// put to the person on the terminal and answered by a resume run. A call a
// stored conversation left waiting is put first. Skipped under -short.
func TestOmnibotRunsOnTheServer(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	r, err := provider.ParseReplay([]byte(liveRecording))
	if err != nil {
		t.Fatal(err)
	}
	handler, tok := omnibotTestHandler(t, api.WithOmnibotModel(r.Positioned()))
	srv := httptest.NewServer(handler)
	defer srv.Close()
	base := []string{"--server", srv.URL, "--token", tok}

	// A conversation a run left waiting: the person declines it on resume.
	call(t, srv.URL, tok, http.MethodPost, "/assistant/conversations", map[string]any{"name": "waiting"}, http.StatusCreated)
	call(t, srv.URL, tok, http.MethodPost, "/assistant/conversations/waiting:run",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "add a display"}}}, http.StatusOK)
	out, errOut, err := runOmnibot(t, "n\n", append(base, "--resume", "waiting")...)
	if err != nil {
		t.Fatalf("resume = %v; stderr %s", err, errOut)
	}
	for _, want := range []string{"Run it?", "create-component", "omnibot: declined create-component"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("resume stderr = %q, want %q", errOut, want)
		}
	}
	if !strings.Contains(out, "Done.") {
		t.Errorf("resume stdout = %q, want the model's answer after the decline", out)
	}
	call(t, srv.URL, tok, http.MethodGet, "/components/lobby-display", nil, http.StatusNotFound)

	// A prompt in a fresh conversation: looked, asked, approved, answered.
	out, errOut, err = runOmnibot(t, "y\n", append(base, "add a display to the lobby")...)
	if err != nil {
		t.Fatalf("live run = %v; stderr %s", err, errOut)
	}
	for _, want := range []string{"Let me look.", "I will add it.", "Done."} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want %q", out, want)
		}
	}
	for _, want := range []string{"omnibot: conversation ", "omnibot: called list-components", "Run it?", "omnibot: called create-component"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	}
	call(t, srv.URL, tok, http.MethodGet, "/components/lobby-display", nil, http.StatusOK)
}

// A server with no model refuses a live run, and omnibot says why.
func TestOmnibotLiveNeedsAServerModel(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	handler, tok := omnibotTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	_, errOut, err := runOmnibot(t, "", "--server", srv.URL, "--token", tok, "add a display")
	if err == nil || !strings.Contains(err.Error(), "no model") {
		t.Fatalf("live run on a server with no model = %v; stderr %s, want the 503 naming it", err, errOut)
	}
}
