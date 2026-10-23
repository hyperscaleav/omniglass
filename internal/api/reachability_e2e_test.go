package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// reachResp mirrors the reachability read body: per-endpoint verdict, layer
// signals, and the transition history the availability strip reads.
type reachResp struct {
	Component string `json:"component"`
	Endpoints []struct {
		Endpoint string `json:"endpoint"`
		Type     string `json:"transport"`
		Address  string `json:"address"`
		Node     string `json:"node"`
		// LastSeen is the endpoint's freshness: when ingest last landed a
		// batch it delivered, where the verdict's ts is its last transition
		// (#931, #933).
		LastSeen *time.Time `json:"last_seen"`
		Verdict  *struct {
			Value string    `json:"value"`
			TS    time.Time `json:"ts"`
		} `json:"verdict"`
		Responsive *struct {
			Value string    `json:"value"`
			TS    time.Time `json:"ts"`
		} `json:"responsive"`
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
	} `json:"endpoints"`
}

// TestReachabilityAPI drives the reachability read over HTTP: an owner sees a
// component's two endpoints composed with verdict, layers, and history from the
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	// Two endpoints: disp-1-tcp (reachable) and disp-1-icmp (down).
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, params) values
		('disp-1-tcp', 'tcp', (select id from component where name = 'disp-1'), '{"target":"10.20.4.11","port":5000}'::jsonb),
		('disp-1-icmp', 'icmp', (select id from component where name = 'disp-1'), '{"target":"10.20.4.11"}'::jsonb),
		('disp-1-snmp', 'snmp', (select id from component where name = 'disp-1'), '{"target":"10.20.4.11","port":161}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}

	t0 := time.Now().UTC().Add(-3 * time.Minute)
	t1 := t0.Add(time.Minute)
	t2 := t1.Add(time.Minute)
	// disp-1-tcp: up verdict + tcp-open=1 + connect_time.
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "endpoint-reachable", Instance: "disp-1-tcp", Value: "up", Source: "tcp", TS: t2},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "endpoint-responsive", Instance: "disp-1-tcp", Value: "up", Source: "http", TS: t2},
	}); err != nil {
		t.Fatalf("insert tcp state: %v", err)
	}
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "tcp-open", Instance: "disp-1-tcp", Value: 1, Source: "tcp", TS: t2},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "tcp-connect-time", Instance: "disp-1-tcp", Value: 3.1, Source: "tcp", TS: t2},
	}); err != nil {
		t.Fatalf("insert tcp metrics: %v", err)
	}
	// disp-1-icmp: down verdict with two transitions (up then down) + icmp-reachable=0.
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "endpoint-reachable", Instance: "disp-1-icmp", Value: "up", Source: "icmp", TS: t0},
	}); err != nil {
		t.Fatalf("insert icmp up: %v", err)
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "endpoint-reachable", Instance: "disp-1-icmp", Value: "down", Source: "icmp", TS: t1},
	}); err != nil {
		t.Fatalf("insert icmp down: %v", err)
	}
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "icmp-reachable", Instance: "disp-1-icmp", Value: 0, Source: "icmp", TS: t1},
	}); err != nil {
		t.Fatalf("insert icmp metric: %v", err)
	}

	// disp-1-snmp: a driver endpoint. Its samples are the driver's own
	// metrics (uptime here), stamped with the endpoint as their instance; it
	// never gets a reachability verdict or a layer signal (#933).
	t3 := t2.Add(30 * time.Second)
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "uptime", Instance: "disp-1-snmp", Value: 42, Source: "snmp", TS: t3},
	}); err != nil {
		t.Fatalf("insert snmp metric: %v", err)
	}

	// Ingest stamps an endpoint seen as it lands a batch (internal/bus);
	// here the driver endpoint has delivered one and the icmp probe none.
	var snmpID string
	if err := conn.QueryRow(ctx, `select id from endpoint where name = 'disp-1-snmp'`).Scan(&snmpID); err != nil {
		t.Fatalf("snmp id: %v", err)
	}
	if err := gw.StampEndpointsSeen(ctx, []string{snmpID}); err != nil {
		t.Fatalf("stamp snmp seen: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	out := c.do(ownerTok, http.MethodGet, "/components/disp-1/reachability", nil, http.StatusOK)
	var r reachResp
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode reachability: %v", err)
	}
	if r.Component != "disp-1" || len(r.Endpoints) != 3 {
		t.Fatalf("reachability: want disp-1 with 3 endpoints, got %+v", r)
	}
	// endpoints ordered by name: disp-1-icmp, disp-1-snmp, disp-1-tcp.
	icmp := r.Endpoints[0]
	snmp := r.Endpoints[1]
	tcp := r.Endpoints[2]
	if icmp.Endpoint != "disp-1-icmp" || tcp.Endpoint != "disp-1-tcp" {
		t.Fatalf("endpoint order: got %s, %s", icmp.Endpoint, tcp.Endpoint)
	}
	if tcp.Verdict == nil || tcp.Verdict.Value != "up" {
		t.Fatalf("tcp verdict: want up, got %+v", tcp.Verdict)
	}
	if tcp.Responsive == nil || tcp.Responsive.Value != "up" {
		t.Fatalf("tcp responds rung: want up, got %+v", tcp.Responsive)
	}
	if icmp.Responsive != nil {
		t.Fatalf("icmp responds rung: no L7 probe ran, want absent, got %+v", icmp.Responsive)
	}
	if tcp.Address != "10.20.4.11:5000" {
		t.Fatalf("tcp address: want 10.20.4.11:5000, got %q", tcp.Address)
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

	// last_seen is the server's stamp, not a sample's time: absent on an
	// endpoint ingest never landed a batch for, whatever rows it has.
	if icmp.LastSeen != nil || tcp.LastSeen != nil {
		t.Fatalf("unstamped endpoints carry last_seen: icmp %v, tcp %v", icmp.LastSeen, tcp.LastSeen)
	}
	// The driver endpoint: no verdict, no layers, yet it is collecting (#933).
	if snmp.Verdict != nil || len(snmp.Layers) != 0 {
		t.Fatalf("snmp: a driver endpoint has no verdict or layers, got %+v", snmp)
	}
	if snmp.LastSeen == nil || time.Since(*snmp.LastSeen) > time.Minute {
		t.Fatalf("snmp last_seen: want the stamp's now, got %v", snmp.LastSeen)
	}

	// A viewer scoped to a different component (out of scope on disp-1) gets a
	// non-disclosing 404: the permission passes (*:read) but scope injection hides
	// the row.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1"}, all, all, all, all); err != nil {
		t.Fatalf("create other component: %v", err)
	}
	var otherID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'other-1'`).Scan(&otherID); err != nil {
		t.Fatalf("other id: %v", err)
	}
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-other", "viewer", "component", otherID)
	c.do(viewerTok, http.MethodGet, "/components/disp-1/reachability", nil, http.StatusNotFound)
	// In-scope, its own component reachability is visible (empty endpoints).
	c.do(viewerTok, http.MethodGet, "/components/other-1/reachability", nil, http.StatusOK)
}

// sessionReachResp is the part of the reachability read a held session adds
// (#1059): the endpoint's id (the address :retry takes), the session's state
// with its reason, and the gate a terminal refusal holds.
type sessionReachResp struct {
	Endpoints []struct {
		ID       string `json:"id"`
		Endpoint string `json:"endpoint"`
		Session  *struct {
			State  string    `json:"state"`
			TS     time.Time `json:"ts"`
			Reason string    `json:"reason"`
		} `json:"session"`
		Rejection *struct {
			Rung  string `json:"rung"`
			Class string `json:"class"`
		} `json:"rejection"`
		Authenticated *struct {
			Value string `json:"value"`
		} `json:"authenticated"`
	} `json:"endpoints"`
}

// TestReachabilityCarriesTheHeldSession drives the read the console's session
// pill renders (#1059): a rejected session reads its state, its reason and the
// rung its gate holds; the connection fingerprint the gate carries is never
// served (a digest the read has no use for); a gate that lifted reads none
// and serves nothing; an endpoint that never held a session carries no
// session at all. Skipped under -short.
func TestReachabilityCarriesTheHeldSession(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, params) values
		('telnet', 'telnet', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:23"}'::jsonb),
		('tcp', 'tcp', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:51325"}'::jsonb),
		('icmp', 'icmp', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}
	ids := map[string]string{}
	rows, err := conn.Query(ctx, `select name, id::text from endpoint where component = (select id from component where name = 'panel-1')`)
	if err != nil {
		t.Fatalf("endpoint ids: %v", err)
	}
	for rows.Next() {
		var n, id string
		if err := rows.Scan(&n, &id); err != nil {
			t.Fatalf("scan id: %v", err)
		}
		ids[n] = id
	}
	rows.Close()

	const fingerprint = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	at := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	prop := func(key, instance, value string, ts time.Time) storage.PropertySampleWrite {
		return storage.PropertySampleWrite{OwnerKind: "component", OwnerID: "panel-1", Key: key, Instance: instance, Value: value, Source: "session", TS: ts}
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		prop("session-state", "telnet", "establishing", at.Add(-time.Minute)),
		prop("session-reason", "telnet", "", at.Add(-time.Minute)),
		prop("session-state", "telnet", "rejected", at),
		prop("session-reason", "telnet", "auth: auth rejected", at),
		prop("endpoint-authenticated", "telnet", "no", at),
		prop("endpoint-rejection", "telnet", "authenticated auth "+fingerprint, at),
		prop("session-state", "tcp", "up", at),
		prop("session-reason", "tcp", "", at),
		prop("endpoint-rejection", "tcp", "responsive host-key "+fingerprint, at.Add(-time.Hour)),
		prop("endpoint-rejection", "tcp", "none", at),
	}); err != nil {
		t.Fatalf("insert session samples: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	out := c.do(ownerTok, http.MethodGet, "/components/panel-1/reachability", nil, http.StatusOK)
	if strings.Contains(string(out), fingerprint) {
		t.Fatalf("the read served the connection fingerprint: %s", out)
	}
	var r sessionReachResp
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode reachability: %v", err)
	}
	byName := map[string]int{}
	for i, e := range r.Endpoints {
		byName[e.Endpoint] = i
	}
	telnet := r.Endpoints[byName["telnet"]]
	if telnet.ID != ids["telnet"] {
		t.Fatalf("telnet id: want %s (what :retry takes), got %q", ids["telnet"], telnet.ID)
	}
	if telnet.Session == nil || telnet.Session.State != "rejected" || telnet.Session.Reason != "auth: auth rejected" {
		t.Fatalf("telnet session: want rejected with its reason, got %+v", telnet.Session)
	}
	if !telnet.Session.TS.Equal(at) {
		t.Fatalf("telnet session ts: want the transition's %v, got %v", at, telnet.Session.TS)
	}
	if telnet.Rejection == nil || telnet.Rejection.Rung != "authenticated" || telnet.Rejection.Class != "auth" {
		t.Fatalf("telnet rejection: want the authenticated rung, class auth, got %+v", telnet.Rejection)
	}
	if telnet.Authenticated == nil || telnet.Authenticated.Value != "no" {
		t.Fatalf("telnet authenticated rung: want no, got %+v", telnet.Authenticated)
	}

	tcp := r.Endpoints[byName["tcp"]]
	if tcp.Session == nil || tcp.Session.State != "up" || tcp.Session.Reason != "" {
		t.Fatalf("tcp session: want up with no reason, got %+v", tcp.Session)
	}
	if tcp.Rejection != nil {
		t.Fatalf("tcp rejection: a gate that lifted (none) is absent, got %+v", tcp.Rejection)
	}

	icmp := r.Endpoints[byName["icmp"]]
	if icmp.Session != nil || icmp.Rejection != nil {
		t.Fatalf("icmp: an endpoint that never held a session carries none, got %+v / %+v", icmp.Session, icmp.Rejection)
	}
	if icmp.ID != ids["icmp"] {
		t.Fatalf("icmp id: want %s, got %q", ids["icmp"], icmp.ID)
	}
}
