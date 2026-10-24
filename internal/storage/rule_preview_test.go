package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestPreviewEventRule proves a candidate rule's preview (#959): judged over
// each selected series' samples (the newest before the window, then every
// one in the last 24h, in order, sustains ignored), it reports the edges it
// would have written and the series in force now, and writes nothing.
func TestPreviewEventRule(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	product := f.bar.Name
	for _, name := range []string{"pv-1", "pv-2"} {
		if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, ProductName: &product}, f.all, f.all, f.all, f.all); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	at := func(h float64) time.Time { return now.Add(-time.Duration(h * float64(time.Hour))) }
	put := func(comp, inst, v string, ts time.Time) {
		t.Helper()
		if err := insertProperties(ctx, f.gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: comp,
			Key: "endpoint-reachable", Instance: inst, Value: v, TS: ts}}); err != nil {
			t.Fatal(err)
		}
	}
	// pv-1 tcp: down before the window (in force at its start), down again
	// (no edge), up (clear), down (fire), degraded (holds: still in force).
	put("pv-1", "tcp", "down", at(30))
	put("pv-1", "tcp", "down", at(3))
	put("pv-1", "tcp", "up", at(2))
	put("pv-1", "tcp", "down", at(1))
	put("pv-1", "tcp", "degraded", at(0.5))
	// pv-1 icmp: up throughout, nothing.
	put("pv-1", "icmp", "up", at(5))
	// pv-2 tcp: down only before the window: in force now, no edge.
	put("pv-2", "tcp", "down", at(40))

	counts := func() map[string]int {
		out := map[string]int{}
		for _, tbl := range []string{"event", "alarm", "event_rule_condition", "event_rule_state", "audit_log", "event_rule"} {
			out[tbl] = count(t, f.conn, `select count(*) from `+tbl)
		}
		return out
	}
	before := counts()

	spec := storage.RulePreviewSpec{SampleKey: "endpoint-reachable", Fire: criteria("eq", "down"), Clear: ptrCriteria(criteria("eq", "up")),
		Selector: storage.EventRuleSelector{Components: []string{"pv-1", "pv-2"}}}
	p, err := f.gw.PreviewEventRule(ctx, spec, f.all)
	if err != nil {
		t.Fatal(err)
	}
	if p.Truncated || p.RowCap != storage.RulePreviewRowCap || p.Window != storage.RulePreviewWindow || !p.SustainsIgnored {
		t.Fatalf("preview bounds %+v", p)
	}
	type edge struct{ comp, inst, kind string }
	var edges []edge
	for _, e := range p.Edges {
		edges = append(edges, edge{e.ComponentName, e.Instance, e.Kind})
	}
	want := []edge{{"pv-1", "tcp", "clear"}, {"pv-1", "tcp", "fire"}}
	if len(edges) != len(want) || edges[0] != want[0] || edges[1] != want[1] {
		t.Fatalf("edges %+v, want %+v", edges, want)
	}
	if d := p.Edges[1].At.Sub(at(1)); d < -time.Second || d > time.Second {
		t.Errorf("the fire edge at %v, want the 1h-ago sample", p.Edges[1].At)
	}
	inForce := map[string]*time.Time{}
	for _, s := range p.InForce {
		inForce[s.ComponentName+"/"+s.Instance] = s.Since
	}
	if len(inForce) != 2 {
		t.Fatalf("in force %+v, want pv-1/tcp and pv-2/tcp", p.InForce)
	}
	if s, ok := inForce["pv-1/tcp"]; !ok || s == nil || s.Sub(at(1)).Abs() > time.Second {
		t.Errorf("pv-1/tcp in force since %v, want the 1h-ago fire", s)
	}
	if s, ok := inForce["pv-2/tcp"]; !ok || s != nil {
		t.Errorf("pv-2/tcp in force since %v, want in force with no since (before the window)", s)
	}

	// A selector leaves the rest out; an empty one watches every component.
	only, err := f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "endpoint-reachable", Fire: criteria("eq", "down"),
		Selector: storage.EventRuleSelector{Components: []string{"pv-2"}}}, f.all)
	if err != nil || len(only.Edges) != 0 || len(only.InForce) != 1 || only.InForce[0].ComponentName != "pv-2" {
		t.Fatalf("pv-2 only: %+v (%v)", only, err)
	}

	// The cap bounds the read, and says so.
	restore := storage.SetRulePreviewRowCap(2)
	capped, err := f.gw.PreviewEventRule(ctx, spec, f.all)
	restore()
	if err != nil || !capped.Truncated || capped.RowCap != 2 {
		t.Fatalf("capped: truncated %v cap %d (%v), want truncated at 2", capped.Truncated, capped.RowCap, err)
	}

	// A bad criterion is refused with its position; a bad key too.
	_, err = f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "endpoint-reachable", Fire: rules.Criteria{Expr: "value > 1"}}, f.all)
	var ce *rules.CriteriaError
	var ee *expression.Error
	if !errors.Is(err, storage.ErrEventRuleInvalid) || !errors.As(err, &ce) || !errors.As(err, &ee) || ee.Line != 1 {
		t.Fatalf("a bad criterion: %v", err)
	}
	if _, err := f.gw.PreviewEventRule(ctx, storage.RulePreviewSpec{SampleKey: "nope", Fire: criteria("eq", "x")}, f.all); !errors.Is(err, storage.ErrEventRuleInvalid) {
		t.Fatalf("an unknown key: %v", err)
	}

	after := counts()
	for tbl, n := range before {
		if after[tbl] != n {
			t.Errorf("%s went from %d to %d rows; a preview writes nothing", tbl, n, after[tbl])
		}
	}
}
