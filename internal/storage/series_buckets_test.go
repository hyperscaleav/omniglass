package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/accesspath"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
	"github.com/jackc/pgx/v5"
)

// The bucketed series read (#1269): every regular bucket of the requested
// range, a spike surviving as the bucket's max, an empty bucket named by its
// count, and the instant before which retention left nothing to read.
func TestListMetricBuckets(t *testing.T) {
	t.Parallel()
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
	got, err := gw.ListMetricBuckets(ctx, "component", "bucket-1", "icmp-rtt-avg", nil, 720*time.Hour, time.Hour)
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
	short, err := gw.ListMetricBuckets(ctx, "component", "bucket-1", "icmp-rtt-avg", nil, 720*time.Hour, time.Hour)
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
	narrow, err := gw.ListMetricBuckets(ctx, "component", "bucket-1", "icmp-rtt-avg", nil, 6*time.Hour, 30*time.Minute)
	if err != nil {
		t.Fatalf("list narrow: %v", err)
	}
	if len(narrow.Buckets) != 12 || !narrow.CoveredFrom.Equal(narrow.Buckets[0].TS) {
		t.Fatalf("narrow = %d buckets covered from %s, want 12 from the first bucket's start %s", len(narrow.Buckets), narrow.CoveredFrom, narrow.Buckets[0].TS)
	}
}

// The property mirror: count and the value held at the bucket's end.
func TestListPropertyBuckets(t *testing.T) {
	t.Parallel()
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
	got, err := gw.ListPropertyBuckets(ctx, "component", "bucket-2", "firmware-version", nil, 24*time.Hour, time.Hour)
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

// A bucket is one logical series (#1485): one instance's measured rows. Two
// instances of a metric on one owner never blend into one bucket, a read that
// names no instance where two have measured rows is refused with both listed,
// and a declared row (a command's want) never moves min, max, avg or last.
func TestMetricBucketsAggregateOneMeasuredInstance(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	if _, err := f.gw.CreateMetricType(f.ctx, "", storage.MetricTypeSpec{Name: "bucket-level", DataType: "float"}); err != nil {
		t.Fatalf("create metric type: %v", err)
	}
	if _, err := f.gw.CreateCommandType(f.ctx, "", storage.CommandTypeSpec{Name: "set-bucket-level", TargetMetricType: "bucket-level"}); err != nil {
		t.Fatalf("create command type: %v", err)
	}
	comp, err := f.gw.GetComponent(f.ctx, "disp-1", scope.Set{All: true})
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	hour := time.Now().UTC().Truncate(time.Hour)
	if err := insertMetrics(f.ctx, f.gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "bucket-level", Instance: "ssh", Value: 10, Source: "t", TS: hour.Add(-2*time.Hour + 5*time.Minute)},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "bucket-level", Instance: "ssh", Value: 20, Source: "t", TS: hour.Add(-2*time.Hour + 10*time.Minute)},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "bucket-level", Instance: "tls", Value: 1000, Source: "t", TS: hour.Add(-2*time.Hour + 7*time.Minute)},
	}); err != nil {
		t.Fatalf("insert samples: %v", err)
	}

	// No instance named, two measured: refused, both listed, never blended.
	_, err = f.gw.ListMetricBuckets(f.ctx, "component", "disp-1", "bucket-level", nil, 24*time.Hour, time.Hour)
	var amb *storage.AmbiguousInstanceError
	if !errors.As(err, &amb) || !slices.Equal(amb.Instances, []string{"ssh", "tls"}) {
		t.Fatalf("an unnamed instance over two measured = %v, want AmbiguousInstanceError listing ssh and tls", err)
	}

	ssh := "ssh"
	got, err := f.gw.ListMetricBuckets(f.ctx, "component", "disp-1", "bucket-level", &ssh, 24*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("list ssh buckets: %v", err)
	}
	b := got.Buckets[23-2]
	if got.Instance != "ssh" || b.Count != 2 || *b.Min != 10 || *b.Max != 20 || *b.Avg != 15 || *b.Last != 20 {
		t.Fatalf("ssh bucket = %+v (instance %q), want 2 samples 10..20 avg 15 last 20: the tls reading is another series", b, got.Instance)
	}
	tls := "tls"
	other, err := f.gw.ListMetricBuckets(f.ctx, "component", "disp-1", "bucket-level", &tls, 24*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("list tls buckets: %v", err)
	}
	if b := other.Buckets[23-2]; b.Count != 1 || *b.Max != 1000 || *b.Min != 1000 {
		t.Fatalf("tls bucket = %+v, want its one sample of 1000 alone", b)
	}

	// A command's want lands in the declared tier on the ssh instance, now.
	if _, err := f.gw.IssueCommand(f.ctx, f.actor, "component", comp.ID, "set-bucket-level", "ssh", json.RawMessage(`9999`), nil); err != nil {
		t.Fatalf("issue command: %v", err)
	}
	if n := f.count(t, `select count(*) from metric_declared d join metric_series s on s.id = d.series_id where s.instance = 'ssh'`); n != 1 {
		t.Fatalf("declared rows = %d, want the command's one", n)
	}
	after, err := f.gw.ListMetricBuckets(f.ctx, "component", "disp-1", "bucket-level", &ssh, 24*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("list ssh buckets after the command: %v", err)
	}
	var total int64
	for _, b := range after.Buckets {
		total += b.Count
		for _, v := range []*float64{b.Min, b.Max, b.Avg, b.Last} {
			if v != nil && *v == 9999 {
				t.Fatalf("bucket %s = %+v carries the declared 9999: a want is not a reading", b.TS, b)
			}
		}
	}
	if total != 2 {
		t.Fatalf("ssh buckets hold %d samples after the command, want the 2 measured ones", total)
	}
}

