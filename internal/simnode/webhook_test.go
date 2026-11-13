//go:build linux

package simnode

import (
	"sort"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The simulator subscribes a simulated service to its webhook endpoint
// (ADR-1370): a device the server hands a subscription is told, through its
// own subscribe route, to post to the server URL the simulator reaches plus
// the endpoint's path, signed as the endpoint expects. Once per device
// instance and subscription: a re-pull with nothing changed sends nothing,
// and a restarted device (whose state began again from its defaults) is
// subscribed afresh.
func TestTheSimulatorSubscribesAServiceToItsWebhookEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("serves a simulated device")
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	// A profile that posts webhooks, found by its subscribe route.
	var sender []string
	for name, p := range store.Devices {
		if p.Webhook != nil {
			sender = append(sender, name)
		}
	}
	if len(sender) == 0 {
		t.Fatal("no profile declares a webhook subscribe route")
	}
	sort.Strings(sender)
	ip := simtest.ClaimLoopback(t)
	snap := sim.DeviceSnapshot{DeviceID: "ucc", Hostname: "ucc", LoopbackIP: ip, ProfileName: sender[0], Version: 1}
	if err := engine.Sync([]sim.DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	listed := []collection.SimDevice{{ComponentID: "ucc", Name: "ucc", Address: ip,
		Webhook: &collection.SimWebhook{Path: "/api/v1/webhooks/TOKEN", Scheme: "hmac", Secret: "lab-key"}}}

	calls := 0
	s := newSubscriber("http://server:8080", store)
	op := s.operate
	s.operate = func(d *sim.Device, o sim.Operation) sim.OperationResult { calls++; return op(d, o) }
	if errs := s.reconcile(listed, engine.Devices()); len(errs) != 0 {
		t.Fatalf("subscribe: %v", errs)
	}
	hook := func() map[string]any {
		cfg, _ := engine.Devices()[0].State()["configuration"].(map[string]any)
		h, _ := cfg["webhook"].(map[string]any)
		return h
	}
	if h := hook(); h["target_url"] != "http://server:8080/api/v1/webhooks/TOKEN" || h["scheme"] != "hmac" || h["secret"] != "lab-key" {
		t.Fatalf("the service's webhook = %v, want it posting to the endpoint, hmac-signed with the lab key", h)
	}
	if errs := s.reconcile(listed, engine.Devices()); len(errs) != 0 || calls != 1 {
		t.Fatalf("a re-pull with nothing changed: %d subscribe calls (%v), want 1", calls, errs)
	}

	// The simulator restarts: the device's state begins again from its
	// defaults, and it is subscribed again.
	engine.StopAll()
	engine = sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	if err := engine.Sync([]sim.DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	if h := hook(); h["target_url"] != "" {
		t.Fatalf("a restarted service kept %v; the test needs it to start from its defaults", h["target_url"])
	}
	if errs := s.reconcile(listed, engine.Devices()); len(errs) != 0 || calls != 2 {
		t.Fatalf("after a restart: %d subscribe calls (%v), want 2", calls, errs)
	}
	if h := hook(); h["target_url"] != "http://server:8080/api/v1/webhooks/TOKEN" {
		t.Fatalf("the restarted service's webhook = %v, want it subscribed again", h)
	}

	// No subscription listed: nothing is sent.
	listed[0].Webhook = nil
	if errs := s.reconcile(listed, engine.Devices()); len(errs) != 0 || calls != 2 {
		t.Fatalf("with no subscription: %d calls (%v), want none more", calls, errs)
	}
}

// A platform account speaks for every room bound on its webhook endpoint
// (#1747): the server hands the simulator those bound values beside the
// subscription, and the simulator adds each to the service through its
// profile's provision route, as the account's admin would, so its periodic
// report names every one. A room the service already knows keeps its state,
// a re-pull with nothing changed sends nothing, and a newly bound room is
// added on the next pull.
func TestTheSimulatorAddsEveryBoundRoomToItsPlatform(t *testing.T) {
	if testing.Short() {
		t.Skip("serves a simulated device")
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	// The platform is the profile whose webhook face posts to the ucc-webhook
	// driver and provisions its rooms, found by that face rather than named.
	platform := ""
	for name, p := range store.Devices {
		if p.Webhook != nil && p.Webhook.Provision != "" && p.Webhook.Driver == "ucc-webhook" {
			if platform != "" {
				t.Fatalf("profiles %s and %s both provision rooms for ucc-webhook, want one", platform, name)
			}
			platform = name
		}
	}
	if platform == "" {
		t.Fatal("no profile's webhook face names a provision route and the ucc-webhook driver")
	}
	engine := sim.NewEngine(store, profiles.Templates())
	engine.SetPortMap(sim.EphemeralPorts)
	t.Cleanup(engine.StopAll)
	ip := simtest.ClaimLoopback(t)
	snap := sim.DeviceSnapshot{DeviceID: "plat", Hostname: "plat", LoopbackIP: ip, ProfileName: platform, Product: platform, Version: 1}
	if err := engine.Sync([]sim.DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	listed := []collection.SimDevice{{ComponentID: "plat", Name: "plat", Address: ip,
		Webhook: &collection.SimWebhook{Path: "/api/v1/webhooks/TOKEN", Scheme: "hmac", Secret: "lab-key",
			Identities: []string{"hq-conf-1", "zr-huddle-north"}}}}
	calls := 0
	s := newSubscriber("http://server:8080", store)
	op := s.operate
	s.operate = func(d *sim.Device, o sim.Operation) sim.OperationResult { calls++; return op(d, o) }
	if errs := s.reconcile(listed, engine.Devices()); len(errs) != 0 {
		t.Fatalf("subscribe: %v", errs)
	}
	room := func(id string) map[string]any {
		st, _ := engine.Devices()[0].State()["status"].(map[string]any)
		rooms, _ := st["rooms"].(map[string]any)
		r, _ := rooms[id].(map[string]any)
		return r
	}
	if r := room("hq-conf-1"); r == nil || r["state"] != "idle" {
		t.Fatalf("bound room hq-conf-1 = %v, want it on the account, idle", r)
	}
	if r := room("zr-huddle-north"); r["state"] != "in-call" || r["meeting"] != "m-1001" {
		t.Fatalf("a room the account already knew = %v, want its call kept", r)
	}
	if calls != 3 {
		t.Fatalf("%d calls, want the subscribe and one per bound room", calls)
	}
	if errs := s.reconcile(listed, engine.Devices()); len(errs) != 0 || calls != 3 {
		t.Fatalf("a re-pull with nothing changed: %d calls (%v), want 3", calls, errs)
	}
	listed[0].Webhook.Identities = append(listed[0].Webhook.Identities, "hq-conf-2")
	if errs := s.reconcile(listed, engine.Devices()); len(errs) != 0 {
		t.Fatalf("a newly bound room: %v", errs)
	}
	if r := room("hq-conf-2"); r == nil || r["state"] != "idle" {
		t.Fatalf("newly bound room hq-conf-2 = %v, want it added", r)
	}
}
