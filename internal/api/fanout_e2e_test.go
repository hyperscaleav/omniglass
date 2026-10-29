package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestFanOutEndToEnd drives the fan-out over the real wire (#1282): the
// simulated UCC platform, served in process, posts its signed room events
// over HTTP to the real webhook route, the real bus carries each batch, and
// the shipped ucc-webhook driver's room-naming listeners land each room's
// call state on the system its room id is bound to (through the binding
// route). A batched report lands every bound room in one post; a room nobody
// bound lands on no system and reads in the conduit's unbound inbox, the
// post's raw line still on the conduit; binding it over the API makes its
// next value land on its system.
func TestFanOutEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	owner, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "meetings-cloud"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"boardroom-a", "huddle-north", "annex"} {
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer busSrv.Shutdown()
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	const key = "ucc-fanout-signing-key"
	c.do(owner, http.MethodPost, "/secrets", map[string]any{
		"name": "ucc-signing", "secret_type": "hmac-key", "owner_kind": "component", "owner": "meetings-cloud",
		"fields": map[string]string{"key": key},
	}, http.StatusCreated)
	var ep webhookEndpoint
	mustJSON(t, c.do(owner, http.MethodPost, "/endpoints", map[string]any{
		"component": "meetings-cloud", "driver": "ucc-webhook", "inputs": map[string]string{"signing-key": "ucc-signing"},
	}, http.StatusCreated), &ep)
	var r struct {
		Fields map[string]string `json:"fields"`
	}
	mustJSON(t, c.do(owner, http.MethodPost, "/secrets/"+strings.ReplaceAll(ep.Webhook.Secret, "/", "%2F")+":reveal", nil, http.StatusOK), &r)
	hookURL := srv.URL + "/api/v1/webhooks/" + r.Fields["token"]

	bind := func(room, system string) {
		t.Helper()
		c.do(owner, http.MethodPut, "/components/meetings-cloud/identity-bindings/ucc-room-id/"+room,
			map[string]string{"owner_kind": "system", "owner": system}, http.StatusOK)
	}
	bind("zr-boardroom-a", "boardroom-a")
	bind("zr-huddle-north", "huddle-north")

	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	if err := engine.Sync([]sim.DeviceSnapshot{{DeviceID: "ucc-plat", Hostname: "ucc-plat", LoopbackIP: simtest.ClaimLoopback(t), ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatalf("serve the ucc platform: %v", err)
	}
	plat := engine.Devices()[0]
	operate := func(path string, body any) {
		t.Helper()
		b, _ := json.Marshal(body)
		if res := plat.Operate(sim.Operation{Kind: "http", Method: "POST", Path: path, Body: string(b)}); res.Status != 200 {
			t.Fatalf("POST %s on the ucc platform = %+v", path, res)
		}
	}
	operate("/api/webhooks", map[string]string{"url": hookURL, "scheme": "hmac", "secret": key})

	callState := func(system string) string {
		var out struct {
			Properties []struct {
				Name           string `json:"property_type_name"`
				Reconciliation *struct {
					Is json.RawMessage `json:"is"`
				} `json:"reconciliation"`
			} `json:"properties"`
		}
		_ = json.Unmarshal(c.do(owner, http.MethodGet, "/systems/"+system+"/properties", nil, http.StatusOK), &out)
		for _, p := range out.Properties {
			if p.Name == "call-state" && p.Reconciliation != nil {
				var s string
				_ = json.Unmarshal(p.Reconciliation.Is, &s)
				return s
			}
		}
		return ""
	}

	// One room's meeting starts: its system reads in a call.
	operate("/api/rooms/meetings/start", map[string]any{"room": "zr-boardroom-a", "id": "m-7"})
	waitFor(t, "boardroom-a in a call", func() bool { return callState("boardroom-a") == "active" })

	// The batched report: every bound room lands from one post; the annex
	// room (bound to nothing) lands on no system and waits in the inbox.
	operate("/api/rooms/report", map[string]any{})
	waitFor(t, "huddle-north from the report", func() bool { return callState("huddle-north") == "active" })
	var inbox struct {
		Identities []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
			Count int64  `json:"count"`
		} `json:"identities"`
	}
	waitFor(t, "the annex room in the inbox", func() bool {
		mustJSON(t, c.do(owner, http.MethodGet, "/components/meetings-cloud/unbound-identities", nil, http.StatusOK), &inbox)
		for _, u := range inbox.Identities {
			if u.Key == "ucc-room-id" && u.Value == "zr-annex-1" {
				return true
			}
		}
		return false
	})
	if callState("annex") != "" {
		t.Error("the unbound room landed on a system")
	}
	var logs struct {
		Logs []struct {
			Message string `json:"message"`
		} `json:"logs"`
	}
	mustJSON(t, c.do(owner, http.MethodGet, "/components/meetings-cloud/logs", nil, http.StatusOK), &logs)
	raw := false
	for _, l := range logs.Logs {
		raw = raw || strings.Contains(l.Message, "rooms.state")
	}
	if !raw {
		t.Error("the report's raw line is not on the conduit")
	}

	// Bound over the API, the annex room's next report lands on its system.
	bind("zr-annex-1", "annex")
	operate("/api/rooms/report", map[string]any{})
	waitFor(t, "annex from the next report", func() bool { return callState("annex") == "active" })
}
