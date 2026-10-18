package storage_test

import (
	"context"
	"errors"
	"testing"

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
	if err := gw.SetSimImpairment(ctx, "", id, want); err != nil {
		t.Fatal(err)
	}
	if got, err := gw.GetSimImpairment(ctx, id); err != nil || got != want {
		t.Fatalf("read back %+v (%v), want %+v", got, err, want)
	}
	devs, err := gw.NodeSimDevices(ctx, "edge", all)
	if err != nil || len(devs) != 1 || devs[0].Impairment == nil || *devs[0].Impairment != want {
		t.Fatalf("device list = %+v (%v), want the impairment carried", devs, err)
	}

	if err := gw.SetSimImpairment(ctx, "", id, impair.Impairment{LatencyMs: 10, JitterMs: 20}); !errors.Is(err, storage.ErrSimImpairmentInvalid) {
		t.Fatalf("jitter above latency: err %v, want ErrSimImpairmentInvalid", err)
	}

	if err := gw.SetSimImpairment(ctx, "", id, impair.Impairment{}); err != nil {
		t.Fatal(err)
	}
	if devs, _ := gw.NodeSimDevices(ctx, "edge", all); devs[0].Impairment != nil {
		t.Fatalf("a cleared device still carries %+v", devs[0].Impairment)
	}
	var audits int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'sim_impairment' and resource_id = $1`, id).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("%d audit rows for two writes", audits)
	}
}
