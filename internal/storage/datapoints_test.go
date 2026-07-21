package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestInsertMetricDatapoints proves an observed, component-owned reachability
// datapoint is written and read back, and that the owner-arc CHECK rejects a
// datapoint whose owner component does not exist (FK) as a write error.
func TestInsertMetricDatapoints(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	now := time.Now().UTC()
	err = gw.InsertMetricDatapoints(ctx, []storage.MetricDatapointEvent{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "tcp.open", Value: 1, Source: "tcp", TS: now},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "tcp.connect_time", Value: 3.2, Source: "tcp", TS: now},
	})
	if err != nil {
		t.Fatalf("insert datapoints: %v", err)
	}

	dp, err := gw.LatestMetric(ctx, "disp-1", "tcp.open")
	if err != nil {
		t.Fatalf("latest metric: %v", err)
	}
	if dp == nil || dp.Value != 1 || dp.Provenance != "observed" {
		t.Fatalf("latest tcp.open: want value 1 observed, got %+v", dp)
	}

	// An owner component that does not exist violates the component FK.
	err = gw.InsertMetricDatapoints(ctx, []storage.MetricDatapointEvent{
		{OwnerKind: "component", OwnerID: "ghost", Key: "tcp.open", Value: 0, Source: "tcp", TS: now},
	})
	if err == nil {
		t.Fatal("insert with unknown owner: want error, got nil")
	}
}
