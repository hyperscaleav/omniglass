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
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/jackc/pgx/v5"
)

// TestReachabilityNamesTheUpstreamAGateIsHeldBy drives the read of an
// endpoint held by a path upstream (#1652): the node publishes the upstream
// component's id as the reason's source, and the read serves it beside the
// component's name, so an operator reads which upstream is down without a
// second lookup. A source that names no component the caller can read
// serves the id alone. Skipped under -short.
func TestReachabilityNamesTheUpstreamAGateIsHeldBy(t *testing.T) {
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
	upstream, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "gateway-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create upstream: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all); err != nil {
		t.Fatalf("create dependent: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, params) values
		('telnet', 'telnet', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:23"}'::jsonb),
		('icmp', 'icmp', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}

	since := time.Date(2026, 11, 10, 14, 3, 0, 0, time.UTC)
	const gone = "00000000-0000-4000-8000-000000000001"
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	prop := func(instance, value string) storage.PropertySampleWrite {
		return storage.PropertySampleWrite{OwnerKind: "component", OwnerID: "panel-1", Key: "endpoint-rejection", Instance: instance, Value: value, Source: "node", TS: at}
	}
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{
		prop("telnet", "upstream "+since.Format(time.RFC3339)+" "+upstream.ID),
		prop("icmp", "upstream "+since.Format(time.RFC3339)+" "+gone),
	}); err != nil {
		t.Fatalf("insert gate samples: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	out := c.do(ownerTok, http.MethodGet, "/components/panel-1/reachability", nil, http.StatusOK)
	var r struct {
		Endpoints []struct {
			Endpoint    string    `json:"endpoint"`
			Rejection   *struct{} `json:"rejection"`
			GateReasons []struct {
				Kind      string    `json:"kind"`
				Since     time.Time `json:"since"`
				Source    string    `json:"source"`
				Component string    `json:"component"`
			} `json:"gate_reasons"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode reachability: %v", err)
	}
	got := map[string]int{}
	for i, e := range r.Endpoints {
		got[e.Endpoint] = i
	}
	telnet := r.Endpoints[got["telnet"]]
	if telnet.Rejection != nil {
		t.Fatalf("telnet: an upstream hold read as a refusal: %s", out)
	}
	if len(telnet.GateReasons) != 1 {
		t.Fatalf("telnet gate reasons: want the upstream reason, got %s", out)
	}
	g := telnet.GateReasons[0]
	if g.Kind != "upstream" || !g.Since.Equal(since) || g.Source != upstream.ID || g.Component != "gateway-1" {
		t.Fatalf("telnet upstream reason: want upstream since %s from %s named gateway-1, got %+v", since, upstream.ID, g)
	}
	icmp := r.Endpoints[got["icmp"]]
	if len(icmp.GateReasons) != 1 || icmp.GateReasons[0].Source != gone || icmp.GateReasons[0].Component != "" {
		t.Fatalf("icmp: a source naming no readable component serves the id alone, got %s", out)
	}
}
