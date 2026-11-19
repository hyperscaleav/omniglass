package storage_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// trendCriteria fire on a series that rose a quarter of its level across at
// least half an hour of its window, and clear once it has stopped rising.
const (
	trendFire  = "span(window) >= 1800 && growth(window) > 0.25"
	trendClear = "growth(window) <= 0.1"
)

// TestWindowedRuleFiresOnATrend proves a rule with a sample_window judges
// the series' window, not one sample (#1985): a rising series opens an
// alarm and a flat noisy one of the same level does not, both when the rule
// is created over existing history (the prime reads the window) and when
// the samples arrive after it (the ingest reads it); the sweep and the
// reconcile scan read the same window.
func TestWindowedRuleFiresOnATrend(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	gw, all := f.gw, scope.Set{All: true}

	// One sample a minute for the past hour, oldest first: "rise" climbs
	// from 20 to 40, "flat" wobbles around 30 by up to a tenth.
	series := func(instance string, at func(i int) float64) {
		t.Helper()
		now := time.Now()
		for i := 0; i <= 58; i++ {
			if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
				Key: "room-temperature", Instance: instance, Value: at(i), TS: now.Add(time.Duration(i-58) * time.Minute)}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	rise := func(i int) float64 { return 20 + float64(i)*20/58 }
	flat := func(i int) float64 { return 30 * (1 + 0.1*float64((i*37)%21-10)/10) }
	open := func(rule, instance string) int {
		t.Helper()
		return count(t, f.conn, fmt.Sprintf(`select count(*) from alarm a join event_rule r on r.id = a.raised_by_rule_id
			where r.name = '%s' and a.cleared_at is null and a.dedup_key like '%%:%s'`, rule, instance))
	}
	spec := func(name string) storage.EventRuleSpec {
		return storage.EventRuleSpec{Name: name, SampleKey: "room-temperature", Fire: exprCriteria(trendFire),
			Clear: ptrCriteria(exprCriteria(trendClear)), SampleWindow: time.Hour, Severity: "high", FireEventType: "rule-fired"}
	}

	series("rise-before", rise)
	series("flat-before", flat)
	primed, err := gw.CreateEventRule(ctx, "", spec("trend-primed"), all)
	if err != nil {
		t.Fatal(err)
	}
	if primed.SampleWindow != time.Hour {
		t.Fatalf("stored sample_window %s, want 1h", primed.SampleWindow)
	}
	if open("trend-primed", "rise-before") != 1 || open("trend-primed", "flat-before") != 0 {
		t.Fatalf("primed over history: rise %d, flat %d open alarms, want 1 and 0",
			open("trend-primed", "rise-before"), open("trend-primed", "flat-before"))
	}

	if _, err := gw.CreateEventRule(ctx, "", spec("trend-ingest"), all); err != nil {
		t.Fatal(err)
	}
	series("rise-after", rise)
	series("flat-after", flat)
	if open("trend-ingest", "rise-after") != 1 || open("trend-ingest", "flat-after") != 0 {
		t.Fatalf("judged on ingest: rise %d, flat %d open alarms, want 1 and 0",
			open("trend-ingest", "rise-after"), open("trend-ingest", "flat-after"))
	}

	// The reconcile scan rejudges a windowed rule's touched series with its
	// window, and leaves the agreeing state as it found it.
	if err := gw.ReconcileEventRules(ctx, &storage.RuleReconcileState{}); err != nil {
		t.Fatal(err)
	}
	if open("trend-ingest", "rise-after") != 1 || open("trend-ingest", "flat-after") != 0 {
		t.Fatal("the reconcile scan changed a windowed rule's agreeing state")
	}

	// A widened window is a material change: the rule re-primes against it.
	wide := 2 * time.Hour
	widened, err := gw.UpdateEventRule(ctx, "", "trend-ingest", storage.EventRulePatch{SampleWindow: &wide}, all)
	if err != nil || widened.SampleWindow != wide || widened.Version != 2 {
		t.Fatalf("widening the window: %+v (%v), want version 2 at 2h", widened, err)
	}
}

// TestSampleWindowIsChecked proves a sample_window is refused where the
// criteria cannot use it: on a property, past a week, finer than a
// microsecond, unread by the criteria, or missing when the criteria read it;
// and a preview refuses criteria that read window.
func TestSampleWindowIsChecked(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		key    string
		fire   string
		window time.Duration
	}{
		{"on-a-property", "endpoint-reachable", `value == "down"`, time.Hour},
		{"past-a-week", "room-temperature", trendFire, rules.MaxSampleWindow + time.Hour},
		{"negative", "room-temperature", trendFire, -time.Hour},
		{"too-fine", "room-temperature", trendFire, time.Hour + time.Nanosecond},
		{"unread", "room-temperature", "value > 40", time.Hour},
		{"missing", "room-temperature", trendFire, 0},
	} {
		_, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "window-" + tc.name, SampleKey: tc.key,
			Fire: exprCriteria(tc.fire), SampleWindow: tc.window, FireEventType: "rule-fired"}, scope.Set{All: true})
		if !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("%s: err %v, want ErrEventRuleInvalid", tc.name, err)
		}
	}
	// A preview judges each sample alone, so it refuses criteria that read
	// window or age rather than answer that a trend never fires.
	for _, fire := range []string{trendFire, "age > 60 && value > 40"} {
		_, err := f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "room-temperature", Fire: exprCriteria(fire)}, scope.Set{All: true})
		if !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("previewing %q: err %v, want ErrEventRuleInvalid", fire, err)
		}
	}
}

