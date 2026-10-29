package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestASystemOwnsItsPushedTelemetry drives a system-owned push end to end
// (#422, ADR-0422): a real HTTP request, through a real bus, landing real
// rows on the system arc, then read back on the system's own surfaces. A
// system's metric, property and event land with owner_kind system and no
// instance; a log line, which only a component owns, and an instance, which
// names an endpoint a system has none of, refuse the batch whole.
func TestASystemOwnsItsPushedTelemetry(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
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
	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	all := scope.Set{All: true}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "huddle-1"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer busSrv.Shutdown()
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	out := c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":      map[string]any{"kind": "system", "ref": "huddle-1"},
		"source":     "room-booking-cloud",
		"metrics":    []map[string]any{{"name": "room-temperature", "value": 21.5}},
		"properties": []map[string]any{{"name": "video-input", "value": "hdmi2"}},
		"events":     []map[string]any{{"name": "call-started", "message": "call started, 4 participants"}},
	}, http.StatusAccepted)
	var r pushResp
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode push response: %v", err)
	}
	if r.Accepted.Metrics != 1 || r.Accepted.Properties != 1 || r.Accepted.Events != 1 || len(r.Rejected) != 0 {
		t.Fatalf("push = %+v, want one accepted per lane, none rejected", r)
	}

	// The metric reads on the system's effective metrics, sampled.
	waitFor(t, "the system's metric", func() bool {
		var m struct {
			Metrics []effectiveMetricWire `json:"metrics"`
		}
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/huddle-1/metrics", nil, http.StatusOK), &m); err != nil {
			return false
		}
		for _, x := range m.Metrics {
			if x.MetricTypeName == "room-temperature" && x.IsSampled && string(x.Value) == "21.5" {
				return true
			}
		}
		return false
	})
	// The property's change history reads on the system's series, observed,
	// at the owner level, its source the batch's.
	waitFor(t, "the system's property", func() bool {
		var p struct {
			Samples []struct {
				Value      string `json:"value"`
				Instance   string `json:"instance"`
				Provenance string `json:"provenance"`
				Source     string `json:"source"`
			} `json:"samples"`
		}
		if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems/huddle-1/properties/video-input/samples", nil, http.StatusOK), &p); err != nil {
			return false
		}
		return len(p.Samples) == 1 && p.Samples[0].Value == "hdmi2" && p.Samples[0].Instance == "" &&
			p.Samples[0].Provenance == "observed" && p.Samples[0].Source == "room-booking-cloud"
	})
	// The event reads on the system's events, raised by the system itself.
	waitFor(t, "the system's event", func() bool {
		return strings.Contains(string(c.do(ownerTok, http.MethodGet, "/systems/huddle-1/events", nil, http.StatusOK)), "call started, 4 participants")
	})
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	for _, tbl := range []string{"metric", "property", "event"} {
		var n int
		from := tbl
		if tbl == "metric" {
			from = storagetest.MetricRows + " metric"
		}
		if err := conn.QueryRow(ctx, `select count(*) from `+from+` where owner_kind = 'system' and system_id = (select id from system where name = 'huddle-1') and provenance = 'observed' and instance = ''`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 1 {
			t.Fatalf("%d observed %s rows on the system's arc, want 1", n, tbl)
		}
	}

	// An unknown system's series is the non-disclosing 404.
	c.do(ownerTok, http.MethodGet, "/systems/no-such-system/properties/video-input/samples", nil, http.StatusNotFound)

	// A log line is component-owned (#589): a system batch carrying one is
	// refused whole, its sample with it, and the refusal says why.
	resp := c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "system", "ref": "huddle-1"},
		"metrics": []map[string]any{{"name": "occupancy-count", "value": 4}},
		"logs":    []map[string]any{{"message": "booking sync ok"}},
	}, http.StatusUnprocessableEntity)
	if !strings.Contains(string(resp), "log") {
		t.Fatalf("the log refusal does not say it is the log lines: %s", resp)
	}
	// An instance names the endpoint an observation came through, and a
	// system has no endpoints: a named one refuses the batch whole.
	resp = c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "system", "ref": "huddle-1"},
		"metrics": []map[string]any{{"name": "occupancy-count", "instance": "https", "value": 4}},
	}, http.StatusUnprocessableEntity)
	if !strings.Contains(string(resp), `\"https\" names no endpoint on this system`) {
		t.Fatalf("the instance refusal does not name the instance and the system: %s", resp)
	}
	// Neither refused batch landed anything. A later accepted push flushes
	// the lane: once it lands, anything published before it has too.
	c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "system", "ref": "huddle-1"},
		"metrics": []map[string]any{{"name": "room-temperature", "value": 22.5}},
	}, http.StatusAccepted)
	waitFor(t, "the flushing push", func() bool {
		var n int
		_ = conn.QueryRow(ctx, `select count(*) from `+storagetest.MetricRows+` metric where owner_kind = 'system' and value = 22.5`).Scan(&n)
		return n == 1
	})
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from `+storagetest.MetricRows+` metric where metric_type_id = (select id from metric_type where name = 'occupancy-count')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d occupancy-count rows landed from refused batches, want none", n)
	}

	// The writer classes hold on the system arc too: health is the
	// platform's, and a calc rule's output is that rule's alone.
	c.do(ownerTok, http.MethodPost, "/metric-types", map[string]any{"name": "avg-temp", "data_type": "float"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/calc-rules", map[string]any{"name": "room-mean", "owner_kind": "system",
		"inputs": []map[string]any{{"alias": "t", "key": "room-temperature"}}, "reduce": map[string]any{"expr": "avg(t)"}, "output_key": "avg-temp"}, http.StatusCreated)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":      map[string]any{"kind": "system", "ref": "huddle-1"},
		"metrics":    []map[string]any{{"name": "avg-temp", "value": 1}},
		"properties": []map[string]any{{"name": "health", "value": "ok"}},
	}, http.StatusAccepted), &r); err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, x := range r.Rejected {
		reasons[x.Name] = x.Reason
	}
	if len(r.Rejected) != 2 || !strings.Contains(reasons["avg-temp"], "room-mean") || reasons["health"] == "" {
		t.Fatalf("rejected = %+v, want avg-temp naming room-mean and health refused", r.Rejected)
	}
	// Location and node owners are not pushed at all.
	for _, kind := range []string{"location", "node"} {
		c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
			"owner":   map[string]any{"kind": kind, "ref": "anything"},
			"metrics": []map[string]any{{"name": "room-temperature", "value": 1}},
		}, http.StatusUnprocessableEntity)
	}
}

