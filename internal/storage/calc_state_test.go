package storage_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func self(ids ...string) scope.Set { return scope.Set{SelfIDs: ids} }

// TestCalcRuleState proves a rule's live state (#998): one entry per owner
// the caller can read, its name resolved at read time, with the last value,
// version, what was missing or skipped, and whether it is pending; an
// out-of-scope member shows as a count, never a name.
func TestCalcRuleState(t *testing.T) {
	r := newSystemRig(t)
	r.assign("sys-1", "left", "m1")
	r.assign("sys-1", "right", "m2")
	r.systemRule("kind-mean", storage.CalcRuleSelector{SystemTypes: []string{"kind-a"}}, rules.CalcReduce{Reducer: "average"})
	r.temp("m1", "", 20)
	r.drain()
	// A component rule on two components, one of them still pending.
	if _, err := r.create(storage.CalcRuleSpec{Name: "echo", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{As: "t", Key: "occupancy-count"}}, Reduce: rules.CalcReduce{Expr: "t * 1.0"}, OutputKey: "temp-sum"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"m3", "m4"} {
		if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.ids[c], Key: "occupancy-count", Value: 2}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.gw.DrainCalcPending(r.ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.ids["m4"], Key: "occupancy-count", Value: 3}}); err != nil {
		t.Fatal(err)
	}

	// The fleet view of the system rule: sys-1 computed (m2 skipped, by
	// name), sys-2 with no members.
	all, err := r.gw.CalcRuleState(r.ctx, "kind-mean", r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]storage.CalcOwnerState{}
	for _, o := range all.Owners {
		byName[o.OwnerName] = o
	}
	s1 := byName["sys-1"]
	if s1.OwnerID != r.ids["sys-1"] || s1.OwnerKind != "system" || string(s1.Value) != "20" || s1.RuleVersion != 1 || s1.ComputedAt == nil ||
		len(s1.Skipped) != 1 || s1.Skipped[0].ComponentName != "m2" || s1.SkippedHidden != 0 || s1.Pending {
		t.Fatalf("sys-1 state %+v, want 20 at v1 with m2 skipped by name", s1)
	}
	if s2 := byName["sys-2"]; s2.NoMembersSince == nil || len(s2.Missing) != 1 {
		t.Fatalf("sys-2 state %+v, want no members and its input missing", s2)
	}
	if _, ok := byName["sys-3"]; ok {
		t.Fatal("sys-3, outside the selector, has state")
	}

	// A viewer of sys-1 and m1 alone: sys-1 only, m2 a count, never a name.
	scoped, err := r.gw.CalcRuleState(r.ctx, "kind-mean", self(r.ids["m1"]), self(r.ids["sys-1"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Owners) != 1 || scoped.Owners[0].OwnerName != "sys-1" || len(scoped.Owners[0].Skipped) != 0 || scoped.Owners[0].SkippedHidden != 1 {
		t.Fatalf("scoped state %+v, want sys-1 alone with one hidden skipped member", scoped.Owners)
	}

	// The component rule: m4 pending again, m3 not; a viewer of m3 sees m3.
	echo, err := r.gw.CalcRuleState(r.ctx, "echo", r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	if len(echo.Owners) != 2 || echo.PendingDepth != 1 {
		t.Fatalf("echo state %+v, want two owners and one pending", echo)
	}
	for _, o := range echo.Owners {
		if want := o.OwnerName == "m4"; o.Pending != want || string(o.Value) != "2" {
			t.Errorf("echo on %s: %+v, want value 2, pending %v", o.OwnerName, o, want)
		}
	}
	one, err := r.gw.CalcRuleState(r.ctx, "echo", self(r.ids["m3"]), scope.Set{})
	if err != nil || len(one.Owners) != 1 || one.Owners[0].OwnerName != "m3" {
		t.Fatalf("a viewer of m3: %+v (%v)", one, err)
	}
	if _, err := r.gw.CalcRuleState(r.ctx, "nobody", r.all, r.all); !errors.Is(err, storage.ErrCalcRuleNotFound) {
		t.Errorf("an unknown rule: %v", err)
	}
}

// tableCounts is the row counts a preview must leave unchanged.
func (r *evalRig) tableCounts() string {
	r.t.Helper()
	return scalar(r.t, r.conn, `select (select count(*) from metric)::text || ',' || (select count(*) from property)::text || ',' ||
		(select count(*) from calc_pending)::text || ',' || (select count(*) from calc_rule_state)::text || ',' ||
		(select count(*) from audit_log)::text || ',' || (select count(*) from calc_rule)::text`)
}

// TestCalcPreview proves a candidate rule is computed for each selected
// owner in scope with the tick's own reduce, writing nothing (#998): the
// value it would write is the value the tick then writes; whether it differs
// from the current output; missing and skipped inputs; bounded by the owner
// cap; and a candidate that could not be saved is refused as a save would be.
func TestCalcPreview(t *testing.T) {
	r := newSystemRig(t)
	r.assign("sys-1", "left", "m1")
	r.assign("sys-1", "right", "m2")
	r.assign("sys-2", "left", "m3")
	r.temp("m1", "", 20)
	r.temp("m2", "", 30)
	r.drain()
	spec := storage.CalcRuleSpec{OwnerKind: rules.OwnerSystem, Selector: storage.CalcRuleSelector{SystemTypes: []string{"kind-a"}},
		Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "average"}, OutputKey: "avg-temp"}
	before := r.tableCounts()
	pv, err := r.gw.PreviewCalcRule(r.ctx, spec, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	if after := r.tableCounts(); after != before {
		t.Fatalf("the preview wrote: counts %s, then %s", before, after)
	}
	got := map[string]storage.CalcPreviewOwner{}
	for _, o := range pv.Owners {
		got[o.OwnerName] = o
	}
	if o := got["sys-1"]; string(o.Value) != "25" || !o.Differs {
		t.Fatalf("sys-1 preview %+v, want 25, differing from no current value", o)
	}
	if o := got["sys-2"]; o.Value != nil || len(o.Missing) != 1 || len(o.Skipped) != 1 {
		t.Fatalf("sys-2 preview %+v, want missing with m3 skipped", o)
	}
	if pv.OwnersTotal != 2 || pv.OwnersJudged != 2 || pv.Truncated {
		t.Fatalf("preview bounds %+v, want 2 of 2", pv)
	}
	// The tick writes what the preview said.
	spec.Name = "kind-mean"
	if _, err := r.create(spec); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if out := r.systemOutput("sys-1"); out != "25 kind-mean 1 system" {
		t.Fatalf("the tick wrote %q, the preview said 25", out)
	}
	// Previewing the saved rule (by its name) now matches the current output.
	pv, err = r.gw.PreviewCalcRule(r.ctx, spec, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range pv.Owners {
		if o.OwnerName == "sys-1" && o.Differs {
			t.Fatalf("sys-1 preview %+v differs from the output the tick just wrote", o)
		}
	}
	// A scoped viewer previews only its own owners.
	pv, err = r.gw.PreviewCalcRule(r.ctx, spec, self(r.ids["m1"]), self(r.ids["sys-1"]))
	if err != nil || len(pv.Owners) != 1 || pv.Owners[0].OwnerName != "sys-1" || pv.Owners[0].SkippedHidden != 0 {
		t.Fatalf("a scoped preview %+v (%v), want sys-1 alone", pv, err)
	}
	// Truncation at the owner cap.
	restore := storage.SetCalcPreviewOwnerCap(1)
	pv, err = r.gw.PreviewCalcRule(r.ctx, spec, r.all, r.all)
	restore()
	if err != nil || len(pv.Owners) != 1 || pv.OwnersJudged != 1 || pv.OwnersTotal != 2 || !pv.Truncated || pv.OwnerCap != 1 {
		t.Fatalf("a capped preview %+v (%v), want 1 of 2, truncated", pv, err)
	}
	// What a save would refuse, the preview refuses: a second writer, a
	// reduce that does not compile, a cycle.
	other := spec
	other.Name = "another"
	if _, err := r.gw.PreviewCalcRule(r.ctx, other, r.all, r.all); !errors.Is(err, storage.ErrCalcRuleInvalid) || !strings.Contains(err.Error(), "kind-mean") {
		t.Errorf("a second writer: %v", err)
	}
	bad := spec
	bad.Reduce = rules.CalcReduce{Expr: "members +"}
	var ce *rules.CalcError
	if _, err := r.gw.PreviewCalcRule(r.ctx, bad, r.all, r.all); !errors.As(err, &ce) || ce.Field != "reduce.expr" {
		t.Errorf("a broken reduce: %v", err)
	}
	loop := storage.CalcRuleSpec{OwnerKind: rules.OwnerComponent, Inputs: []rules.CalcInput{{As: "a", Key: "avg-temp"}},
		Reduce: rules.CalcReduce{Expr: "a"}, OutputKey: "room-temperature"}
	if _, err := r.gw.PreviewCalcRule(r.ctx, loop, r.all, r.all); !errors.Is(err, rules.ErrCalcCycle) {
		t.Errorf("a cycle: %v", err)
	}
	if after := r.tableCounts(); after == "" {
		t.Fatal("counts")
	}
}
