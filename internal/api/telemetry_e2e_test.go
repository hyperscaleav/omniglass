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
		Samples int `json:"samples"`
		Logs    int `json:"logs"`
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1"}, all); err != nil {
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

	// One batch spanning both arrays and all three sample destinations, plus a name
	// the registry does not know.
	body := map[string]any{
		"owner":  map[string]any{"kind": "component", "ref": "bar-1"},
		"source": "webex-cloud",
		"samples": []map[string]any{
			{"name": "video.input", "text": "hdmi2"},
			{"name": "icmp.rtt_avg", "number": 12.4},
			{"name": "call.started", "text": "call started, 4 participants"},
			{"name": "audio.level", "number": -42.5},
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
	if r.Accepted.Samples != 3 || r.Accepted.Logs != 1 {
		t.Fatalf("accepted = %+v, want 3 samples and 1 log", r.Accepted)
	}
	// Rejection is SYNCHRONOUS even though acceptance is not: that is the whole
	// reason the route validates before publishing.
	if len(r.Rejected) != 1 || r.Rejected[0].Name != "audio.level" {
		t.Fatalf("rejected = %+v, want exactly audio.level", r.Rejected)
	}

	// The registry, not the caller, decided which table each name landed in.
	waitFor(t, "the metric row", func() bool {
		m, err := gw.LatestMetric(ctx, "bar-1", "icmp.rtt_avg")
		return err == nil && m != nil && m.Value == 12.4 && m.Source == "webex-cloud"
	})
	waitFor(t, "the state row", func() bool {
		s, err := gw.LatestState(ctx, "bar-1", "video.input", "")
		return err == nil && s != nil && s.Value == "hdmi2"
	})
	waitFor(t, "the caught event", func() bool {
		evs, err := gw.ListComponentEvents(ctx, "bar-1", time.Now().Add(-time.Hour), 50)
		if err != nil {
			return false
		}
		for _, e := range evs {
			if e.Key == "call.started" && e.Origin == "caught" && e.Source == "webex-cloud" {
				return true
			}
		}
		return false
	})
	waitFor(t, "the log line", func() bool {
		logs, err := gw.ListComponentLogs(ctx, "bar-1", time.Now().Add(-time.Hour), 50)
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
	if m, err := gw.LatestMetric(ctx, "bar-1", "audio.level"); err != nil {
		t.Fatalf("latest audio.level: %v", err)
	} else if m != nil {
		t.Fatalf("reject-not-project failed: audio.level landed as %+v", m)
	}

	// KIND MISMATCH is reported, not silently dropped. The name resolves, so the
	// old check (name only) passed it, returned 202 with an empty rejected list,
	// and the batch then died in the consumer's numericValue/stringValue. That is
	// precisely the mistake a human makes by hand, and it was the one case the
	// synchronous-rejection report missed.
	out = c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner": map[string]any{"kind": "component", "ref": "bar-1"},
		"samples": []map[string]any{
			{"name": "video.input", "number": 3},     // state property, sent a number
			{"name": "icmp.rtt_avg", "text": "fast"}, // metric property, sent text
			{"name": "call.started", "number": 1},    // event type, sent a number
			{"name": "icmp.rtt_avg", "number": 9.5},  // correct, must still land
		},
	}, http.StatusAccepted)
	var mismatch pushResp
	if err := json.Unmarshal(out, &mismatch); err != nil {
		t.Fatalf("decode mismatch response: %v", err)
	}
	if mismatch.Accepted.Samples != 1 {
		t.Fatalf("accepted = %d, want only the correctly-typed sample", mismatch.Accepted.Samples)
	}
	if len(mismatch.Rejected) != 3 {
		t.Fatalf("rejected = %+v, want all three mismatches reported", mismatch.Rejected)
	}
	// The reason has to be actionable: it must name the kind the caller should
	// have sent, not merely say "invalid".
	for _, r := range mismatch.Rejected {
		switch r.Name {
		case "video.input", "call.started":
			if !strings.Contains(r.Reason, "text") {
				t.Errorf("%s rejection %q does not tell the caller to send text", r.Name, r.Reason)
			}
		case "icmp.rtt_avg":
			if !strings.Contains(r.Reason, "number") {
				t.Errorf("%s rejection %q does not tell the caller to send a number", r.Name, r.Reason)
			}
		default:
			t.Errorf("unexpected rejection %+v", r)
		}
	}

	// A batch of nothing but rejects publishes nothing, and says so.
	out = c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "component", "ref": "bar-1"},
		"samples": []map[string]any{{"name": "not.a.real.property", "number": 1}},
	}, http.StatusAccepted)
	var allRejected pushResp
	_ = json.Unmarshal(out, &allRejected)
	if allRejected.Accepted.Samples != 0 || len(allRejected.Rejected) != 1 {
		t.Fatalf("all-rejected batch = %+v, want 0 accepted and 1 rejected", allRejected)
	}

	// Validation: an empty batch and an unsupported owner arc are refused up front,
	// the arc because deriveSamples still hardcodes component (#422).
	c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner": map[string]any{"kind": "component", "ref": "bar-1"},
	}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "system", "ref": "boardroom-a"},
		"samples": []map[string]any{{"name": "video.input", "text": "hdmi2"}},
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-1"}, all); err != nil {
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
		"owner":   map[string]any{"kind": "component", "ref": "bar-1"},
		"samples": []map[string]any{{"name": "video.input", "text": "hdmi2"}},
	}, http.StatusNotFound)
	// Its own component is fine.
	c.do(pusherTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "component", "ref": "other-1"},
		"samples": []map[string]any{{"name": "video.input", "text": "hdmi2"}},
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
		"owner":   map[string]any{"kind": "component", "ref": "bar-1"},
		"samples": []map[string]any{{"name": "video.input", "text": "hdmi2"}},
	}, http.StatusNotFound)
	// Its own push scope still works, so the fence is narrow, not broken.
	c.do(wideReadNarrowPush, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "component", "ref": "other-1"},
		"samples": []map[string]any{{"name": "video.input", "text": "hdmi2"}},
	}, http.StatusAccepted)

	// A viewer has the read floor but not telemetry:push.
	viewerTok := setupScopedViewer(t, ctx, dsn, "just-viewer", "viewer", "component", otherID)
	c.do(viewerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "component", "ref": "other-1"},
		"samples": []map[string]any{{"name": "video.input", "text": "hdmi2"}},
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1"}, scope.Set{All: true}); err != nil {
		t.Fatalf("create component: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw)) // no WithTelemetryPublisher
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	c.do(ownerTok, http.MethodPost, "/telemetry:push", map[string]any{
		"owner":   map[string]any{"kind": "component", "ref": "bar-1"},
		"samples": []map[string]any{{"name": "video.input", "text": "hdmi2"}},
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
