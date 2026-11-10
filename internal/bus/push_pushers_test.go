package bus_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/labfootprint"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestAMeasuredCorporateLabReadsItsPushLane is the scale lab's push lane end
// to end (#1743): the corporate plan seeded as the lab seeds it, each pusher
// it declares holding a token minted for its principal, and each one an
// emulator device of its profile pushing through the real push route. The
// lab's delivery read over the window carries push-lane rows, the boot
// settled by a push after it, every item stored, loss 0.
//
// Each site's meeting platform (#1747) is the push lane's other outside
// actor: subscribed, as the simulator subscribes it, to the webhook endpoint
// its seed minted, with every system of its site added as a room, its signed
// report lands through the real webhook route. The read over the reports'
// own window is push-lane rows at loss 0, the bound rooms stored.
func TestAMeasuredCorporateLabReadsItsPushLane(t *testing.T) {
	f := newDeliveryFixture(t)
	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := scenario.Resolve(m, cat, 20)
	if err != nil {
		t.Fatal(err)
	}
	// The plan's simulator secrets need a key; the seed opens the database
	// with one, as the lab seed does.
	seedGW, err := storage.NewPG(f.ctx, f.dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open the seed's gateway: %v", err)
	}
	defer seedGW.Close()
	// The platforms are simulated on a node, as a lab's are, and only a
	// marked lab hands a simulator its webhooks (ADR-1370).
	all := scope.Set{All: true}
	const node = "edge-1"
	if _, err := seedGW.CreateNode(f.ctx, "", storage.NodeSpec{Name: node}, all, all); err != nil {
		t.Fatal(err)
	}
	if err := seedGW.MarkLab(f.ctx); err != nil {
		t.Fatal(err)
	}
	res, err := devseed.SeedPlan(f.ctx, seedGW, "", plan, devseed.PlanOptions{Endpoints: true, NodeFor: func(string) string { return node }})
	if err != nil {
		t.Fatalf("seed the plan: %v", err)
	}
	if res.Pushers == 0 || res.Senders != len(plan.Sites) {
		t.Fatalf("the corporate plan seeded %d pushers and %d webhook senders, want pushers and one sender a site", res.Pushers, res.Senders)
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	// The webhook route checks each post against its sealed signing key, so
	// the API reads through the gateway that holds the key.
	apiSrv := httptest.NewServer(api.NewHandler(seedGW, api.WithTelemetryPublisher(srv)))
	defer apiSrv.Close()

	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	engine := sim.NewEngine(store, profiles.Templates())
	var snaps []sim.DeviceSnapshot
	tokens := map[string]string{}
	pushes := 0
	for _, site := range plan.Sites {
		for _, p := range site.Pushers {
			if p.Webhook {
				continue // a webhook sender signs its posts: below
			}
			tok, hash, prefix, err := auth.NewBearerToken()
			if err != nil {
				t.Fatal(err)
			}
			if ok, err := f.gw.SetServiceToken(f.ctx, "", p.Name, hash, prefix); err != nil || !ok {
				t.Fatalf("set %s's token: %v %v", p.Name, ok, err)
			}
			tokens[p.Name] = tok
			snaps = append(snaps, sim.DeviceSnapshot{DeviceID: p.Name, Hostname: p.Name, LoopbackIP: "127.0.0.1", ProfileName: p.Profile, Version: 1})
			pushes += len(p.Systems)
		}
	}
	if err := engine.LoadDevices(snaps); err != nil {
		t.Fatalf("load the pushers: %v", err)
	}
	devices := engine.Devices()
	if len(devices) != res.Pushers {
		t.Fatalf("the engine runs %d pushers, the seed granted %d", len(devices), res.Pushers)
	}
	for _, d := range devices {
		d.SetPushCredential(apiSrv.URL, tokens[d.DeviceID])
	}

	from := time.Now().UTC().Add(-time.Millisecond)
	for _, d := range devices {
		if err := d.PushOnce(f.ctx); err != nil {
			t.Fatalf("%s pushed: %v", d.DeviceID, err)
		}
	}
	f.waitPushReceipts(t, srv, pushes)
	to := time.Now().UTC()
	time.Sleep(5 * time.Millisecond)

	// The platforms' window: each subscribed and given its site's rooms as
	// the simulator does from its device list, then its report posted.
	hooks, err := seedGW.NodeSimWebhooks(f.ctx, node)
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != res.Senders {
		t.Fatalf("the node holds %d webhooks to post, want the %d platforms", len(hooks), res.Senders)
	}
	var platforms []sim.DeviceSnapshot
	for comp := range hooks {
		platforms = append(platforms, sim.DeviceSnapshot{DeviceID: comp, Hostname: comp, LoopbackIP: "127.0.0.1", ProfileName: "ucc-platform", Version: 1})
	}
	senders := sim.NewEngine(store, profiles.Templates())
	if err := senders.LoadDevices(platforms); err != nil {
		t.Fatalf("load the platforms: %v", err)
	}
	hookFrom := time.Now().UTC()
	rooms := 0
	for _, d := range senders.Devices() {
		h := hooks[d.DeviceID]
		sub, _ := json.Marshal(map[string]string{"url": apiSrv.URL + h.Path, "scheme": h.Scheme, "secret": h.Secret})
		operate(t, d, "/api/webhooks", string(sub))
		for _, id := range h.Identities {
			body, _ := json.Marshal(map[string]string{"id": id})
			operate(t, d, "/api/rooms", string(body))
		}
		rooms += len(h.Identities)
		operate(t, d, "/api/rooms/report", "{}")
	}
	if rooms != plan.Totals.Systems {
		t.Fatalf("the platforms speak for %d rooms, want every one of the plan's %d systems", rooms, plan.Totals.Systems)
	}
	f.waitPushReceipts(t, srv, pushes+len(hooks))
	hookTo := time.Now().UTC()
	time.Sleep(5 * time.Millisecond)

	// One push after the windows settles the server's boot, as the lab's
	// next cadence does.
	if err := devices[0].PushOnce(f.ctx); err != nil {
		t.Fatalf("the settling push: %v", err)
	}
	settle := len(plan.Sites[0].Pushers[0].Systems)
	f.waitPushReceipts(t, srv, pushes+len(hooks)+settle)

	rows := pushRows(t, f, delivery.Window{From: from, To: to})
	if len(rows) == 0 {
		t.Fatal("the delivery read carries no push-lane row")
	}
	for _, r := range rows {
		if r.Lane != labfootprint.LanePush {
			t.Fatalf("row %+v is not the push lane's", r)
		}
	}
	wantRow(t, rows, delivery.Metric, uint64(pushes), uint64(pushes), 0, map[delivery.Outcome]uint64{delivery.Stored: uint64(pushes)})
	wantRow(t, rows, delivery.Property, uint64(pushes), uint64(pushes), 0, map[delivery.Outcome]uint64{delivery.Stored: uint64(pushes)})

	// The reports carry a state for every room on the account: each bound
	// room stored, the platform's own demo rooms in the unbound inbox, and
	// nothing lost.
	hookRows := pushRows(t, f, delivery.Window{From: hookFrom, To: hookTo})
	stored := uint64(0)
	for _, r := range hookRows {
		if r.In == 0 {
			continue // a kind the reports do not carry reads no ratio
		}
		if r.Loss == nil || *r.Loss != 0 {
			t.Errorf("webhook row %+v, want loss 0", r)
		}
		stored += r.Outcomes[delivery.Stored]
	}
	if len(hookRows) == 0 || stored < uint64(rooms) {
		t.Fatalf("the reports' window reads %+v, want push-lane rows storing at least one state for each of %d rooms", hookRows, rooms)
	}
}

// operate calls one of a simulated service's routes, as its admin would.
func operate(t *testing.T, d *sim.Device, path, body string) {
	t.Helper()
	res := d.Operate(sim.Operation{Kind: "http", Method: http.MethodPost, Path: path, Body: body})
	if res.Error != "" || res.Status >= 300 {
		t.Fatalf("%s %s: status %d %s", d.Hostname, path, res.Status, res.Error)
	}
}
