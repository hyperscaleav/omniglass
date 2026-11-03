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

// The bucketed series read (#1269): every regular bucket of the requested
// range, a spike surviving as the bucket's max, an empty bucket named by its
// count, and the instant before which retention left nothing to read.
func TestListMetricBuckets(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bucket-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	// Anchor the samples to the hour grid the read bins to, so a bucket's
	// content does not depend on the minute the test runs.
	hour := time.Now().UTC().Truncate(time.Hour)
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "bucket-1", Key: "icmp-rtt-avg", Instance: "icmp", Value: 5, Source: "t", TS: hour.Add(-3*time.Hour + 5*time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-1", Key: "icmp-rtt-avg", Instance: "icmp", Value: 400, Source: "t", TS: hour.Add(-3*time.Hour + 20*time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-1", Key: "icmp-rtt-avg", Instance: "icmp", Value: 7, Source: "t", TS: hour.Add(-3*time.Hour + 50*time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-1", Key: "icmp-rtt-avg", Instance: "icmp", Value: 9, Source: "t", TS: hour.Add(-1*time.Hour + 10*time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-1", Key: "tcp-connect-time", Instance: "icmp", Value: 99, Source: "t", TS: hour.Add(-3*time.Hour + 6*time.Minute)},
	}); err != nil {
		t.Fatalf("insert samples: %v", err)
	}

	// 30 days at 1 hour is 720 buckets, whatever retention keeps.
	got, err := gw.ListMetricBuckets(ctx, "component", "bucket-1", "icmp-rtt-avg", 720*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("list buckets: %v", err)
	}
	if len(got.Buckets) != 720 {
		t.Fatalf("buckets = %d, want 720: every regular bucket of the range, never a truncated tail", len(got.Buckets))
	}
	for i := 1; i < len(got.Buckets); i++ {
		if d := got.Buckets[i].TS.Sub(got.Buckets[i-1].TS); d != time.Hour {
			t.Fatalf("bucket %d starts %s after the one before, want a regular hour", i, d)
		}
	}
	if last := got.Buckets[719]; !last.TS.Equal(hour) {
		t.Fatalf("newest bucket starts %s, want the current hour %s", last.TS, hour)
	}

	spike := got.Buckets[719-3]
	if !spike.TS.Equal(hour.Add(-3*time.Hour)) || spike.Count != 3 {
		t.Fatalf("spike bucket = %+v, want the hour three back with its 3 samples", spike)
	}
	if spike.Max == nil || *spike.Max != 400 || spike.Min == nil || *spike.Min != 5 {
		t.Fatalf("spike bucket min/max = %v/%v, want 5/400: a one-sample spike survives as the bucket's max", spike.Min, spike.Max)
	}
	if spike.Avg == nil || *spike.Avg != (5.0+400+7)/3 || spike.Last == nil || *spike.Last != 7 {
		t.Fatalf("spike bucket avg/last = %v/%v, want %v/7", spike.Avg, spike.Last, (5.0+400+7)/3)
	}
	// The other key's sample in the same hour is not this series'.
	empty := got.Buckets[719-2]
	if empty.Count != 0 || empty.Min != nil || empty.Max != nil || empty.Avg != nil || empty.Last != nil {
		t.Fatalf("empty bucket = %+v, want count 0 and null min/max/avg/last", empty)
	}

	// The default window is 14 days: nothing older is read, and covered_from
	// says so, so the empty buckets before it are retention's, not collection's.
	wantCovered := time.Now().UTC().Add(-14 * 24 * time.Hour)
	if d := got.CoveredFrom.Sub(wantCovered); d < -time.Minute || d > time.Minute {
		t.Fatalf("covered_from = %s, want about %s (the install's metric_days)", got.CoveredFrom, wantCovered)
	}

	// A type's own window wins over the install's.
	three := 3
	if _, err := gw.SetMetricTypeRetention(ctx, "", "icmp-rtt-avg", &three); err != nil {
		t.Fatalf("set retention: %v", err)
	}
	short, err := gw.ListMetricBuckets(ctx, "component", "bucket-1", "icmp-rtt-avg", 720*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("list buckets: %v", err)
	}
	if want := time.Now().UTC().Add(-72 * time.Hour); short.CoveredFrom.Sub(want) < -time.Minute || short.CoveredFrom.Sub(want) > time.Minute {
		t.Fatalf("covered_from = %s, want about %s (the type's effective retention)", short.CoveredFrom, want)
	}
	if len(short.Buckets) != 720 {
		t.Fatalf("buckets = %d, want 720 still", len(short.Buckets))
	}

	// A range shorter than the retention is covered from its own start.
	narrow, err := gw.ListMetricBuckets(ctx, "component", "bucket-1", "icmp-rtt-avg", 6*time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatalf("list narrow: %v", err)
	}
	if len(narrow.Buckets) != 12 || !narrow.CoveredFrom.Equal(narrow.Buckets[0].TS) {
		t.Fatalf("narrow = %d buckets covered from %s, want 12 from the first bucket's start %s", len(narrow.Buckets), narrow.CoveredFrom, narrow.Buckets[0].TS)
	}
}

// The property mirror: count and the value held at the bucket's end.
func TestListPropertyBuckets(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bucket-2"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	hour := time.Now().UTC().Truncate(time.Hour)
	if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "bucket-2", Key: "firmware-version", Value: "2.1.0", Source: "collection", TS: hour.Add(-2*time.Hour + 5*time.Minute)},
		{OwnerKind: "component", OwnerID: "bucket-2", Key: "firmware-version", Value: "2.1.4", Source: "collection", TS: hour.Add(-2*time.Hour + 40*time.Minute)},
	}); err != nil {
		t.Fatalf("insert samples: %v", err)
	}
	got, err := gw.ListPropertyBuckets(ctx, "component", "bucket-2", "firmware-version", 24*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("list buckets: %v", err)
	}
	if len(got.Buckets) != 24 {
		t.Fatalf("buckets = %d, want 24", len(got.Buckets))
	}
	b := got.Buckets[23-2]
	if b.Count != 2 || b.Last == nil || *b.Last != "2.1.4" {
		t.Fatalf("bucket = %+v, want 2 changes ending at 2.1.4", b)
	}
	if e := got.Buckets[23-1]; e.Count != 0 || e.Last != nil {
		t.Fatalf("empty bucket = %+v, want count 0 and a null last", e)
	}
	// Property types retain for property_days (90) unless their own says otherwise.
	if want := time.Now().UTC().Add(-24 * time.Hour); got.CoveredFrom.Sub(want) > time.Minute+time.Hour || got.CoveredFrom.Before(got.Buckets[0].TS) {
		t.Fatalf("covered_from = %s, want the range's own start (inside 90 days)", got.CoveredFrom)
	}
}