// The single-instance default: an unnamed read over a series with one
// measured instance reads that one, and an intended series on another instance
// does not make the read ambiguous, since it is not measured.
func TestMetricBucketsDefaultToTheOneMeasuredInstance(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	if _, err := f.gw.CreateMetricType(f.ctx, "", storage.MetricTypeSpec{Name: "bucket-solo", DataType: "float"}); err != nil {
		t.Fatalf("create metric type: %v", err)
	}
	if _, err := f.gw.CreateCommandType(f.ctx, "", storage.CommandTypeSpec{Name: "set-bucket-solo", TargetMetricType: "bucket-solo"}); err != nil {
		t.Fatalf("create command type: %v", err)
	}
	comp, err := f.gw.GetComponent(f.ctx, "disp-1", scope.Set{All: true})
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	hour := time.Now().UTC().Truncate(time.Hour)
	if err := insertMetrics(f.ctx, f.gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "bucket-solo", Instance: "ssh", Value: 3, Source: "t", TS: hour.Add(-time.Hour + 5*time.Minute)},
	}); err != nil {
		t.Fatalf("insert samples: %v", err)
	}
	if _, err := f.gw.IssueCommand(f.ctx, f.actor, "component", comp.ID, "set-bucket-solo", "tls", json.RawMessage(`7`), nil); err != nil {
		t.Fatalf("issue command: %v", err)
	}
	got, err := f.gw.ListMetricBuckets(f.ctx, "component", "disp-1", "bucket-solo", nil, 24*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("an unnamed read over one measured instance = %v, want it read", err)
	}
	if got.Instance != "ssh" || got.Buckets[23-1].Count != 1 || *got.Buckets[23-1].Last != 3 {
		t.Fatalf("got instance %q bucket %+v, want ssh's one sample of 3", got.Instance, got.Buckets[23-1])
	}
}

