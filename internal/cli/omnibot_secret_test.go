package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/assistant/provider"
)

// heard keeps everything the model was sent, so a test can read what reached
// it.
type heard struct {
	provider.Provider
	msgs []provider.Message
}

func (h *heard) Next(ctx context.Context, msgs []provider.Message, tools []provider.Tool) (provider.Turn, error) {
	h.msgs = msgs
	return h.Provider.Next(ctx, msgs, tools)
}

// A credential handed to omnibot in its chat never reaches the model (#2112):
// /secret reads the value without echo, creates the secret with the person's
// own token, and only its reference enters the conversation. The model then
// binds it by that name, the bind audited as omnibot's, while the value is in
// nothing the model read and nothing the CLI printed. Skipped under -short.
func TestOmnibotSecretNeverReachesTheModel(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	handler, tok := omnibotTestHandler(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	call(t, srv.URL, tok, http.MethodPost, "/components", map[string]any{"name": "amp-9", "product": "generic-device"}, http.StatusCreated)

	recording := filepath.Join(t.TempDir(), "bind.yaml")
	if err := os.WriteFile(recording, []byte(`
turns:
  - tool_calls:
      - name: create-endpoint
        arguments: {component: amp-9, driver: snmp-generic, inputs: {host: 203.0.113.9, community: lobby-community}}
  - text: amp-9 polls over SNMP with lobby-community.
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var model *heard
	defer func(prev func(string) (provider.Provider, error)) { openModel = prev }(openModel)
	openModel = func(path string) (provider.Provider, error) {
		r, err := provider.LoadReplay(path)
		model = &heard{Provider: r}
		return model, err
	}

	const value = "pub1ic-s3cret-community"
	var stdout, stderr bytes.Buffer
	root := Root("test")
	// A /secret that fails (no such location) says why without the value.
	root.SetIn(strings.NewReader("/secret stray snmp-community location:nowhere\n" + value + "\n" +
		"/secret lobby-community snmp-community\n" + value + "\nattach it to amp-9\ny\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"omnibot", "--server", srv.URL, "--token", tok, "--replay", recording})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("omnibot chat: %v\nstderr: %s", err, stderr.String())
	}

	if model == nil || len(model.msgs) == 0 {
		t.Fatal("the model was never asked")
	}
	var sent strings.Builder
	for _, m := range model.msgs {
		sent.WriteString(m.Text + "\n")
		for _, c := range m.ToolCalls {
			b, _ := json.Marshal(c.Arguments)
			sent.Write(b)
		}
	}
	if strings.Contains(sent.String(), value) {
		t.Errorf("the model was sent the credential's value:\n%s", sent.String())
	}
	if !strings.Contains(sent.String(), "lobby-community") {
		t.Errorf("the model was not told the credential's reference:\n%s", sent.String())
	}
	for name, out := range map[string]string{"stdout": stdout.String(), "stderr": stderr.String()} {
		if strings.Contains(out, value) {
			t.Errorf("the CLI printed the credential's value on %s:\n%s", name, out)
		}
	}
	if !strings.Contains(stderr.String(), "create the secret stray") {
		t.Errorf("the failed /secret did not say so; stderr: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "amp-9 polls over SNMP") {
		t.Errorf("the chat did not answer; stdout: %s\nstderr: %s", stdout.String(), stderr.String())
	}

	endpoints := auditOf(t, srv.URL, tok, "endpoint")
	if len(endpoints) != 1 || endpoints[0].Verb != "create" || endpoints[0].Agent != "omnibot" {
		t.Errorf("the bind's audit rows = %+v, want one create by agent omnibot", endpoints)
	}
	secrets := auditOf(t, srv.URL, tok, "secret")
	if len(secrets) != 1 || secrets[0].Verb != "create" || secrets[0].Agent != "" {
		t.Errorf("the secret's audit rows = %+v, want one create by the person, no agent", secrets)
	}
}

type auditRow struct {
	Verb  string `json:"verb"`
	Agent string `json:"agent"`
}

// auditOf is the audit log's rows for one resource.
func auditOf(t *testing.T, server, tok, resource string) []auditRow {
	t.Helper()
	var audit struct {
		Events []auditRow `json:"events"`
	}
	if err := json.Unmarshal(call(t, server, tok, http.MethodGet, "/audit-log?resource="+resource, nil, http.StatusOK), &audit); err != nil {
		t.Fatal(err)
	}
	return audit.Events
}

// call is one REST call with tok, answering the body when the status is want.
func call(t *testing.T, server, tok, method, path string, body any, want int) []byte {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, server+"/api/v1"+path, rdr)
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, resp.StatusCode, want, out)
	}
	return out
}
