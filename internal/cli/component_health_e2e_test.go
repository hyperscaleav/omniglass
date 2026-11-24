package cli

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestComponentHealthReadsItsHistoryFromTheAPI: against a real server, `omniglass
// component health list` returns a component's verdict and the transitions
// recorded for it, and --since bounds them (#2305). Skipped under -short.
func TestComponentHealthReadsItsHistoryFromTheAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("gw: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	prod := storagetest.MintProduct(t, ctx, gw, "video-bar")
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &prod.Name}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "high", Message: "no signal", DedupKey: "no-signal"}); err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)

	type report struct {
		OwnerKind   string `json:"owner_kind"`
		Health      string `json:"health"`
		Transitions []struct {
			Health string `json:"health"`
		} `json:"transitions"`
	}
	read := func(extra ...string) report {
		t.Helper()
		args := append([]string{"component", "health", "list", "bar-1", "--server", srv.URL, "--token", tok}, extra...)
		out, err := runCLI(t, args...)
		if err != nil {
			t.Fatalf("omniglass component health list: %v\n%s", err, out)
		}
		var r report
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("printed %q, not a report: %v", out, err)
		}
		return r
	}

	got := read()
	if got.OwnerKind != "component" || got.Health != "outage" && got.Health != "degraded" {
		t.Fatalf("report = %+v, want a component with an alarmed verdict", got)
	}
	if len(got.Transitions) == 0 {
		t.Fatalf("no transitions for an alarmed component: %+v", got)
	}
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if got := read("--since", future); len(got.Transitions) != 0 {
		t.Errorf("--since in the future returned %d transitions, want 0", len(got.Transitions))
	}
}
