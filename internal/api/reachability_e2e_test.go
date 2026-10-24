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
			Rung       string `json:"rung"`
			Class      string `json:"class"`
			PinnedKey  string `json:"pinned_key"`
			OfferedKey string `json:"offered_key"`
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
// and serves nothing; an endpoint that never held a session, or no longer
// holds one, carries no session at all. Skipped under -short.
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
		('icmp', 'icmp', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9"}'::jsonb),
		('http', 'http', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:80"}'::jsonb),
		('ssh', 'ssh', (select id from component where name = 'panel-1'), '{"target":"127.60.0.9:22"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}
	// A standing listen task is what holds an endpoint's session (ADR-0150
	// point 3): telnet and tcp hold one; http's listen task was removed (its
	// driver lost the listener), so the node tore its session down.
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, spec) select 'listen-' || name, 'listen', id, '{}'::jsonb
		from endpoint where name in ('telnet', 'tcp', 'ssh') and component = (select id from component where name = 'panel-1')`); err != nil {
		t.Fatalf("insert listen tasks: %v", err)
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
	pinnedKey, offeredKey := "SHA256:"+strings.Repeat("P", 43), "SHA256:"+strings.Repeat("O", 43)
	decoyPinned, decoyOffered := "SHA256:"+strings.Repeat("x", 43), "SHA256:"+strings.Repeat("y", 43)
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
		prop("session-state", "http", "rejected", at),
		prop("session-reason", "http", "auth: auth rejected", at),
		// A changed host key (#1078): the gate carries both keys as data,
		// and the reason names different ones, so a read that parsed the
		// prose would show the wrong keys.
		prop("session-state", "ssh", "rejected", at),
		prop("session-reason", "ssh", "host-key: the host key changed: pinned "+decoyPinned+", offered "+decoyOffered, at),
		prop("endpoint-rejection", "ssh", "responsive host-key "+fingerprint+" "+pinnedKey+" "+offeredKey, at),
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

	// Teardown publishes no state (ADR-0150), so the last one outlives a
	// session nobody holds; the read serves a session only while the
	// endpoint is configured to hold one, so a torn-down session's rejected
	// never stands beside a Retry that can change nothing.
	if http := r.Endpoints[byName["http"]]; http.Session != nil {
		t.Fatalf("http: an endpoint no longer holding a session carries its last state: %+v", http.Session)
	}

	// A key refusal's gate serves both keys from the gate, never from the
	// reason; an auth gate serves none.
	sshEp := r.Endpoints[byName["ssh"]]
	if sshEp.Rejection == nil || sshEp.Rejection.Class != "host-key" || sshEp.Rejection.Rung != "responsive" ||
		sshEp.Rejection.PinnedKey != pinnedKey || sshEp.Rejection.OfferedKey != offeredKey {
		t.Fatalf("ssh rejection: want the host-key gate with the gate's own keys, got %+v", sshEp.Rejection)
	}
	if telnet.Rejection.PinnedKey != "" || telnet.Rejection.OfferedKey != "" {
		t.Fatalf("telnet: an auth gate served keys: %+v", telnet.Rejection)
	}

	icmp := r.Endpoints[byName["icmp"]]
	if icmp.Session != nil || icmp.Rejection != nil {
		t.Fatalf("icmp: an endpoint that never held a session carries none, got %+v / %+v", icmp.Session, icmp.Rejection)
	}
	if icmp.ID != ids["icmp"] {
		t.Fatalf("icmp id: want %s, got %q", ids["icmp"], icmp.ID)
	}
}

// TestReachabilityCarriesTheTLSFacts drives the read the console's tls rows
// render (#1035): a tls endpoint reads its verification mode (from its
// params, verify when none is set), the key it pins (in pin mode alone), the
// latest handshake time, and the certificate's expiry and days left; a gate
// for a changed key carries the pinned and offered keys as data, and while a
// certificate gate holds the last accepted certificate's facts are withheld
// rather than shown as current. Skipped under -short.
func TestReachabilityCarriesTheTLSFacts(t *testing.T) {
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
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	for _, name := range []string{"panel-1", "panel-2"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
	}
	for comp, params := range map[string]string{"panel-1": `{"target":"127.60.0.9:51326","tls-mode":"pin"}`, "panel-2": `{"target":"127.60.0.10:51326"}`} {
		c := comp
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tls", Component: &c, Params: []byte(params)}, all); err != nil {
			t.Fatalf("tls endpoint on %s: %v", comp, err)
		}
	}
	pinned, offered := "SHA256:"+strings.Repeat("A", 43), "SHA256:"+strings.Repeat("B", 43)
	expiry := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	const fingerprint = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	prop := func(key, value string) storage.PropertySampleWrite {
		return storage.PropertySampleWrite{OwnerKind: "component", OwnerID: "panel-1", Key: key, Instance: "tls", Value: value, Source: "tls", TS: at}
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		prop("tls-certificate", pinned),
		prop("tls-certificate-expiry", expiry.Format(time.RFC3339)),
		// panel-2 verifies, yet holds a key from an earlier pin: not served.
		{OwnerKind: "component", OwnerID: "panel-2", Key: "tls-certificate", Instance: "tls", Value: pinned, Source: "tls", TS: at},
	}); err != nil {
		t.Fatal(err)
	}
	metric := func(key string, v float64) storage.MetricSampleWrite {
		return storage.MetricSampleWrite{OwnerKind: "component", OwnerID: "panel-1", Key: key, Instance: "tls", Value: v, Source: "tls", TS: at}
	}
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{metric("tls-handshake-time", 12.5), metric("tls-certificate-days-left", 10.02)}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	type tlsResp struct {
		Endpoints []struct {
			Endpoint string `json:"endpoint"`
			TLS      *struct {
				Mode        string     `json:"mode"`
				Key         string     `json:"key"`
				HandshakeMS *float64   `json:"handshake_ms"`
				Expires     *time.Time `json:"expires"`
				DaysLeft    *float64   `json:"days_left"`
			} `json:"tls"`
			Rejection *struct {
				Rung       string `json:"rung"`
				Class      string `json:"class"`
				PinnedKey  string `json:"pinned_key"`
				OfferedKey string `json:"offered_key"`
			} `json:"rejection"`
		} `json:"endpoints"`
	}
	read := func(comp string) tlsResp {
		var r tlsResp
		out := c.do(ownerTok, http.MethodGet, "/components/"+comp+"/reachability", nil, http.StatusOK)
		if strings.Contains(string(out), fingerprint) {
			t.Fatalf("the read served the connection fingerprint: %s", out)
		}
		if err := json.Unmarshal(out, &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Endpoints) != 1 {
			t.Fatalf("%s endpoints = %+v", comp, r.Endpoints)
		}
		return r
	}
	ep := read("panel-1").Endpoints[0]
	if ep.TLS == nil || ep.TLS.Mode != "pin" || ep.TLS.Key != pinned {
		t.Fatalf("tls = %+v, want pin mode and the pinned key", ep.TLS)
	}
	if ep.TLS.HandshakeMS == nil || *ep.TLS.HandshakeMS != 12.5 || ep.TLS.DaysLeft == nil || *ep.TLS.DaysLeft != 10.02 {
		t.Fatalf("tls = %+v, want the handshake time and the days left", ep.TLS)
	}
	if ep.TLS.Expires == nil || !ep.TLS.Expires.Equal(expiry) {
		t.Fatalf("tls expires = %v, want %v", ep.TLS.Expires, expiry)
	}
	if ep.Rejection != nil {
		t.Fatalf("rejection = %+v before any gate", ep.Rejection)
	}

	// The component is replaced: a certificate gate holds, carrying both
	// keys, and the last accepted certificate's facts are no longer served
	// as current (under the gate the probe connects only, so they freeze).
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "panel-1", Key: "endpoint-rejection", Instance: "tls", Value: "responsive certificate " + fingerprint + " " + pinned + " " + offered, Source: "tls", TS: at.Add(time.Second)},
	}); err != nil {
		t.Fatal(err)
	}
	ep = read("panel-1").Endpoints[0]
	if ep.Rejection == nil || ep.Rejection.Class != "certificate" || ep.Rejection.PinnedKey != pinned || ep.Rejection.OfferedKey != offered {
		t.Fatalf("rejection = %+v, want class certificate with both keys", ep.Rejection)
	}
	if ep.TLS == nil || ep.TLS.Key != pinned || ep.TLS.Expires != nil || ep.TLS.DaysLeft != nil {
		t.Fatalf("tls under a certificate gate = %+v, want the pin kept and the stale certificate facts withheld", ep.TLS)
	}

	// An endpoint with no mode set verifies; a key left from an earlier pin
	// plays no part in verifying, so it is not served; nothing is measured.
	if ep := read("panel-2").Endpoints[0]; ep.TLS == nil || ep.TLS.Mode != "verify" || ep.TLS.Key != "" || ep.TLS.DaysLeft != nil {
		t.Fatalf("panel-2 tls = %+v, want verify, no key, nothing measured", ep.TLS)
	}
}
