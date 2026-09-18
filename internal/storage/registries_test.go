package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestRegistrySeed proves the boot seed lands the catalog canon on its two lanes
// (the reachability metric_types, the non-numeric property_types) and that a
// second Run is idempotent. The transports are a code registry (ADR-0073,
// internal/transport), no longer seeded rows, so they are not asserted here.
func TestRegistrySeed(t *testing.T) {
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
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed (2nd, idempotent): %v", err)
	}

	// The reachability canon seeds on the metric lane (#587), official=true.
	mts, err := gw.ListMetricTypes(ctx)
	if err != nil {
		t.Fatalf("list metric types: %v", err)
	}
	metricOfficial := map[string]bool{}
	metricUnit := map[string]string{}
	for _, mt := range mts {
		metricOfficial[mt.Name] = mt.Official
		if mt.Unit != nil {
			metricUnit[mt.Name] = *mt.Unit
		}
	}
	for _, name := range []string{"icmp-reachable", "icmp-rtt-avg", "tcp-open", "tcp-connect-time"} {
		if !metricOfficial[name] {
			t.Errorf("metric type %s: want seeded official=true, got %v", name, metricOfficial)
		}
	}
	// The numeric facts live on the metric lane: the timing series keep their unit.
	if metricUnit["icmp-rtt-avg"] != "ms" || metricUnit["tcp-connect-time"] != "ms" {
		t.Errorf("metric units = %v, want ms on the timing series", metricUnit)
	}

	props, err := gw.ListPropertyTypes(ctx)
	if err != nil {
		t.Fatalf("list properties: %v", err)
	}
	official := map[string]bool{}
	for _, prop := range props {
		official[prop.Name] = prop.Official
	}
	// The property lane holds the non-numeric canon, no numeric row among it.
	for _, name := range []string{"endpoint-reachable", "health", "serial-number"} {
		if !official[name] {
			t.Errorf("property %s: want seeded official=true, got %v", name, official)
		}
	}
	if _, ok := official["icmp-reachable"]; ok {
		t.Error("icmp-reachable seeded on the property lane; numbers are metrics")
	}

}