// TestASystemPushIsFencedOnTheSystemTier holds the system push to ADR-0752's
// fence (#422, ADR-0422): telemetry:push resolved on the system tier is the
// fence, its own scope is enough to see the system, a system outside both the
// push scope and the caller's system:read is a non-disclosing 404, and one
// readable but not pushable a truthful 403. The tiers do not leak into each
// other: push on a system confers nothing on its members, and push on a
// member nothing on its system.
func TestASystemPushIsFencedOnTheSystemTier(t *testing.T) {
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
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx,
		`insert into role (name, official, permissions, inherits) values ('push-only', false, '{telemetry:push}', '{}')`); err != nil {
		t.Fatalf("insert push-only role: %v", err)
	}
	all := scope.Set{All: true}
	ids := map[string]string{}
	loc, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	ids["hq"] = loc.ID
	hq := "hq"
	for _, name := range []string{"sys-a", "sys-b"} {
		s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: name, LocationName: &hq}, all, all)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		ids[name] = s.ID
	}
	sysA := "sys-a"
	m, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "member-a", SystemName: &sysA}, all, all, all, all)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	ids["member-a"] = m.ID
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(discardPublisher{})))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	bootstrapOwnerTok(t, ctx, gw)

	push := func(tok, kind, ref string, want int) {
		t.Helper()
		c.do(tok, http.MethodPost, "/telemetry:push", map[string]any{
			"owner":   map[string]any{"kind": kind, "ref": ref},
			"metrics": []map[string]any{{"name": "room-temperature", "value": 21.5}},
		}, want)
	}

	// A push-only integration on sys-a pushes there and learns nothing else.
	pushOnly := principalWithGrants(t, ctx, dsn, "push-only-a", []grant{{role: "push-only", scopeKind: "system", scopeID: ids["sys-a"]}})
	push(pushOnly, "system", "sys-a", http.StatusAccepted)
	push(pushOnly, "system", ids["sys-a"], http.StatusAccepted)
	push(pushOnly, "system", "sys-b", http.StatusNotFound)
	push(pushOnly, "system", "no-such-system", http.StatusNotFound)
	// Push on a system confers nothing on its member component.
	push(pushOnly, "component", "member-a", http.StatusNotFound)
	// Push confers no read.
	c.do(pushOnly, http.MethodGet, "/systems/sys-a", nil, http.StatusForbidden)

	// A wide read and a narrow push: the read decides only the refusal, a
	// truthful 403 on a system it can see but may not push to.
	wideNarrow := setupScopedPrincipal(t, ctx, dsn, "wide-read-narrow-push-sys",
		grantSpec{role: "viewer", scopeKind: "all"},
		grantSpec{role: "operator", scopeKind: "system", scopeID: ids["sys-b"]},
	)
	push(wideNarrow, "system", "sys-a", http.StatusForbidden)
	push(wideNarrow, "system", "sys-b", http.StatusAccepted)

	// Push on a member component confers nothing on its system.
	memberPusher := setupScopedViewer(t, ctx, dsn, "member-pusher", "operator", "component", ids["member-a"])
	push(memberPusher, "component", "member-a", http.StatusAccepted)
	push(memberPusher, "system", "sys-a", http.StatusNotFound)

	// A location-tier push grant reaches no system, even one placed there.
	placePusher := setupScopedViewer(t, ctx, dsn, "place-pusher", "push-only", "location", ids["hq"])
	push(placePusher, "system", "sys-a", http.StatusNotFound)

	// A viewer has the read floor but not telemetry:push.
	viewer := setupScopedViewer(t, ctx, dsn, "sys-viewer", "viewer", "system", ids["sys-a"])
	push(viewer, "system", "sys-a", http.StatusForbidden)
}
