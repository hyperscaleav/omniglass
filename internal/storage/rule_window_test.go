package storage_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
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
			Clear: ptrCriteria(exprCriteria(trendClear)), SampleWindow: time.Hour, Severity: "high", FireEventType: "condition-started"}
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
			Fire: exprCriteria(tc.fire), SampleWindow: tc.window, FireEventType: "condition-started"}, scope.Set{All: true})
		if !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("%s: err %v, want ErrEventRuleInvalid", tc.name, err)
		}
	}
	// A preview judges each sample alone, so it refuses criteria that read
	// window rather than answer that a trend never fires.
	_, err := f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "room-temperature", Fire: exprCriteria(trendFire)}, scope.Set{All: true})
	if !errors.Is(err, storage.ErrEventRuleInvalid) {
		t.Errorf("previewing a windowed criterion: err %v, want ErrEventRuleInvalid", err)
	}
}