// TestARuleReadsItsSeriesAge proves a rule's age is how long the series it
// judges has run (#1985): a device key's from its oldest kept sample, so a
// gate past the series' hour holds a rise it would otherwise fire on; and a
// process's own key's from the process's start (its process-uptime), not
// from a series that spans restarts, so a process just restarted is young
// however long its heap series has run.
func TestARuleReadsItsSeriesAge(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	gw, all := f.gw, scope.Set{All: true}
	now := time.Now()
	rise := func(i int) float64 { return 20e6 + float64(i)*20e6/58 }

	for i := 0; i <= 58; i++ {
		if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
			Key: "room-temperature", Instance: "aged", Value: rise(i) / 1e6, TS: now.Add(time.Duration(i-58) * time.Minute)}}); err != nil {
			t.Fatal(err)
		}
	}
	young, err := gw.EnsureServerComponent(ctx, "restarted")
	if err != nil {
		t.Fatal(err)
	}
	old, err := gw.EnsureServerComponent(ctx, "long-running")
	if err != nil {
		t.Fatal(err)
	}
	// Both heaps rose across the past hour; one process started ten minutes
	// ago, the other two hours ago.
	if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{
		{ComponentID: young, Key: writerclass.ProcessUptime, Value: 600, TS: now},
		{ComponentID: old, Key: writerclass.ProcessUptime, Value: 7200, TS: now},
	}); err != nil {
		t.Fatal(err)
	}
	var heap []storage.PlatformMetric
	for i := 0; i <= 58; i++ {
		for _, c := range []string{young, old} {
			heap = append(heap, storage.PlatformMetric{ComponentID: c, Key: writerclass.ProcessHeapSize,
				Value: rise(i), TS: now.Add(time.Duration(i-58) * time.Minute)})
		}
	}
	if err := gw.RecordPlatformMetrics(ctx, heap); err != nil {
		t.Fatal(err)
	}

	create := func(name, key string, age int) {
		t.Helper()
		if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: name, SampleKey: key,
			Fire: exprCriteria(fmt.Sprintf("age >= %d && %s", age, trendFire)), Clear: ptrCriteria(exprCriteria(trendClear)),
			SampleWindow: time.Hour, Severity: "high", FireEventType: "rule-fired"}, all); err != nil {
			t.Fatal(err)
		}
	}
	open := func(rule, component string) int {
		t.Helper()
		return count(t, f.conn, fmt.Sprintf(`select count(*) from alarm a join event_rule r on r.id = a.raised_by_rule_id
			where r.name = '%s' and a.cleared_at is null and a.component_id = (select id from component where id::text = '%s' or name = '%s')`,
			rule, component, component))
	}

	create("aged-under", "room-temperature", 3000)
	create("aged-over", "room-temperature", 7200)
	if open("aged-under", "bar-1") != 1 || open("aged-over", "bar-1") != 0 {
		t.Errorf("an hour-old series: gated at 50m %d, at 2h %d open alarms, want 1 and 0",
			open("aged-under", "bar-1"), open("aged-over", "bar-1"))
	}

	create("heap-aged", writerclass.ProcessHeapSize, 3600)
	if open("heap-aged", old) != 1 || open("heap-aged", young) != 0 {
		t.Errorf("an hour's heap rise gated at an hour's uptime: long-running %d, restarted %d open alarms, want 1 and 0",
			open("heap-aged", old), open("heap-aged", young))
	}
}

// TestTheRuleWindowIsABoundedProbe proves a window's read is bounded by its
// buckets, not by the series' cadence (#1985): a series written fifty times
// a bucket reads at most windowBucketSamples rows a bucket, where grouping
// the window's rows and limiting after would read every one.
func TestTheRuleWindowIsABoundedProbe(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	now := time.Now()
	const samples = 3000
	dense := make([]storage.MetricSampleWrite, samples)
	for i := range dense {
		dense[i] = storage.MetricSampleWrite{OwnerKind: "component", OwnerID: "bar-1", Key: "room-temperature",
			Instance: "dense", Value: float64(i), TS: now.Add(-time.Duration(i) * 200 * time.Millisecond)}
	}
	if err := insertMetrics(ctx, f.gw, dense); err != nil {
		t.Fatal(err)
	}
	var comp string
	if err := f.conn.QueryRow(ctx, `select id from component where name = 'bar-1'`).Scan(&comp); err != nil {
		t.Fatal(err)
	}
	// An hour's window: 10s buckets, so the ten minutes written fall in at
	// most 62 buckets, of fifty samples each. (The plan's row counts are
	// per-loop means, rounded, so the sum is a little off a whole count.)
	bucket := rules.WindowBucket(time.Hour).Seconds()
	var plan []map[string]any
	if err := f.conn.QueryRow(ctx, "explain (analyze, format json) "+storage.ExportRuleWindowSQL,
		comp, "room-temperature", "dense", bucket, rules.MaxWindowPoints, storage.ExportWindowBucketSamples).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	read := metricRowsRead(plan[0]["Plan"].(map[string]any))
	if limit := float64(62 * storage.ExportWindowBucketSamples); read == 0 || read > limit {
		t.Errorf("the window read %v metric rows of %d, want at most %v (its buckets' newest few)", read, samples, limit)
	}
}

// metricRowsRead sums the rows a plan's scans of the metric partitions
// returned, across every loop.
func metricRowsRead(node map[string]any) float64 {
	var n float64
	if rel, _ := node["Relation Name"].(string); rel == "metric" || strings.HasPrefix(rel, "metric_p") {
		n += node["Actual Rows"].(float64) * node["Actual Loops"].(float64)
	}
	kids, _ := node["Plans"].([]any)
	for _, k := range kids {
		n += metricRowsRead(k.(map[string]any))
	}
	return n
}
