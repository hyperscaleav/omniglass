package cli

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestReachabilityListSaysWhyTheVerdictIsNull is #981's CLI acceptance: on an
// install with no node, `omniglass component reachability list` for a component
// with a probed endpoint prints the null verdict beside the reason, so the
// operator is told a node must be assigned rather than left with a bare null.
// Skipped under -short.
func TestReachabilityListSaysWhyTheVerdictIsNull(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("gw: %v", err)
	}
	defer gw.Close()
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, params)
		values ('panel-1-icmp', 'icmp', (select id from component where name = 'panel-1'), '{"target":"10.0.0.5"}'::jsonb)`); err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	server := httptest.NewServer(api.NewHandler(gw))
	defer server.Close()

	got, err := runCLI(t, "component", "reachability", "list", "panel-1", "--server", server.URL, "--token", tok)
	if err != nil {
		t.Fatalf("omniglass component reachability list: %v (%s)", err, got)
	}
	var body struct {
		Endpoints []map[string]any `json:"endpoints"`
	}
	if err := json.Unmarshal([]byte(got), &body); err != nil || len(body.Endpoints) != 1 {
		t.Fatalf("printed %q, want one endpoint (%v)", got, err)
	}
	row := body.Endpoints[0]
	if v, present := row["verdict"]; !present || v != nil {
		t.Errorf("verdict = %v (present %v), want an explicit null", v, present)
	}
	if row["pending"] != "no-node" {
		t.Errorf("pending = %v, want no-node", row["pending"])
	}
}
