package bus_test

import (
	"bytes"
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
	res, err := devseed.SeedPlan(f.ctx, seedGW, "", plan, devseed.PlanOptions{Endpoints: true})
	if err != nil {
		t.Fatalf("seed the plan: %v", err)
	}
	if res.Pushers == 0 {
		t.Fatal("the corporate plan declares no pusher")
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(f.gw, api.WithTelemetryPublisher(srv)))
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
	to := time.Now().UTC()
	time.Sleep(5 * time.Millisecond)
	// One push after the window settles the server's boot, as the lab's
	// next cadence does.
	if err := devices[0].PushOnce(f.ctx); err != nil {
		t.Fatalf("the settling push: %v", err)
	}
	settle := len(plan.Sites[0].Pushers[0].Systems)
	f.waitPushReceipts(t, srv, pushes+settle)

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
}
