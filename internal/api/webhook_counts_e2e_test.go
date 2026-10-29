package api_test

import (
	"bytes"
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
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestAWebhookEndpointReportsItsOwnPosts drives #1369's push half over the
// real wire: the simulated UCC service posts its signed webhooks to the real
// API. A webhook-fed component has no node and no poll, yet each report
// window lands its endpoint's accepted posts as its own series, then one
// zero once the stream goes quiet, and a window with refused posts (a bad
// signature) lands their count.
func TestAWebhookEndpointReportsItsOwnPosts(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "ucc-1"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	busSrv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer busSrv.Shutdown()
	srv := httptest.NewServer(api.NewHandler(gw, api.WithTelemetryPublisher(busSrv), api.WithCollectionReportEvery(500*time.Millisecond)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	const key = "ucc-counts-signing-key"
	c.do(owner, http.MethodPost, "/secrets", map[string]any{
		"name": "ucc-signing", "secret_type": "hmac-key", "owner_kind": "component", "owner": "ucc-1",
		"fields": map[string]string{"key": key},
	}, http.StatusCreated)
	var ep struct {
		Name    string `json:"name"`
		Webhook *struct {
			Secret string `json:"secret"`
		} `json:"webhook"`
	}
	mustJSON(t, c.do(owner, http.MethodPost, "/endpoints", map[string]any{
		"component": "ucc-1", "driver": "ucc-webhook", "inputs": map[string]string{"signing-key": "ucc-signing"},
	}, http.StatusCreated), &ep)
	var revealed struct {
		Fields map[string]string `json:"fields"`
	}
	mustJSON(t, c.do(owner, http.MethodPost, "/secrets/"+strings.ReplaceAll(ep.Webhook.Secret, "/", "%2F")+":reveal", nil, http.StatusOK), &revealed)
	hookURL := srv.URL + "/api/v1/webhooks/" + revealed.Fields["token"]

	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	uccProfile := ""
	for name, p := range store.Devices {
		if p.Model == "UCC Platform" {
			uccProfile = name
		}
	}
	if err := engine.Sync([]sim.DeviceSnapshot{{DeviceID: "ucc", Hostname: "ucc", LoopbackIP: simtest.ClaimLoopback(t), ProfileName: uccProfile, Version: 1}}); err != nil {
		t.Fatal(err)
	}
	ucc := engine.Devices()[0]
	operate := func(path string, body any) {
		t.Helper()
		b, _ := json.Marshal(body)
		if res := ucc.Operate(sim.Operation{Kind: "http", Method: "POST", Path: path, Body: string(b)}); res.Status != 200 {
			t.Fatalf("POST %s on the ucc service = %+v", path, res)
		}
	}
	subscribe := func(fault string) {
		operate("/api/webhooks", map[string]string{"url": hookURL, "scheme": "hmac", "secret": key, "fault": fault})
	}
	latest := func(k string) (float64, bool) {
		m, err := gw.LatestMetricInstance(ctx, "ucc-1", k, ep.Name)
		if err != nil || m == nil {
			return 0, false
		}
		return m.Value, true
	}
	waitCount := func(what, k string, ok func(float64) bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if v, landed := latest(k); landed && ok(v) {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		v, landed := latest(k)
		t.Fatalf("%s: %s on ucc-1[%s] = %v (landed %v)", what, k, ep.Name, v, landed)
	}

	// Two signed posts land: the window counts them, then the quiet window
	// after reports zero.
	subscribe("")
	operate("/api/meetings/start", map[string]string{"id": "m-1", "room": "huddle"})
	operate("/api/meetings/end", map[string]string{"id": "m-1"})
	waitCount("the posts counted", writerclass.WebhookPostCount, func(v float64) bool { return v >= 1 })
	waitCount("the quiet window as zero", writerclass.WebhookPostCount, func(v float64) bool { return v == 0 })
	if v, landed := latest(writerclass.WebhookRefusalCount); landed {
		t.Errorf("no post was refused, yet a refusal count of %v landed", v)
	}

	// A bad signature is refused, and its window counts the refusal.
	subscribe("bad-signature")
	operate("/api/meetings/start", map[string]string{"id": "m-2"})
	waitCount("the refusal counted", writerclass.WebhookRefusalCount, func(v float64) bool { return v >= 1 })
}
