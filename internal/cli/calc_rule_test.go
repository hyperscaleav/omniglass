package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCalcRuleCommands drives the generated `calc-rule create` and
// `calc-rule rename` (#995) against a canned server: create posts the inputs
// and reduce given as JSON flags, and rename posts the :rename custom method
// on the rule's current name.
func TestCalcRuleCommands(t *testing.T) {
	var method, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"id":"0b6f8d2e-6f4e-4f7a-9d55-3c1f2d7a9e10","name":"room-avg"}`))
	}))
	defer srv.Close()

	out, err := runCLI(t, "calc-rule", "create", "--name", "room-avg", "--owner-kind", "component",
		"--inputs", `[{"as":"t","key":"room-temperature"}]`, "--reduce", `{"reducer":"average"}`, "--output-key", "avg-temp",
		"--server", srv.URL)
	if err != nil {
		t.Fatalf("create failed: %v (%s)", err, out)
	}
	inputs, _ := body["inputs"].([]any)
	reduce, _ := body["reduce"].(map[string]any)
	if method != http.MethodPost || path != "/api/v1/calc-rules" || len(inputs) != 1 || reduce["reducer"] != "average" || body["output_key"] != "avg-temp" {
		t.Fatalf("the CLI sent %s %s %v, want POST /api/v1/calc-rules with the inputs, reduce and output key", method, path, body)
	}

	out, err = runCLI(t, "calc-rule", "rename", "room-avg", "--name", "room-max", "--server", srv.URL)
	if err != nil {
		t.Fatalf("rename failed: %v (%s)", err, out)
	}
	if method != http.MethodPost || path != "/api/v1/calc-rules/room-avg:rename" || body["name"] != "room-max" {
		t.Fatalf("the CLI sent %s %s %v, want POST /api/v1/calc-rules/room-avg:rename {name: room-max}", method, path, body)
	}
}
