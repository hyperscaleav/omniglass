package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
func TestARefusalReachesTheOperatorBeforeTheStatus(t *testing.T) {
	const detail = "creating a component into a system writes that system's membership, which requires system:update (the same permission PUT /systems/{name}/members/{component} requires). Create the component without a system, or ask for system:update."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"title":"Forbidden","status":403,"detail":` + quoteJSON(detail) + `}`))
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

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("a 403 returned no error, so the command would exit 0")
	}
	if !strings.Contains(out.String(), "system:update") {
		t.Fatalf("stdout = %q, want the refusal to name the permission to ask for", out.String())
	}
}

// quoteJSON is a minimal JSON string encoder for the fixture body above, so the
// message can be written once and shared with the assertion.
func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
