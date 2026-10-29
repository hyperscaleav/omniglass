package storage_test

import (
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func (r *evalRig) metricAt(key string, v float64, ts time.Time) {
	r.t.Helper()
	if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.comp, Key: key,
		Value: v, Source: "test", TS: ts}}); err != nil {
		r.t.Fatal(err)
	}
}

func windowSpec(name string, seconds int) storage.CalcRuleSpec {
	return storage.CalcRuleSpec{Name: name, OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature", Window: seconds}}, Reduce: rules.CalcReduce{Expr: "avg(t)"}, OutputKey: "avg-temp"}
}

// TestCalcWindowAverages proves a windowed input (#1142): the rule reads the
// key's samples in its window, so avg(t) is their mean, a sample older than
// the window is left out, and the window is kept on the rule as written.
func TestCalcWindowAverages(t *testing.T) {
	r := newEvalRig(t)
	now := time.Now()
	r.metricAt("room-temperature", 100, now.Add(-time.Hour+time.Minute)) // outside a 5m window
	r.metricAt("room-temperature", 10, now.Add(-2*time.Minute))
	r.metricAt("room-temperature", 20, now.Add(-time.Minute))
	rule, err := r.create(windowSpec("temp-5m", 300))
	if err != nil {
		t.Fatal(err)
	}
	if len(rule.Inputs) != 1 || rule.Inputs[0].Window != 300 {
		t.Fatalf("inputs %+v, want the 300s window kept", rule.Inputs)
	}
	got, err := r.gw.GetCalcRule(r.ctx, "temp-5m")
	if err != nil || got.Inputs[0].Window != 300 {
		t.Fatalf("read back %+v, %v; want the 300s window", got, err)
	}
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 1 || got[0] != "15 temp-5m 1" {
		t.Fatalf("avg-temp rows %v, want one calculated 15 (the two samples in the window)", got)
	}
	r.metricAt("room-temperature", 30, now)
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 2 || got[1] != "20 temp-5m 1" {
		t.Fatalf("avg-temp rows %v, want a second, 20, after a new sample", got)
	}
}

// TestCalcWindowAgesOut proves a window that ages out with no new sample
// still updates (#1142): the serve records when its oldest sample leaves the
// window, the tick turns that due time into a mark, and the rule recomputes
// without the sample. A window left empty writes nothing and says its input
// is missing, as a key with no value does.
func TestCalcWindowAgesOut(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.create(windowSpec("temp-3s", 3)); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r.metricAt("room-temperature", 10, now.Add(-2500*time.Millisecond))
	r.metricAt("room-temperature", 20, now)
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 1 || got[0] != "15 temp-3s 1" {
		t.Fatalf("avg-temp rows %v, want 15 over both samples", got)
	}
	due := scalar(t, r.conn, `select (window_due_at is not null)::text from calc_rule_state`)
	if due != "true" {
		t.Fatalf("window_due_at set %s, want a due time for the oldest sample", due)
	}
	// Before the oldest sample ages out the tick has nothing to do.
	if n := r.drain(); n != 0 {
		t.Fatalf("served %d before the window moved, want 0", n)
	}
	time.Sleep(time.Until(now.Add(600 * time.Millisecond)))
	if n := r.drain(); n != 1 {
		t.Fatalf("served %d after the oldest sample aged out, want 1", n)
	}
	if got := r.calculated("avg-temp"); len(got) != 2 || got[1] != "20 temp-3s 1" {
		t.Fatalf("avg-temp rows %v, want a second, 20, with no new sample", got)
	}
	time.Sleep(time.Until(now.Add(3100 * time.Millisecond)))
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 2 {
		t.Fatalf("avg-temp rows %v after the window emptied, want still two", got)
	}
	if got := scalar(t, r.conn, `select missing::text || ' ' || (window_due_at is null)::text from calc_rule_state`); got != `["room-temperature"] true` {
		t.Fatalf("state %q, want the input missing and no due time", got)
	}
	if n := r.drain(); n != 0 {
		t.Fatalf("served %d over an empty window, want 0: nothing is left to age out", n)
	}
}

// TestCalcWindowBounded proves one window's read is bounded (#1142): the
// newest MaxCalcWindowSamples samples in it are reduced, the older ones left
// out.
func TestCalcWindowBounded(t *testing.T) {
	r := newEvalRig(t)
	n := rules.MaxCalcWindowSamples + 5
	base := time.Now().Add(-time.Duration(n) * time.Millisecond)
	evs := make([]storage.MetricSampleWrite, n)
	for i := range evs {
		evs[i] = storage.MetricSampleWrite{OwnerKind: "component", OwnerID: r.comp, Key: "room-temperature",
			Value: float64(i), Source: "test", TS: base.Add(time.Duration(i) * time.Millisecond)}
	}
	if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), evs); err != nil {
		t.Fatal(err)
	}
	if _, err := r.create(windowSpec("temp-1m", 60)); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 1 || got[0] != "504.5 temp-1m 1" {
		t.Fatalf("avg-temp rows %v, want 504.5, the mean of the newest %d", got, rules.MaxCalcWindowSamples)
	}
}

