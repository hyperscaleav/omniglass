package storage_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func metricAt(t *testing.T, f *healthFixture, comp string, v float64, ts time.Time) {
	t.Helper()
	if err := insertMetrics(context.Background(), f.gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: comp,
		Key: "room-temperature", Value: v, TS: ts}}); err != nil {
		t.Fatal(err)
	}
}

func propAt(t *testing.T, f *healthFixture, comp, v string, ts time.Time) {
	t.Helper()
	if err := insertProperties(context.Background(), f.gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: comp,
		Key: "endpoint-reachable", Instance: "tcp", Value: v, TS: ts}}); err != nil {
		t.Fatal(err)
	}
}

func mkComponents(t *testing.T, f *healthFixture, names ...string) {
	t.Helper()
	product := f.bar.Name
	for _, n := range names {
		if _, err := f.gw.CreateComponent(context.Background(), "", storage.ComponentSpec{Name: n, ProductName: &product}, f.all, f.all, f.all, f.all); err != nil {
			t.Fatal(err)
		}
	}
}

func edgeKinds(p *storage.RulePreview) []string {
	var out []string
	for _, e := range p.Edges {
		out = append(out, e.ComponentName+":"+e.Kind)
	}
	return out
}

// A rule with hysteresis (fire above 80, clear below 70) starts the window
// from the newest DECISIVE sample before it, not the newest: 85 then 75
// before the window leave it in force, so the 60 inside it is a clear edge,
// as the ingest judged it.
func TestPreviewSeedsFromTheNewestDecisiveSample(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	mkComponents(t, f, "hy-1")
	now := time.Now()
	metricAt(t, f, "hy-1", 85, now.Add(-30*time.Hour))
	metricAt(t, f, "hy-1", 75, now.Add(-26*time.Hour))
	metricAt(t, f, "hy-1", 60, now.Add(-2*time.Hour))
	p, err := f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "room-temperature", Fire: rules.Criteria{Expr: "value > 80"},
		Clear: &rules.Criteria{Expr: "value < 70"}, Selector: storage.EventRuleSelector{Components: []string{"hy-1"}}}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if got := edgeKinds(p); len(got) != 1 || got[0] != "hy-1:clear" || len(p.InForce) != 0 || len(p.StateUnknown) != 0 {
		t.Fatalf("edges %v in force %+v unknown %+v, want one clear and nothing in force", got, p.InForce, p.StateUnknown)
	}
}

// A series with no decisive sample within the lookback starts unknown, and
// says so rather than guessing: its first decisive sample sets the state
// without an edge.
func TestPreviewStateUnknown(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	mkComponents(t, f, "un-1")
	now := time.Now()
	propAt(t, f, "un-1", "down", now.Add(-3*time.Hour))
	propAt(t, f, "un-1", "up", now.Add(-2*time.Hour))
	p, err := f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "endpoint-reachable", Fire: rules.Criteria{Expr: `value == "down"`},
		Selector: storage.EventRuleSelector{Components: []string{"un-1"}}}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.StateUnknown) != 1 || p.StateUnknown[0].ComponentName != "un-1" {
		t.Fatalf("unknown %+v, want un-1/tcp", p.StateUnknown)
	}
	if got := edgeKinds(p); len(got) != 1 || got[0] != "un-1:clear" {
		t.Fatalf("edges %v, want only the clear after the first decisive sample set the state", got)
	}
}

// A sample that arrives older than its series' newest is history to the
// ingest, which judges nothing by it; the preview walks the same way.
func TestPreviewSkipsLateSamples(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	mkComponents(t, f, "late-1")
	base := time.Now().Add(-time.Hour)
	propAt(t, f, "late-1", "up", base.Add(-48*time.Hour)) // a decisive seed: out of force
	propAt(t, f, "late-1", "up", base.Add(20*time.Second))
	propAt(t, f, "late-1", "down", base.Add(10*time.Second)) // late
	p, err := f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "endpoint-reachable", Fire: rules.Criteria{Expr: `value == "down"`},
		Selector: storage.EventRuleSelector{Components: []string{"late-1"}}}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Edges) != 0 || len(p.InForce) != 0 {
		t.Fatalf("edges %v in force %+v, want none: the late down is history", edgeKinds(p), p.InForce)
	}
}

// The selector is applied before anything is read: a one-component selector
// on a fleet with more samples than the cap judges its component whole.
// The cap takes whole series, counted.
func TestPreviewSelectsAndCapsWholeSeries(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	var names []string
	for i := 0; i < 6; i++ {
		names = append(names, fmt.Sprintf("fl-%d", i))
	}
	mkComponents(t, f, names...)
	now := time.Now()
	for _, n := range names {
		propAt(t, f, n, "down", now.Add(-3*time.Hour))
		propAt(t, f, n, "up", now.Add(-2*time.Hour))
	}
	defer storage.SetRulePreviewRowCap(3)()
	one, err := f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "endpoint-reachable", Fire: rules.Criteria{Expr: `value == "down"`},
		Selector: storage.EventRuleSelector{Components: []string{"fl-5"}}}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if one.Truncated || one.SeriesJudged != 1 || one.SeriesTotal != 1 || len(one.StateUnknown) != 1 {
		t.Fatalf("one component on a big fleet: truncated %v judged %d/%d", one.Truncated, one.SeriesJudged, one.SeriesTotal)
	}
	two, err := f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "endpoint-reachable", Fire: rules.Criteria{Expr: `value == "down"`},
		Selector: storage.EventRuleSelector{Components: []string{"fl-1", "fl-2"}}}, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if !two.Truncated || two.SeriesJudged != 1 || two.SeriesTotal != 2 {
		t.Fatalf("two series of 2 rows at a cap of 3: truncated %v judged %d/%d, want 1 of 2 judged whole", two.Truncated, two.SeriesJudged, two.SeriesTotal)
	}
	for _, e := range two.Edges {
		if e.ComponentName != two.Edges[0].ComponentName {
			t.Fatalf("edges from a series past the cap: %v", edgeKinds(two))
		}
	}
}

// Every per-series read is an index probe on the series index, never a scan
// of the lane.
func TestPreviewReadsAreIndexProbes(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.conn.Exec(ctx, "set enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ lane, index, table string }{
		{"property", "property_series_idx", "property"},
		{"metric", "series_id_ts_id_idx", "metric_p"},
	} {
		for _, part := range []string{"instances", "seed", "window"} {
			var args []any
			switch part {
			case "instances":
				args = []any{uuid.NewString(), []string{uuid.NewString(), uuid.NewString()}}
			case "seed":
				args = []any{uuid.NewString(), uuid.NewString(), "tcp", time.Now(), time.Now().Add(-time.Hour), 10}
			default:
				args = []any{uuid.NewString(), uuid.NewString(), "tcp", time.Now(), 10}
			}
			rows, err := f.conn.Query(ctx, "explain "+storage.ExportPreviewSQL(tc.lane, part), args...)
			if err != nil {
				t.Fatalf("explain %s %s: %v", tc.lane, part, err)
			}
			var plan string
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					t.Fatal(err)
				}
				plan += line + "\n"
			}
			rows.Close()
			if !strings.Contains(plan, tc.index) || strings.Contains(plan, "Seq Scan on "+tc.table+" ") || strings.Contains(plan, "Seq Scan on "+tc.table+"\n") || tc.table == "metric_p" && strings.Contains(plan, "Seq Scan on metric_p") {
				t.Errorf("the %s lane's %s read is not an index probe on %s:\n%s", tc.lane, part, tc.index, plan)
			}
		}
	}
}
