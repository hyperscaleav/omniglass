package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/spf13/cobra"
)

// TestARefusalReachesTheOperatorBeforeTheStatus pins the third part of #707,
// which is the part that gets forgotten: a refusal an operator cannot act on
// sends them to an administrator with no ask. The API answers the create's
// membership gate with a 403 that NAMES system:update; this proves the CLI
// actually shows it, rather than swallowing the body and printing the status
// alone, which is what "the CLI's refusal names the permission" comes down to on
// this side of the wire.
//
// It drives the real command runtime (runAPICommand, the hand-written half every
// generated command calls) against a real server, so what is asserted is what an
// operator sees on stdout, not that a message exists somewhere in the response.
//
// The server is canned rather than the real handler, because what is under test
// is the CLI's rendering and a real one would drag Postgres and a principal in
// for it. So the message is taken from the API's own sentinel rather than
// retyped: a copy would go on passing while the API's wording drifted, and the
// wording is the whole subject. The API side of the same promise, that the
// refusal is issued at all and names the permission, is
// TestCreatingAComponentIntoASystemNeedsTheMembershipPermission.
func TestARefusalReachesTheOperatorBeforeTheStatus(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"title": "Forbidden", "status": http.StatusForbidden, "detail": api.ErrSystemBindNeedsUpdate,
	})
	if err != nil {
		t.Fatalf("encode the refusal body: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	// The real invocation shape: the connection flags live on the root and every
	// generated command inherits them, so the command is executed rather than
	// called, which is also what parses the flags clientFromCmd reads.
	var out bytes.Buffer
	root := &cobra.Command{Use: "omniglass"}
	addClientFlags(root)
	root.AddCommand(&cobra.Command{
		Use: "create",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAPICommand(cmd, http.MethodPost, "/components", map[string]any{"name": "panel", "system": "av"})
		},
	})
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"create", "--server", srv.URL})

	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("a 403 returned no error, so the command would exit 0")
	}
	if !strings.Contains(out.String(), "system:update") {
		t.Fatalf("stdout = %q, want the refusal to name the permission to ask for", out.String())
	}
	// The WHOLE message, not a prefix of it: the recovery that needs no grant is
	// the last sentence, and a renderer that truncated would drop exactly that.
	// Decoded off the front of stdout, since cobra's own error line and usage
	// follow the body the command printed.
	var shown struct {
		Detail string `json:"detail"`
	}
	if err := json.NewDecoder(bytes.NewReader(out.Bytes())).Decode(&shown); err != nil {
		t.Fatalf("stdout does not open with the problem document the CLI was handed: %v (%q)", err, out.String())
	}
	if shown.Detail != api.ErrSystemBindNeedsUpdate {
		t.Errorf("stdout carried detail %q, want the API's own %q", shown.Detail, api.ErrSystemBindNeedsUpdate)
	}
}
