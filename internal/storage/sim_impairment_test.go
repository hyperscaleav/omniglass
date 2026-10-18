package storage_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/impair"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSimImpairment proves an impairment is a persisted, audited fact about
// a simulated component that reaches its node's simulator: set, read,
// carried on the device list, refused when the network could not apply it,
// and cleared back to a healthy device.
func TestSimImpairment(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dsp"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	comp, node := "dsp", "edge"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &comp, Node: &node,
		Params: []byte(`{"target":"127.60.0.5:1710"}`)}, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var id string
	if err := conn.QueryRow(ctx, `select id from component where name = 'dsp'`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	if got, err := gw.GetSimImpairment(ctx, id); err != nil || got != (impair.Impairment{}) {
		t.Fatalf("a fresh device: %+v (%v), want healthy", got, err)
	}
	want := impair.Impairment{LatencyMs: 250, JitterMs: 50, LossPct: 2.5}
	if got, err := gw.SetSimImpairment(ctx, "", id, want); err != nil || got != want {
		t.Fatalf("set returned %+v (%v), want %+v", got, err, want)
	}
	if got, err := gw.GetSimImpairment(ctx, id); err != nil || got != want {
		t.Fatalf("read back %+v (%v), want %+v", got, err, want)
	}
	devs, err := gw.NodeSimDevices(ctx, "edge", all)
	if err != nil || len(devs) != 1 || devs[0].Impairment == nil || *devs[0].Impairment != want {
		t.Fatalf("device list = %+v (%v), want the impairment carried", devs, err)
	}

	if _, err := gw.SetSimImpairment(ctx, "", id, impair.Impairment{LatencyMs: 10, JitterMs: 20}); !errors.Is(err, storage.ErrSimImpairmentInvalid) {
		t.Fatalf("jitter above latency: err %v, want ErrSimImpairmentInvalid", err)
	}

	// Loss is stored to hundredths, and what is returned is what is stored.
	if got, err := gw.SetSimImpairment(ctx, "", id, impair.Impairment{LossPct: 2.555}); err != nil || got.LossPct != 2.56 {
		t.Fatalf("2.555%% loss returned %+v (%v), want 2.56", got, err)
	}
	if got, _ := gw.GetSimImpairment(ctx, id); got.LossPct != 2.56 {
		t.Fatalf("2.555%% loss read back %v, want 2.56", got.LossPct)
	}

	// A loss that rounds to nothing is a healthy device: no row.
	if _, err := gw.SetSimImpairment(ctx, "", id, impair.Impairment{LossPct: 0.004}); err != nil {
		t.Fatal(err)
	}
	if devs, _ := gw.NodeSimDevices(ctx, "edge", all); devs[0].Impairment != nil {
		t.Fatalf("a cleared device still carries %+v", devs[0].Impairment)
	}
	var audits int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'sim_impairment' and resource_id = $1`, id).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 3 {
		t.Fatalf("%d audit rows for three writes", audits)
	}
}

// Two holders of the sim address lock run one after the other, never at
// once: the second waits for the first to finish.
func TestSimAddressLockSerializes(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	inside, overlap := make(chan struct{}, 2), false
	var active int32
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			done <- gw.WithSimAddressLock(ctx, func() error {
				if atomic.AddInt32(&active, 1) > 1 {
					overlap = true
				}
				inside <- struct{}{}
				time.Sleep(150 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return nil
			})
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if overlap || len(inside) != 2 {
		t.Fatalf("overlap %v, entered %d times", overlap, len(inside))
	}
}
