package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestReachabilityNamesWhyAVerdictIsPending drives the read an operator meets
// on an empty install (#981): a probed endpoint no node is assigned to serves a
// null verdict with pending no-node, one assigned to a node that has not yet
// probed it awaiting-probe, and one with a verdict carries no pending field.
// Skipped under -short.
func TestReachabilityNamesWhyAVerdictIsPending(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-1"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, params) values
		('a-unassigned', 'icmp', (select id from component where name = 'disp-1'), '{"target":"10.20.4.11"}'::jsonb),
		('b-assigned', 'icmp', (select id from component where name = 'disp-1'), '{"target":"10.20.4.12"}'::jsonb),
		('c-verdict', 'icmp', (select id from component where name = 'disp-1'), '{"target":"10.20.4.13"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}
	if _, err := conn.Exec(ctx, `update endpoint set node_name = (select principal_id from node where name = 'edge-1')
		where name in ('b-assigned', 'c-verdict')`); err != nil {
		t.Fatalf("assign node: %v", err)
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "endpoint-reachable", Instance: "c-verdict", Value: "up", Source: "icmp", TS: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("insert verdict: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	out := c.do(ownerTok, http.MethodGet, "/components/disp-1/reachability", nil, http.StatusOK)
	var r struct {
		Endpoints []map[string]any `json:"endpoints"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode reachability: %v", err)
	}
	if len(r.Endpoints) != 3 {
		t.Fatalf("want 3 endpoints, got %d: %s", len(r.Endpoints), out)
	}
	for i, want := range []struct{ name, pending string }{
		{"a-unassigned", "no-node"},
		{"b-assigned", "awaiting-probe"},
		{"c-verdict", ""},
	} {
		row := r.Endpoints[i]
		if row["endpoint"] != want.name {
			t.Fatalf("row %d: want %s, got %v", i, want.name, row["endpoint"])
		}
		got, present := row["pending"]
		switch {
		case want.pending == "" && present:
			t.Errorf("%s: has a verdict, want no pending field, got %v", want.name, got)
		case want.pending != "" && got != want.pending:
			t.Errorf("%s: pending = %v, want %s", want.name, got, want.pending)
		}
		if want.pending != "" && row["verdict"] != nil {
			t.Errorf("%s: pending beside a verdict %v", want.name, row["verdict"])
		}
	}
}
