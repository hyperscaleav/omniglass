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
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

type pushResp struct {
	Accepted struct {
		Metrics    int `json:"metrics"`
		Properties int `json:"properties"`
		Events     int `json:"events"`
		Logs       int `json:"logs"`
	} `json:"accepted"`
	Rejected []struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	} `json:"rejected"`
}

// TestTelemetryPushAPI drives the push lane end to end as a caller would: a real
// HTTP request, through a real bus, landing real rows. It is the proof that the
// route, the trusted subject, the consumer's subject branch, and the shared land()
// all line up, which no unit test can establish on its own.
//
// Every property name here is one the boot seed really registers, except the one
// that deliberately is not, so the rejection is real rather than illustrative.
func TestTelemetryPushAPI(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer busSrv.Shutdown()

	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// One batch mixing every lane, plus a name the metric catalog does not know:
	// the acceptance's mixed batch, each entry landing in its lane's table.
	suppliedTS := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	body := map[string]any{
		"owner":  map[string]any{"kind": "component", "ref": "bar-1"},
		"source": "webex-cloud",
		"metrics": []map[string]any{
			{"name": "icmp-rtt-avg", "value": 12.4, "ts": suppliedTS.Format(time.RFC3339)},
			{"name": "audio-level", "value": -42.5},
		},
		"properties": []map[string]any{
			{"name": "video-input", "value": "hdmi2"},
		},
		"events": []map[string]any{
			{"name": "call-started", "message": "call started, 4 participants"},
		},
		"logs": []map[string]any{
			{"message": "media channel renegotiated to 1080p30", "severity": "info", "facility": "media"},
		},
	}
	out := c.do(ownerTok, http.MethodPost, "/telemetry:push", body, http.StatusAccepted)
	var r pushResp
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("decode push response: %v", err)
	}
	if r.Accepted.Metrics != 1 || r.Accepted.Properties != 1 || r.Accepted.Events != 1 || r.Accepted.Logs != 1 {
		t.Fatalf("accepted = %+v, want 1 per lane", r.Accepted)
	}
	// Rejection is SYNCHRONOUS even though acceptance is not: that is the whole
	// reason the route validates before publishing.
	if len(r.Rejected) != 1 || r.Rejected[0].Name != "audio-level" {
		t.Fatalf("rejected = %+v, want exactly audio-level", r.Rejected)
	}

	// The lane the caller chose is the table the row landed in, and the supplied
	// per-sample ts survived ingest verbatim (the #594 ts rule).
	waitFor(t, "the metric row", func() bool {
		m, err := gw.LatestMetric(ctx, "bar-1", "icmp-rtt-avg")
		return err == nil && m != nil && m.Value == 12.4 && m.Source == "webex-cloud" && m.TS.Equal(suppliedTS)
	})
	waitFor(t, "the property row", func() bool {
		s, err := gw.LatestProperty(ctx, "bar-1", "video-input", "")
		return err == nil && s != nil && s.Value == "hdmi2"
	})
	waitFor(t, "the caught event", func() bool {
		evs, err := gw.ListComponentEvents(ctx, "bar-1", time.Hour, 50)
		if err != nil {
			return false
		}
		for _, e := range evs {
			if e.Key == "call-started" && e.Origin == "caught" && e.Source == "webex-cloud" {
				return true
			}
		}
		return false
	})
	waitFor(t, "the log line", func() bool {
		logs, err := gw.ListComponentLogs(ctx, "bar-1", time.Hour, 50)
		if err != nil {
			return false
		}
		for _, l := range logs {
			// The line set no source of its own, so it inherits the batch's.
			if l.Message == "media channel renegotiated to 1080p30" && l.Source == "webex-cloud" {
				return true
			}
		}
		return false
	})
	// An unregistered name reached no table at all.
	if m, err := gw.LatestMetric(ctx, "bar-1", "audio-level"); err != nil {
		t.Fatalf("latest audio-level: %v", err)
	} else if m != nil {
		t.Fatalf("reject-not-project failed: audio-level landed as %+v", m)
	}

	// SCHEMA VIOLATION IS A 422 (#594 acceptance): the name resolves, so the
	// mistake is in the data, and the request is refused whole rather than
	// half-landing a batch around it. video-input's seeded validation is an
	// input-name enum, so "composite" violates it; the error must name the
	// property so the refusal is actionable.
	resp := c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner": map[string]any{"kind": "component", "ref": "bar-1"},
		"properties": []map[string]any{
			{"name": "video-input", "value": "composite"},
		},
	}, http.StatusUnprocessableEntity)
	if !strings.Contains(string(resp), "video-input") {
		t.Fatalf("schema-violation 422 does not name the property: %s", resp)
	}
	// A value of the wrong JSON shape for the data_type (a number for a string
	// property) is the same refusal: the lane's type contract, not a kind guess.
	c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner": map[string]any{"kind": "component", "ref": "bar-1"},
		"properties": []map[string]any{
			{"name": "video-input", "value": 3},
		},
	}, http.StatusUnprocessableEntity)
	// And nothing landed for either refusal.
	if s, err := gw.LatestProperty(ctx, "bar-1", "video-input", ""); err != nil {
		t.Fatalf("latest video-input: %v", err)
	} else if s != nil && s.Value != "hdmi2" {
		t.Fatalf("a refused property value landed: %+v", s)
	}

	// A LANE MISMATCH is refused by name: the lane is the array, so a property
	// name pushed as a metric is unknown to the metric catalog even though the
	// property catalog knows it. The reason names the lane so the caller learns
	// which array to use.
	out = c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner": map[string]any{"kind": "component", "ref": "bar-1"},
		"metrics": []map[string]any{
			{"name": "video-input", "value": 3},    // a property name on the metric lane
			{"name": "icmp-rtt-avg", "value": 9.5}, // correct, must still land
		},
		"events": []map[string]any{
			{"name": "icmp-rtt-avg", "message": "x"}, // a metric name on the event lane
		},
	}, http.StatusAccepted)
	var mismatch pushResp
	if err := json.Unmarshal(out, &mismatch); err != nil {
		t.Fatalf("decode mismatch response: %v", err)
	}
	if mismatch.Accepted.Metrics != 1 || mismatch.Accepted.Events != 0 {
		t.Fatalf("accepted = %+v, want only the correctly-laned metric", mismatch.Accepted)
	}
	if len(mismatch.Rejected) != 2 {
		t.Fatalf("rejected = %+v, want both cross-lane names reported", mismatch.Rejected)
	}
	for _, r := range mismatch.Rejected {
		switch r.Name {
		case "video-input":
			if !strings.Contains(r.Reason, "metric") {
				t.Errorf("video-input rejection %q does not name the metric lane", r.Reason)
			}
		case "icmp-rtt-avg":
			if !strings.Contains(r.Reason, "event") {
				t.Errorf("icmp-rtt-avg rejection %q does not name the event lane", r.Reason)
			}
		default:
			t.Errorf("unexpected rejection %+v", r)
		}
	}

	// A batch of nothing but rejects publishes nothing, and says so.
	out = c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "component", "ref": "bar-1"},
		"metrics": []map[string]any{{"name": "not.a.real.property", "value": 1}},
	}, http.StatusAccepted)
	var allRejected pushResp
	_ = json.Unmarshal(out, &allRejected)
	if allRejected.Accepted.Metrics != 0 || len(allRejected.Rejected) != 1 {
		t.Fatalf("all-rejected batch = %+v, want 0 accepted and 1 rejected", allRejected)
	}

	// Validation: an empty batch and an unsupported owner arc are refused up front,
	// the arc because the lane derivations still hardcode component (#422).
	c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner": map[string]any{"kind": "component", "ref": "bar-1"},
	}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":      map[string]any{"kind": "system", "ref": "boardroom-a"},
		"properties": []map[string]any{{"name": "video-input", "value": "hdmi2"}},
	}, http.StatusUnprocessableEntity)

	// An oversized body is refused BEFORE the handler by Huma's 1 MiB request cap
	// (413), which is also what keeps a batch from ever exceeding NATS's 1 MiB max
	// payload: protobuf encodes more compactly than the JSON it came from, so no
	// separate encoded-size guard is needed. Pinned so that reasoning stays true.
	big := make([]map[string]any, 0, 200)
	long := ""
	for i := 0; i < 6000; i++ {
		long += "x"
	}
	for i := 0; i < 200; i++ {
		big = append(big, map[string]any{"message": long})
	}
	c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner": map[string]any{"kind": "component", "ref": "bar-1"},
		"logs":  big,
	}, http.StatusRequestEntityTooLarge)

	// SCOPE IS THE FENCE that makes a caller-declared owner trustworthy. A viewer
	// scoped to another component cannot push to bar-1, and learns nothing about
	// whether it exists (404, not 403).
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1"}, all, all, all, all); err != nil {
		t.Fatalf("create other component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var otherID string
	if err := conn.QueryRow(ctx, `select id from component where name = 'other-1'`).Scan(&otherID); err != nil {
		t.Fatalf("other id: %v", err)
	}
	// An operator holds telemetry:push; scope decides which owners it reaches.
	pusherTok := setupScopedViewer(t, ctx, dsn, "pusher", "operator", "component", otherID)
	c.do(pusherTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":      map[string]any{"kind": "component", "ref": "bar-1"},
		"properties": []map[string]any{{"name": "video-input", "value": "hdmi2"}},
	}, http.StatusNotFound)
	// Its own component is fine.
	c.do(pusherTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":      map[string]any{"kind": "component", "ref": "other-1"},
		"properties": []map[string]any{{"name": "video-input", "value": "hdmi2"}},
	}, http.StatusAccepted)

	// PRIVILEGE ESCALATION, pinned closed. A principal can easily hold a WIDE read
	// and a NARROW push: viewer over the whole estate plus operator on one component
	// is an ordinary shape. If the route fenced the write with the component:read
	// scope, this caller could push telemetry to any component it can see, which is
	// everything. The fence must be the scope that actually confers telemetry:push.
	wideReadNarrowPush := setupScopedPrincipal(t, ctx, dsn, "wide-read-narrow-push",
		grantSpec{role: "viewer", scopeKind: "all"},
		grantSpec{role: "operator", scopeKind: "component", scopeID: otherID},
	)
	c.do(wideReadNarrowPush, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":      map[string]any{"kind": "component", "ref": "bar-1"},
		"properties": []map[string]any{{"name": "video-input", "value": "hdmi2"}},
	}, http.StatusNotFound)
	// Its own push scope still works, so the fence is narrow, not broken.
	c.do(wideReadNarrowPush, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":      map[string]any{"kind": "component", "ref": "other-1"},
		"properties": []map[string]any{{"name": "video-input", "value": "hdmi2"}},
	}, http.StatusAccepted)

	// A viewer has the read floor but not telemetry:push.
	viewerTok := setupScopedViewer(t, ctx, dsn, "just-viewer", "viewer", "component", otherID)
	c.do(viewerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":      map[string]any{"kind": "component", "ref": "other-1"},
		"properties": []map[string]any{{"name": "video-input", "value": "hdmi2"}},
	}, http.StatusForbidden)

	// Unauthenticated is a 401.
	c.do("", http.MethodPost, "/telemetry:push", body, http.StatusUnauthorized)
}

// TestTelemetryPushWithoutBus proves the route refuses rather than pretending: a
// handler built with no publisher must not accept telemetry that nothing will ever
// consume.
func TestTelemetryPushWithoutBus(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1"}, scope.Set{All: true}, scope.Set{All: true}, scope.Set{All: true}, scope.Set{All: true}); err != nil {
		t.Fatalf("create component: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw)) // no WithTelemetryPublisher
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":      map[string]any{"kind": "component", "ref": "bar-1"},
		"properties": []map[string]any{{"name": "video-input", "value": "hdmi2"}},
	}, http.StatusServiceUnavailable)
}

// waitFor polls until cond holds or a deadline passes. Landing is asynchronous by
// design (the route publishes, the consumer writes), so a read straight after the
// 202 would race.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
