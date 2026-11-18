package bus_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestEndpointObservationBracketsTheOutpostBetweenTwoSimReads proves the lab's
// endpoint observation over a real in-process NATS server (#1639): the server
// reads the simulator, then the outpost, then the simulator again, each on the
// outpost's own grant; with no simulator it says so, and with a simulator but
// no outpost answering it says the outpost is absent, both at once.
func TestEndpointObservationBracketsTheOutpostBetweenTwoSimReads(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	all := scope.Set{All: true}
	if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "outpost-a"}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "outpost-a", hashHex("secret-a"), all); err != nil {
		t.Fatal(err)
	}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	if _, err := srv.EndpointObservation(ctx, "outpost-a"); !errors.Is(err, bus.ErrNoSimulator) {
		t.Fatalf("with no simulator: err = %v, want ErrNoSimulator", err)
	}

	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo("outpost-a", "secret-a"),
		nats.CustomInboxPrefix(collection.InboxPrefix("outpost-a")))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	var mu sync.Mutex
	var order []string
	simReads := 0
	if _, err := nc.Subscribe(collection.SimobsSubject("outpost-a"), func(m *nats.Msg) {
		mu.Lock()
		simReads++
		order = append(order, "sim")
		n := simReads
		mu.Unlock()
		b, _ := json.Marshal(collection.SimObservation{Outpost: "outpost-a", At: time.Now(),
			Protocols: []collection.SimProtocolObservation{{Protocol: "http", Received: int64(10 * n)}}})
		_ = m.Respond(b)
	}); err != nil {
		t.Fatal(err)
	}
	_ = nc.Flush()

	start := time.Now()
	if _, err := srv.EndpointObservation(ctx, "outpost-a"); !errors.Is(err, bus.ErrOutpostAbsent) {
		t.Fatalf("with no outpost answering: err = %v, want ErrOutpostAbsent", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("an absent outpost took %v to say so, want at once", time.Since(start))
	}

	if _, err := nc.Subscribe(collection.RequestTallySubject("outpost-a"), func(m *nats.Msg) {
		mu.Lock()
		order = append(order, "outpost")
		mu.Unlock()
		tally := collection.NewRequestTally()
		tally.Begin(collection.TallyHTTP).End(collection.TallyAnswered)
		snap := tally.Snapshot(time.Now())
		snap.Outpost = "outpost-a"
		b, _ := json.Marshal(snap)
		_ = m.Respond(b)
	}); err != nil {
		t.Fatal(err)
	}
	_ = nc.Flush()
	mu.Lock()
	order, simReads = nil, 0
	mu.Unlock()

	obs, err := srv.EndpointObservation(ctx, "outpost-a")
	if err != nil {
		t.Fatalf("observation: %v", err)
	}
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if len(got) != 3 || got[0] != "sim" || got[1] != "outpost" || got[2] != "sim" {
		t.Fatalf("read order = %v, want sim, outpost, sim", got)
	}
	if obs.SimBefore.Protocol("http").Received != 10 || obs.SimAfter.Protocol("http").Received != 20 {
		t.Fatalf("sim reads = %+v then %+v, want the first then the second", obs.SimBefore, obs.SimAfter)
	}
	if obs.Outpost.Outpost != "outpost-a" || len(obs.Outpost.Transports) != 1 || obs.Outpost.Transports[0].Answered != 1 {
		t.Fatalf("outpost read = %+v", obs.Outpost)
	}
}
