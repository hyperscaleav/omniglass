package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// reachResp mirrors the reachability read body: per-interface verdict, layer
// signals, and the transition history the availability strip reads.
type reachResp struct {
	Component  string `json:"component"`
	Interfaces []struct {
		Interface string `json:"interface"`
		Type      string `json:"type"`
		Endpoint  string `json:"endpoint"`
		Node      string `json:"node"`
		Verdict   *struct {
			Value string    `json:"value"`
			TS    time.Time `json:"ts"`
		} `json:"verdict"`
		Layers []struct {
			Layer  string  `json:"layer"`
			Check  string  `json:"check"`
			Value  float64 `json:"value"`
			Detail string  `json:"detail"`
		} `json:"layers"`
		History []struct {
			TS    time.Time `json:"ts"`
			Value string    `json:"value"`
		} `json:"history"`
	} `json:"interfaces"`
}

// TestReachabilityAPI drives the reachability read over HTTP: an owner sees a
// component's two interfaces composed with verdict, layers, and history from the
// state and metric sinks; a viewer with no scope on the component gets a
// non-disclosing 404 (permission gate + scope injection). Skipped under -short.
func TestReachabilityAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1", ComponentType: "display"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	// Two interfaces: disp-1-tcp (reachable) and disp-1-icmp (down).
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, params) values
		('disp-1-tcp', 'tcp', 'disp-1', '{"target":"10.20.4.11","port":5000}'::jsonb),
		('disp-1-icmp', 'icmp', 'disp-1', '{"target":"10.20.4.11"}'::jsonb)`); err != nil {
		t.Fatalf("insert interfaces: %v", err)
	}

	t0 := time.Now().UTC().Add(-3 * time.Minute)
	t1 := t0.Add(time.Minute)
	t2 := t1.Add(time.Minute)
	// disp-1-tcp: up verdict + tcp.open=1 + connect_time.
	if err := gw.InsertStateDatapoints(ctx, []storage.StateDatapointEvent{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "interface.reachable", Instance: "disp-1-tcp", Value: "up", Source: "tcp", TS: t2},
	}); err != nil {
		t.Fatalf("insert tcp state: %v", err)
	}
	if err := gw.InsertMetricDatapoints(ctx, []storage.MetricDatapointEvent{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "tcp.open", Instance: "disp-1-tcp", Value: 1, Source: "tcp", TS: t2},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "tcp.connect_time", Instance: "disp-1-tcp", Value: 3.1, Source: "tcp", TS: t2},
	}); err != nil {
		t.Fatalf("insert tcp metrics: %v", err)
	}
	// disp-1-icmp: down verdict with two transitions (up then down) + icmp.reachable=0.
	if err := gw.InsertStateDatapoints(ctx, []storage.StateDatapointEvent{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "interface.reachable", Instance: "disp-1-icmp", Value: "up", Source: "icmp", TS: t0},
	}); err != nil {
		t.Fatalf("insert icmp up: %v", err)
	}
	if err := gw.InsertStateDatapoints(ctx, []storage.StateDatapointEvent{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "interface.reachable", Instance: "disp-1-icmp", Value: "down", Source: "icmp", TS: t1},
	}); err != nil {
		t.Fatalf("insert icmp down: %v", err)
	}
	if err := gw.InsertMetricDatapoints(ctx, []storage.MetricDatapointEvent{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "icmp.reachable", Instance: "disp-1-icmp", Value: 0, Source: "icmp", TS: t1},
	}); err != nil {
		t.Fatalf("insert icmp metric: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	out := c.do(ownerTok, http.MethodGet, "/components/disp-1/reachability", nil, http.StatusOK)
	var r reachResp
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode reachability: %v", err)
	}
	if r.Component != "disp-1" || len(r.Interfaces) != 2 {
		t.Fatalf("reachability: want disp-1 with 2 interfaces, got %+v", r)
	}
	// interfaces ordered by name: disp-1-icmp then disp-1-tcp.
	icmp := r.Interfaces[0]
	tcp := r.Interfaces[1]
	if icmp.Interface != "disp-1-icmp" || tcp.Interface != "disp-1-tcp" {
		t.Fatalf("interface order: got %s, %s", icmp.Interface, tcp.Interface)
	}
	if tcp.Verdict == nil || tcp.Verdict.Value != "up" {
		t.Fatalf("tcp verdict: want up, got %+v", tcp.Verdict)
	}
	if tcp.Endpoint != "10.20.4.11:5000" {
		t.Fatalf("tcp endpoint: want 10.20.4.11:5000, got %q", tcp.Endpoint)
	}
	if len(tcp.Layers) == 0 {
		t.Fatalf("tcp layers: want at least the L4 layer, got none")
	}
	if icmp.Verdict == nil || icmp.Verdict.Value != "down" {
		t.Fatalf("icmp verdict: want down, got %+v", icmp.Verdict)
	}
	if len(icmp.History) != 2 || icmp.History[0].Value != "up" || icmp.History[1].Value != "down" {
		t.Fatalf("icmp history: want [up down], got %+v", icmp.History)
	}

	// A viewer scoped to a different component (out of scope on disp-1) gets a
	// non-disclosing 404: the permission passes (*:read) but scope injection hides
	// the row.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1", ComponentType: "display"}, all); err != nil {
		t.Fatalf("create other component: %v", err)
	}
	var otherID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'other-1'`).Scan(&otherID); err != nil {
		t.Fatalf("other id: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other", "viewer", "component", otherID)
	c.do(viewerTok, http.MethodGet, "/components/disp-1/reachability", nil, http.StatusNotFound)
	// In-scope, its own component reachability is visible (empty interfaces).
	c.do(viewerTok, http.MethodGet, "/components/other-1/reachability", nil, http.StatusOK)
}
