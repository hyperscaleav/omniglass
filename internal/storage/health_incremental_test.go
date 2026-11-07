package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/health"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestHealthIncrementalMatchesFullRecompute is the equivalence #1562 rests on.
// A write records a location's verdict from its children's RECORDED verdicts
// (its own systems and its child locations), not by folding every system in
// its subtree again, so the recorded tree is only right if every write keeps
// every level of it current. This drives a deep tree through every kind of
// write that moves a verdict or the shape under it, and after each one holds
// every recorded verdict to a recompute from scratch: each system's verdict
// derived live from its roles (SystemHealth), each location's the worst of
// those over its whole subtree, walked here in Go from the tree as it stands.
// The subtree fold is also what LocationHealth reports, so the record, the
// report and the oracle all have to agree.
func TestHealthIncrementalMatchesFullRecompute(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()

	// Two campuses, the first with two buildings, a floor level under each,
	// so a write's chain is four locations deep and a move crosses chains.
	// The fixture's hq > hq-b1 > hq-r1 (holding hq-huddle) stays beside it.
	for _, l := range []struct{ name, kind, parent string }{
		{"eq-b1-f1", "floor", "hq-b1"},
		{"eq-b1-f1-r1", "room", "eq-b1-f1"},
		{"eq-b1-f1-r2", "room", "eq-b1-f1"},
		{"hq-b2", "building", "hq"},
		{"eq-b2-f1", "floor", "hq-b2"},
		{"eq-b2-f1-r1", "room", "eq-b2-f1"},
		{"site-2", "campus", ""},
		{"s2-b1", "building", "site-2"},
		{"s2-r1", "room", "s2-b1"},
	} {
		var parent *string
		if l.parent != "" {
			parent = ptrStr(l.parent)
		}
		f.mustLocation(t, ctx, l.name, l.kind, parent)
	}
	pair := f.pairStandard(t, ctx, "eq-pair")
	if err := f.gw.UpsertStandard(ctx, storage.Standard{Name: "eq-main", Label: "Main"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	if _, err := f.gw.SetSystemRole(ctx, "", "standard", "eq-main", storage.SystemRoleSpec{
		Name: "main", Label: "Main", Quorum: 1, Impact: "outage",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	f.staffIn(t, ctx, pair, "pair", "sys-a", "eq-b1-f1-r1", "a1", "a2")
	f.staffIn(t, ctx, pair, "pair", "sys-b", "eq-b1-f1-r2", "b1", "b2")
	f.staffIn(t, ctx, "eq-main", "main", "sys-o", "eq-b1-f1-r2", "o1")
	f.staffIn(t, ctx, pair, "pair", "sys-c", "eq-b2-f1-r1", "c1", "c2")
	f.staffIn(t, ctx, pair, "pair", "sys-d", "s2-r1", "d1", "d2")
	f.assertMatchesFullRecompute(t, ctx, "the staffed fleet")

	alarms := map[string]*storage.Alarm{}
	raise := func(component, severity string) func() error {
		return func() error {
			a, err := f.gw.RaiseAlarm(ctx, "", component, storage.AlarmSpec{Severity: severity, Message: "eq " + component})
			alarms[component] = a
			return err
		}
	}
	clear := func(component string) func() error {
		return func() error { return f.gw.ClearAlarm(ctx, "", component, alarms[component].ID) }
	}
	for _, step := range []struct {
		what string
		do   func() error
	}{
		{"degrade a room deep in the first building", raise("a1", "high")},
		{"take its sibling floor's room out entirely", raise("o1", "high")},
		{"a warning that moves nothing", raise("c1", "warning")},
		{"restore the outage, leaving the degraded room", clear("o1")},
		{"unstaff a role below its quorum", func() error {
			return f.gw.UnassignRole(ctx, "", "sys-b", "pair", "b2", f.all, f.all, f.all, f.all)
		}},
		{"move the degraded system to the other campus", func() error {
			_, err := f.gw.MoveSystem(ctx, "", "sys-a", storage.SystemMove{LocationName: ptrStr("s2-r1")}, f.all, f.all, all)
			return err
		}},
		{"move a whole floor, systems and all, into the other building", func() error {
			_, err := f.gw.MoveLocation(ctx, "", "eq-b1-f1", storage.LocationMove{ParentName: ptrStr("hq-b2")}, f.all, f.all)
			return err
		}},
		{"staff the role again", func() error {
			return f.gw.AssignRole(ctx, "", "sys-b", "pair", "b2", f.all, f.all, f.all, f.all)
		}},
		{"take the outage system down again, under its moved floor", raise("o1", "high")},
		{"delete a system", func() error { return f.gw.DeleteSystem(ctx, "", "sys-c", f.all, f.all) }},
		{"clear the moved system's alarm", clear("a1")},
		{"restore the outage", clear("o1")},
	} {
		if err := step.do(); err != nil {
			t.Fatalf("%s: %v", step.what, err)
		}
		f.assertMatchesFullRecompute(t, ctx, step.what)
	}

	// Concurrent writes under shared ancestors: each holds the shared locks in
	// turn, and the one that commits second must read the first's children.
	runTogether(t, raise("b1", "high"), raise("o1", "high"), raise("d1", "high"), raise("bar-1", "high"))
	f.assertMatchesFullRecompute(t, ctx, "four alarms raised together")
	runTogether(t, clear("b1"), clear("o1"), clear("d1"), clear("bar-1"))
	f.assertMatchesFullRecompute(t, ctx, "the same four cleared together")
	f.assertTransitionOnly(t, ctx)
}

// staffIn builds a system conforming to standard in room and staffs role with
// the components, each a new room bar.
func (f *healthFixture) staffIn(t *testing.T, ctx context.Context, standard, role, system, room string, components ...string) {
	t.Helper()
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: system, StandardID: &standard, LocationName: &room,
	}, f.all, all); err != nil {
		t.Fatalf("create system %s: %v", system, err)
	}
	for _, c := range components {
		if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c, ProductName: &f.bar.Name}, f.all, all, all, all); err != nil {
			t.Fatalf("create component %s: %v", c, err)
		}
		if err := f.gw.AssignRole(ctx, "", system, role, c, f.all, f.all, f.all, f.all); err != nil {
			t.Fatalf("assign %s: %v", c, err)
		}
	}
}

