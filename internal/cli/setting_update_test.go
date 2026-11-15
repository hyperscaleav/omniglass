package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recorder stands up a server that records every request it is sent and answers
// 200 with an empty object, so a test can assert both what reached the wire and
// that nothing did.
type recorder struct {
	method, path, body string
	calls              int
}

func newRecorder(t *testing.T) (*recorder, string) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.method, rec.path, rec.body = r.Method, r.URL.Path, string(b)
		rec.calls++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

// TestSettingUpdateCarriesAMergePatch is #1333: `setting update <namespace>`
// sends the document given as --body, exactly, as the PATCH body. The merge patch
// has no declared properties (its keys are the namespace's own), so the generator
// used to render a command with no way to say what to change.
func TestSettingUpdateCarriesAMergePatch(t *testing.T) {
	rec, url := newRecorder(t)
	const doc = `{"egress_allow":["a"]}`
	if _, err := runCLI(t, "setting", "update", "notification", "--body", doc, "--server", url); err != nil {
		t.Fatalf("setting update: %v", err)
	}
	if rec.method != http.MethodPatch || rec.path != "/api/v1/settings/notification" {
		t.Errorf("request = %s %s, want PATCH /api/v1/settings/notification", rec.method, rec.path)
	}
	if rec.body != doc {
		t.Errorf("body = %s, want exactly %s", rec.body, doc)
	}
}

// TestSettingUpdateRestoresAKeyWithNull pins the restore spelling the help
// teaches: null on a key is sent as null, not dropped or quoted.
func TestSettingUpdateRestoresAKeyWithNull(t *testing.T) {
	rec, url := newRecorder(t)
	if _, err := runCLI(t, "setting", "update", "notification", "--body", `{"egress_allow":null}`, "--server", url); err != nil {
		t.Fatalf("setting update: %v", err)
	}
	if rec.body != `{"egress_allow":null}` {
		t.Errorf("body = %s, want the null carried through", rec.body)
	}
}

// TestSettingUpdateReadsABodyFile covers --body-file, a path or `-` for stdin.
func TestSettingUpdateReadsABodyFile(t *testing.T) {
	rec, url := newRecorder(t)
	path := filepath.Join(t.TempDir(), "patch.json")
	if err := os.WriteFile(path, []byte(`{"egress_allow":["b"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "setting", "update", "notification", "--body-file", path, "--server", url); err != nil {
		t.Fatalf("setting update: %v", err)
	}
	if rec.body != `{"egress_allow":["b"]}` {
		t.Errorf("body = %s, want the file's document", rec.body)
	}

	root := Root("test")
	root.SetIn(strings.NewReader(`{"egress_allow":["c"]}`))
	root.SetArgs([]string{"setting", "update", "notification", "--body-file", "-", "--server", url})
	root.SetOut(io.Discard)
	if err := root.Execute(); err != nil {
		t.Fatalf("setting update from stdin: %v", err)
	}
	if rec.body != `{"egress_allow":["c"]}` {
		t.Errorf("stdin body = %s, want the piped document", rec.body)
	}
}

// TestSettingUpdateRefusesABadBodyBeforeAnyRequest: a malformed document, a
// document that is not an object, no document at all, and both spellings at once
// are each a usage error (non-zero) that never leaves the shell.
func TestSettingUpdateRefusesABadBodyBeforeAnyRequest(t *testing.T) {
	cases := map[string][]string{
		"malformed":      {"--body", `{"egress_allow":`},
		"not an object":  {"--body", `["a"]`},
		"neither":        {},
		"both":           {"--body", `{}`, "--body-file", "x.json"},
		"missing file":   {"--body-file", filepath.Join(t.TempDir(), "absent.json")},
		"empty document": {"--body", ``},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			rec, url := newRecorder(t)
			args := append([]string{"setting", "update", "notification", "--server", url}, extra...)
			out, err := runCLI(t, args...)
			if err == nil {
				t.Fatalf("accepted; output was %q", out)
			}
			if rec.calls != 0 {
				t.Errorf("the CLI issued %d request(s) anyway; a bad body must be refused at the shell", rec.calls)
			}
		})
	}
}
