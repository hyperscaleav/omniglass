package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEventRuleRenameCommand drives the generated `event-rule rename` (#977)
// against a canned server: it posts the :rename custom method on the rule's
// current name, carrying the new name in the body.
func TestEventRuleRenameCommand(t *testing.T) {
	var method, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"id":"0b6f8d2e-6f4e-4f7a-9d55-3c1f2d7a9e10","name":"door-held"}`))
	}))
	defer srv.Close()

	out, err := runCLI(t, "event-rule", "rename", "door-open", "--name", "door-held", "--server", srv.URL)
	if err != nil {
		t.Fatalf("rename failed: %v (%s)", err, out)
	}
	if method != http.MethodPost || path != "/api/v1/event-rules/door-open:rename" || body["name"] != "door-held" {
		t.Fatalf("the CLI sent %s %s %v, want POST /api/v1/event-rules/door-open:rename {name: door-held}", method, path, body)
	}
}
