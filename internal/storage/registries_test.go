package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestRegistrySeed proves the boot seed lands the reachability datapoint_type
// canon and the icmp/tcp interface_types, and that a second Run is idempotent.
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

	dts, err := gw.ListDatapointTypes(ctx)
	if err != nil {
		t.Fatalf("list datapoint_types: %v", err)
	}
	want := map[string]string{"icmp.reachable": "metric", "icmp.rtt_avg": "metric", "tcp.open": "metric", "tcp.connect_time": "metric"}
	got := map[string]string{}
	for _, dt := range dts {
		got[dt.Name] = dt.Kind
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("datapoint_type %s: want kind %q, got %q", name, kind, got[name])
		}
	}

	its, err := gw.ListInterfaceTypes(ctx)
	if err != nil {
		t.Fatalf("list interface_types: %v", err)
	}
	seen := map[string]bool{}
	for _, it := range its {
		seen[it.Name] = it.Built
	}
	if !seen["icmp"] || !seen["tcp"] {
		t.Errorf("interface_types: want icmp+tcp built, got %v", seen)
	}
}
