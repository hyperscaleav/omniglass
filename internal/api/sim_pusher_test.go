package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The lab's simulated pusher (#1370) holds telemetry:push and nothing else,
// scoped to ONE system: it pushes there, and every other owner (another
// system, a component) is the non-disclosing 404. It reads its one owner off
// its own grant, which is how the simulator learns where to push.
func TestTheSimulatedPusherPushesOnlyToItsOneSystem(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	var target string
	for _, n := range []string{"booked-room", "other-room"} {
		s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: n}, all, all)
		if err != nil {
			t.Fatal(err)
		}
		if n == "booked-room" {
			target = s.ID
		}
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if err := devseed.GrantPusher(ctx, gw, "", "sim-booking-service", target); err != nil {
		t.Fatal(err)
	}
	if err := devseed.GrantPusher(ctx, gw, "", "sim-booking-service", target); err != nil {
		t.Fatalf("granting it again: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := gw.SetServiceToken(ctx, "", "sim-booking-service", hash, prefix); err != nil || !ok {
		t.Fatalf("set the pusher's token: %v, %v", ok, err)
	}
	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer busSrv.Shutdown()
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	var me struct {
		Permissions []string `json:"permissions"`
		Grants      []struct {
			ScopeKind string `json:"scope_kind"`
			ScopeID   string `json:"scope_id"`
		} `json:"grants"`
	}
	if err := json.Unmarshal(c.do(tok, http.MethodGet, "/auth/me", nil, http.StatusOK), &me); err != nil {
		t.Fatal(err)
	}
	if len(me.Grants) != 1 || me.Grants[0].ScopeKind != "system" || me.Grants[0].ScopeID != target {
		t.Fatalf("the pusher's grants = %+v, want one, on the booked room", me.Grants)
	}
	for _, p := range me.Permissions {
		if p != "telemetry:push" && p != "telemetry:read" {
			t.Errorf("the pusher holds %s; it holds telemetry:push and nothing else", p)
		}
	}

	push := func(kind, ref string, want int) {
		t.Helper()
		c.do(tok, http.MethodPost, "/telemetry:push", map[string]any{
			"owner":   map[string]any{"kind": kind, "ref": ref},
			"metrics": []map[string]any{{"name": "booking-count", "value": 3}},
		}, want)
	}
	push("system", target, http.StatusAccepted)
	push("system", "booked-room", http.StatusAccepted)
	push("system", "other-room", http.StatusNotFound)
	push("component", "panel-1", http.StatusNotFound)
	// It reads nothing: no system:read, so even its own system's page is
	// refused by the permission gate.
	c.do(tok, http.MethodGet, "/systems/booked-room", nil, http.StatusForbidden)
}