// assertMatchesFullRecompute holds every system's and every location's latest
// recorded verdict to the from-scratch answer (see
// TestHealthIncrementalMatchesFullRecompute). An owner with no recorded row
// has never been unhealthy and reads as healthy.
func (f *healthFixture) assertMatchesFullRecompute(t *testing.T, ctx context.Context, after string) {
	t.Helper()
	recorded := map[string]string{}
	rows, err := f.conn.Query(ctx, `
		select coalesce(system_id, location_id), value #>> '{}' from (
			select distinct on (system_id, location_id) system_id, location_id, value from property
			where property_type_id = (select id from property_type where name = 'health')
			  and owner_kind in ('system', 'location') and instance = '' and provenance = 'calculated'
			order by system_id, location_id, id desc) latest`)
	if err != nil {
		t.Fatalf("read recorded verdicts: %v", err)
	}
	for rows.Next() {
		var id, v string
		if err := rows.Scan(&id, &v); err != nil {
			t.Fatalf("scan recorded verdict: %v", err)
		}
		recorded[id] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read recorded verdicts: %v", err)
	}
	read := func(id string) string {
		if v, ok := recorded[id]; ok {
			return v
		}
		return health.Healthy.String()
	}

	// Each system from its roles, live.
	located := map[string][]health.Verdict{}
	rows, err = f.conn.Query(ctx, `select id, name, location_id from system`)
	if err != nil {
		t.Fatalf("read systems: %v", err)
	}
	type system struct{ id, name string }
	var systems []system
	var placed []*string
	for rows.Next() {
		var s system
		var loc *string
		if err := rows.Scan(&s.id, &s.name, &loc); err != nil {
			t.Fatalf("scan system: %v", err)
		}
		systems = append(systems, s)
		placed = append(placed, loc)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read systems: %v", err)
	}
	for i, s := range systems {
		rep, err := f.gw.SystemHealth(ctx, s.name, 0, f.all)
		if err != nil {
			t.Fatalf("system health %s: %v", s.name, err)
		}
		if got := read(s.id); got != rep.Verdict {
			t.Errorf("after %s: system %s recorded %q, a full recompute says %q", after, s.name, got, rep.Verdict)
		}
		if placed[i] != nil {
			located[*placed[i]] = append(located[*placed[i]], health.ParseVerdict(rep.Verdict))
		}
	}

	// Each location as the worst over its whole subtree, walked from the tree.
	rows, err = f.conn.Query(ctx, `select id, name, parent_id from location`)
	if err != nil {
		t.Fatalf("read locations: %v", err)
	}
	type location struct{ id, name string }
	var locations []location
	children := map[string][]string{}
	for rows.Next() {
		var l location
		var parent *string
		if err := rows.Scan(&l.id, &l.name, &parent); err != nil {
			t.Fatalf("scan location: %v", err)
		}
		locations = append(locations, l)
		if parent != nil {
			children[*parent] = append(children[*parent], l.id)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read locations: %v", err)
	}
	var subtree func(id string) []health.Verdict
	subtree = func(id string) []health.Verdict {
		out := append([]health.Verdict(nil), located[id]...)
		for _, c := range children[id] {
			out = append(out, subtree(c)...)
		}
		return out
	}
	for _, l := range locations {
		want := health.RollUp(subtree(l.id)).String()
		if got := read(l.id); got != want {
			t.Errorf("after %s: location %s recorded %q, a full recompute says %q", after, l.name, got, want)
		}
		rep, err := f.gw.LocationHealth(ctx, l.name, 0, f.all)
		if err != nil {
			t.Fatalf("location health %s: %v", l.name, err)
		}
		if rep.Verdict != want {
			t.Errorf("after %s: location %s reports %q, a full recompute says %q", after, l.name, rep.Verdict, want)
		}
	}
}
