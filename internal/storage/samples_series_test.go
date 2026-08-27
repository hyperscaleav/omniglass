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

// The first timeseries reads (#794): the samples BEHIND the effective reads'
// latest value, raw and capped, newest first. One shape for both owner arcs.
func TestListMetricSeries(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "probe-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	now := time.Now().UTC()
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "probe-1", Key: "icmp-rtt-avg", Value: 5.0, Source: "t", TS: now.Add(-3 * time.Hour)},
		{OwnerKind: "component", OwnerID: "probe-1", Key: "icmp-rtt-avg", Value: 6.1, Source: "t", TS: now.Add(-1 * time.Minute)},
		{OwnerKind: "component", OwnerID: "probe-1", Key: "icmp-rtt-avg", Value: 5.5, Source: "t", TS: now.Add(-30 * time.Hour)},
		{OwnerKind: "component", OwnerID: "probe-1", Key: "tcp-connect-time", Value: 9.9, Source: "t", TS: now},
	}); err != nil {
		t.Fatalf("insert samples: %v", err)
	}

	got, err := gw.ListMetricSeries(ctx, "component", "probe-1", "icmp-rtt-avg", 24*time.Hour, 100)
	if err != nil {
		t.Fatalf("list series: %v", err)
	}
	if len(got) != 2 || got[0].Value != 6.1 || got[1].Value != 5.0 {
		t.Fatalf("series = %+v, want the two in-window rtt samples newest first, never the other key or the out-of-window row", got)
	}

	// The cap keeps the newest rows.
	capped, err := gw.ListMetricSeries(ctx, "component", "probe-1", "icmp-rtt-avg", 24*time.Hour, 1)
	if err != nil {
		t.Fatalf("capped: %v", err)
	}
	if len(capped) != 1 || capped[0].Value != 6.1 {
		t.Fatalf("capped = %+v", capped)
	}
}

// The property mirror: the change history behind the effective value.
func TestListPropertySeries(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "probe-2"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	now := time.Now().UTC()
	if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "probe-2", Key: "firmware-version", Value: "2.1.0", Source: "collection", TS: now.Add(-2 * time.Hour)},
		{OwnerKind: "component", OwnerID: "probe-2", Key: "firmware-version", Value: "2.1.4", Source: "collection", TS: now.Add(-1 * time.Hour)},
	}); err != nil {
		t.Fatalf("insert samples: %v", err)
	}
	got, err := gw.ListPropertySeries(ctx, "component", "probe-2", "firmware-version", 24*time.Hour, 100)
	if err != nil {
		t.Fatalf("list property series: %v", err)
	}
	if len(got) != 2 || got[0].Value != "2.1.4" || got[1].Value != "2.1.0" {
		t.Fatalf("series = %+v, want the change history newest first", got)
	}
}
