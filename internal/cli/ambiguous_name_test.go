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

func TestAmbiguousRefLocationMatchesTheAPI(t *testing.T) {
	if ambiguousRefLocation != api.AmbiguousRefLocation {
		t.Fatalf("cli %q != api %q", ambiguousRefLocation, api.AmbiguousRefLocation)
	}
}

// TestAcknowledgeByAmbiguousNameSaysToUseTheID pins #2334 on the CLI side: a 409
// for a name many components share reads as "use the id", listing them.
func TestAcknowledgeByAmbiguousNameSaysToUseTheID(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"title": "Conflict", "status": 409, "detail": "component \"videobar-1\" is ambiguous",
		"errors": []map[string]any{{"location": api.AmbiguousRefLocation, "message": "x",
			"value": api.AmbiguousRefValue{Count: 12, Candidates: []string{
				"hq.a.$comp.videobar-1 (11111111-1111-1111-1111-111111111111)",
				"hq.b.$comp.videobar-1 (22222222-2222-2222-2222-222222222222)"}}}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	var out bytes.Buffer
	root := &cobra.Command{Use: "omniglass", SilenceUsage: true}
	addClientFlags(root)
	root.AddCommand(&cobra.Command{Use: "ack", RunE: func(cmd *cobra.Command, _ []string) error {
		return runAPICommand(cmd, http.MethodPost, "/components/videobar-1/alarms/x:acknowledge", nil)
	}})
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"ack", "--server", srv.URL})
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("a 409 returned no error")
	}
	for _, want := range []string{"matches 12 rows", "id or dotted address", "11111111-1111-1111-1111-111111111111", "(and 10 more)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

// Any other 409 keeps the generic status error.
func TestOtherConflictKeepsTheStatusError(t *testing.T) {
	if err := ambiguousNameError(409, []byte(`{"detail":"taken"}`)); err != nil {
		t.Fatalf("got %v", err)
	}
}