// The property mirror of the series rule: two measured instances never blend,
// the unnamed read is refused listing them, and a declared value never becomes
// a bucket's last.
func TestPropertyBucketsAggregateOneMeasuredInstance(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	hour := time.Now().UTC().Truncate(time.Hour)
	if err := insertProperties(f.ctx, f.gw, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "disp-1", Key: "firmware-version", Instance: "ssh", Value: "2.1.0", Source: "collection", TS: hour.Add(-2*time.Hour + 5*time.Minute)},
		{OwnerKind: "component", OwnerID: "disp-1", Key: "firmware-version", Instance: "tls", Value: "7.0.0", Source: "collection", TS: hour.Add(-2*time.Hour + 9*time.Minute)},
	}); err != nil {
		t.Fatalf("insert samples: %v", err)
	}
	if _, err := f.conn.Exec(f.ctx, `insert into property (owner_kind, component_id, property_type_id, instance, provenance, value, ts)
		values ('component', (select id from component where name = 'disp-1'),
		        (select id from property_type_in_effect() where name = 'firmware-version'), 'ssh', 'declared', '"9.9.9"'::jsonb, now())`); err != nil {
		t.Fatalf("plant a declared value: %v", err)
	}

	_, err := f.gw.ListPropertyBuckets(f.ctx, "component", "disp-1", "firmware-version", nil, 24*time.Hour, time.Hour)
	var amb *storage.AmbiguousInstanceError
	if !errors.As(err, &amb) || !slices.Equal(amb.Instances, []string{"ssh", "tls"}) {
		t.Fatalf("an unnamed instance over two measured = %v, want AmbiguousInstanceError listing ssh and tls", err)
	}
	ssh := "ssh"
	got, err := f.gw.ListPropertyBuckets(f.ctx, "component", "disp-1", "firmware-version", &ssh, 24*time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("list ssh buckets: %v", err)
	}
	if b := got.Buckets[23-2]; b.Count != 1 || b.Last == nil || *b.Last != "2.1.0" {
		t.Fatalf("ssh bucket = %+v, want its one change to 2.1.0, not tls's 7.0.0", b)
	}
	for _, b := range got.Buckets {
		if b.Last != nil && *b.Last == "9.9.9" {
			t.Fatalf("bucket %s ends on the declared 9.9.9: a declared value is not a measured change", b.TS)
		}
	}
}

// The bucketed read's time bound reaches the sample scan (#1485): the range's
// lower bound and the coverage floor sit in the WHERE on metric itself, as bind
// parameters and now() rather than a CTE's column, so the planner prunes the
// daily partitions outside the range and searches each remaining one with a
// time condition instead of reading a series' whole history. Both statements
// the read issues are explained as the gateway issued them, captured off the
// pool tracer: the instance probe and the aggregate.
func TestMetricBucketsBoundTheSampleScan(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	counter := querycount.New()
	gw, err := storage.NewPG(ctx, dsn, storage.WithQueryTracer(counter))
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bucket-3"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "bucket-3", Key: "icmp-rtt-avg", Instance: "icmp", Value: 5, Source: "t", TS: time.Now().UTC().Add(-time.Hour)},
	}); err != nil {
		t.Fatalf("insert samples: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	counter.Reset()
	if _, err := gw.ListMetricBuckets(ctx, "component", "bucket-3", "icmp-rtt-avg", nil, 24*time.Hour, time.Hour); err != nil {
		t.Fatalf("list buckets: %v", err)
	}
	// The migration keeps three days ahead of today; a 24 hour range ending
	// in the current bucket can never reach the third of them.
	now := time.Now().UTC()
	ahead := fmt.Sprintf("metric_p%s", time.Date(now.Year(), now.Month(), now.Day()+3, 0, 0, 0, 0, time.UTC).Format("20060102"))
	for _, marker := range []string{"select s.instance from metric_series s", "as lst"} {
		t.Run(marker, func(t *testing.T) {
			call := oneCall(t, counter, marker)
			plan, err := accesspath.Explain(ctx, conn, call.SQL, call.Args...)
			if err != nil {
				t.Fatalf("explain the captured statement: %v", err)
			}
			var parts int
			for _, s := range plan {
				if !strings.HasPrefix(s.Relation, "metric_p") {
					continue
				}
				parts++
				if s.Relation == ahead {
					t.Errorf("the bucketed read scans %s, a day past its range: the time bound is not pruning partitions\n\t%s", s.Relation, plan)
				}
				if s.Index == "" || s.Disabled || !strings.Contains(s.Cond, "ts") {
					t.Errorf("the bucketed read scans %s without a time condition on an index: %s\n\t%s", s.Relation, s, plan)
				}
			}
			if parts == 0 {
				t.Fatalf("the plan scans no metric partition at all, so nothing was proven:\n\t%s", plan)
			}
		})
	}
}