// TestCalcWindowRefused proves a window outside 1s to an hour, or on a
// system rule, is refused at write on its field (#1142), on create and on
// update.
func TestCalcWindowRefused(t *testing.T) {
	r := newEvalRig(t)
	for name, spec := range map[string]storage.CalcRuleSpec{
		"over an hour": windowSpec("too-long", 3601),
		"negative":     windowSpec("negative", -5),
		"system rule": {Name: "sys-window", OwnerKind: rules.OwnerSystem,
			Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature", Window: 60}}, Reduce: rules.CalcReduce{Expr: "avg(t)"}, OutputKey: "avg-temp"},
	} {
		_, err := r.create(spec)
		var ce *rules.CalcError
		if !errors.Is(err, storage.ErrCalcRuleInvalid) || !errors.As(err, &ce) || ce.Field != "inputs[0].window_seconds" {
			t.Errorf("%s: %v, want ErrCalcRuleInvalid on inputs[0].window_seconds", name, err)
		}
	}
	if _, err := r.create(avgSpec("temp-now")); err != nil {
		t.Fatal(err)
	}
	long := []rules.CalcInput{{Alias: "t", Key: "room-temperature", Window: 7200}}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "temp-now", storage.CalcRulePatch{Inputs: &long}, r.all); !errors.Is(err, storage.ErrCalcRuleInvalid) {
		t.Errorf("update to a 2h window: %v, want ErrCalcRuleInvalid", err)
	}
}

// TestCalcWindowPreview proves a windowed candidate previews the window's
// mean, and that a windowed input weighs on the preview's work cap as the
// range read it is, not as one current-value read (#1142 review).
func TestCalcWindowPreview(t *testing.T) {
	r := newEvalRig(t)
	other, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "calc-other"}, r.all, r.all, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r.metricAt("room-temperature", 10, now.Add(-time.Minute))
	r.metricAt("room-temperature", 30, now)
	if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: other.ID,
		Key: "room-temperature", Value: 5, Source: "test", TS: now}}); err != nil {
		t.Fatal(err)
	}
	pv, err := r.gw.PreviewCalcRule(r.ctx, windowSpec("", 300), r.all, r.all)
	if err != nil || pv.OwnersJudged != 2 || pv.Truncated {
		t.Fatalf("preview %+v (%v), want both owners judged", pv, err)
	}
	mean := ""
	for _, o := range pv.Owners {
		if o.OwnerID == r.comp {
			mean = string(o.Value)
		}
	}
	if mean != "20" {
		t.Fatalf("the window's preview on calc-target is %v, want 20", mean)
	}
	restore := storage.SetCalcPreviewWorkCap(150)
	pv, err = r.gw.PreviewCalcRule(r.ctx, windowSpec("", 300), r.all, r.all)
	restore()
	if err != nil || pv.OwnersJudged != 1 || !pv.Truncated {
		t.Fatalf("a work-capped windowed preview %+v (%v), want 1 of 2, truncated", pv, err)
	}
}

// TestASteadyMetricOutputStaysWithinRetention: the metric lane keeps no floor
// (ADR-1275), so an output that holds steady is written again once its newest
// row is half a day old, and a rule still producing it keeps a value within
// any window a metric type may keep; a younger unchanged result writes nothing.
func TestASteadyMetricOutputStaysWithinRetention(t *testing.T) {
	r := newEvalRig(t)
	now := time.Now()
	r.metricAt("room-temperature", 20, now.Add(-time.Minute))
	if _, err := r.create(windowSpec("temp-steady", 0)); err != nil {
		t.Fatal(err)
	}
	r.drain()
	r.metricAt("room-temperature", 20, now)
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 1 {
		t.Fatalf("avg-temp rows %v, want one: an unchanged result inside half a day writes nothing", got)
	}
	if _, err := r.conn.Exec(r.ctx, `update metric set ts = ts - interval '13 hours'
		where series_id in (select id from metric_series where provenance = 'calculated')`); err != nil {
		t.Fatal(err)
	}
	r.metricAt("room-temperature", 20, now.Add(time.Second))
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 2 || got[0] != got[1] {
		t.Fatalf("avg-temp rows %v, want the steady value written again past half a day", got)
	}
}
