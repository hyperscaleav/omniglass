package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestReachabilityReads proves the two read helpers the reachability BFF composes
// over: ListComponentInterfaces returns a component's interfaces ordered by name,
// and LatestMetricInstance resolves one interface's latest probe value rather than
// the newest across every interface (as the instance-blind LatestMetric does).
func TestReachabilityReads(t *testing.T) {
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

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1", ComponentType: "display"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	// Two interfaces on the component (icmp and tcp), seeded via raw insert since
	// there is no interface CRUD gateway method yet.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, params) values
		('disp-1-tcp', 'tcp', 'disp-1', '{"target":"10.20.4.11","port":5000}'::jsonb),
		('disp-1-icmp', 'icmp', 'disp-1', '{"target":"10.20.4.11"}'::jsonb)`); err != nil {
		t.Fatalf("insert interfaces: %v", err)
	}

	ifaces, err := gw.ListComponentInterfaces(ctx, "disp-1")
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
	if len(ifaces) != 2 || ifaces[0].Name != "disp-1-icmp" || ifaces[1].Name != "disp-1-tcp" {
		t.Fatalf("interfaces: want [disp-1-icmp disp-1-tcp] ordered, got %+v", ifaces)
	}
	if ifaces[1].Type != "tcp" || len(ifaces[1].Params) == 0 {
		t.Fatalf("tcp interface: want type tcp with params, got %+v", ifaces[1])
	}

	// tcp.open on two different interface instances at different times: the newer
	// write is on disp-1-icmp is irrelevant; instance-scoped read must return the
	// disp-1-tcp value even though it is older.
	older := time.Now().UTC().Add(-2 * time.Minute)
	newer := older.Add(time.Minute)
	if err := gw.InsertMetricDatapoints(ctx, []storage.MetricDatapointEvent{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "tcp.open", Instance: "disp-1-tcp", Value: 1, Source: "tcp", TS: older},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "tcp.open", Instance: "disp-1-icmp", Value: 0, Source: "tcp", TS: newer},
	}); err != nil {
		t.Fatalf("insert metrics: %v", err)
	}

	dp, err := gw.LatestMetricInstance(ctx, "disp-1", "tcp.open", "disp-1-tcp")
	if err != nil {
		t.Fatalf("latest metric instance: %v", err)
	}
	if dp == nil || dp.Value != 1 || dp.Instance != "disp-1-tcp" {
		t.Fatalf("tcp.open[disp-1-tcp]: want value 1, got %+v", dp)
	}

	// A series with no datapoint returns nil, not an error.
	if none, err := gw.LatestMetricInstance(ctx, "disp-1", "icmp.reachable", "disp-1-icmp"); err != nil {
		t.Fatalf("latest missing metric: %v", err)
	} else if none != nil {
		t.Fatalf("icmp.reachable[disp-1-icmp]: want nil, got %+v", none)
	}
}
