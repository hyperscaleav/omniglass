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

// TestInsertPropertySamples proves an observed, component-owned property verdict is
// written and read back: LatestProperty returns the most recent value for a series,
// PropertyTransitions returns the ordered rows for the availability strip, and the
// owner-arc FK rejects a state for a component that does not exist.
func TestInsertPropertySamples(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	t0 := time.Now().UTC().Add(-2 * time.Minute)
	t1 := t0.Add(time.Minute)
	// Two transitions for one series (disp-1, interface-reachable, if-1): up then down.
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "interface-reachable", Instance: "if-1", Value: "up", Source: "tcp", TS: t0},
	}); err != nil {
		t.Fatalf("insert up: %v", err)
	}
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "interface-reachable", Instance: "if-1", Value: "down", Source: "tcp", TS: t1},
	}); err != nil {
		t.Fatalf("insert down: %v", err)
	}

	latest, err := gw.LatestProperty(ctx, "disp-1", "interface-reachable", "if-1")
	if err != nil {
		t.Fatalf("latest property: %v", err)
	}
	if latest == nil || latest.Value != "down" || latest.Provenance != "observed" {
		t.Fatalf("latest interface-reachable: want down observed, got %+v", latest)
	}

	// A different instance is a different series: no value yet.
	if other, err := gw.LatestProperty(ctx, "disp-1", "interface-reachable", "if-2"); err != nil {
		t.Fatalf("latest if-2: %v", err)
	} else if other != nil {
		t.Fatalf("if-2 should have no state, got %+v", other)
	}

	rows, err := gw.PropertyTransitions(ctx, "disp-1", "interface-reachable", "if-1", 0)
	if err != nil {
		t.Fatalf("property transitions: %v", err)
	}
	if len(rows) != 2 || rows[0].Value != "up" || rows[1].Value != "down" {
		t.Fatalf("transitions: want [up down] ordered, got %+v", rows)
	}

	// An owner component that does not exist violates the component FK.
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "ghost", Key: "interface-reachable", Instance: "if-1", Value: "up", Source: "tcp", TS: t1},
	}); err == nil {
		t.Fatal("insert with unknown owner: want error, got nil")
	}
}
