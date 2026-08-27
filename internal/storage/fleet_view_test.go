package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// The fleet projection is the read behind the fleet canvas: every root
// location, the systems beneath it, and one dot per component in each system.
// It is deliberately NOT a component list. The canvas paints thousands of
// squares, and shipping full rows to do it is the thing this exists to avoid,
// so the tests below pin what it carries as hard as what it excludes.
//
// The fixture is two roots of different shape, which is the fleet the canvas
// has to survive: nothing in the model requires a building or a floor.
//
//	hq    (campus)   > hq-b1 (building) > hq-r1 (room) -> hq-huddle, staffed
//	depot (building)                    > depot-r1 (room) -> depot-huddle, shared occupant
//
// Two roots, three hops deep against two, and no level in common below the
// root: the seeded type registry allows a building at root, which is what lets
// the fixture prove the canvas needs no fixed campus/building/floor/room ladder.
//
// bar-1 fills a role in hq-huddle and is ALSO a member of depot-huddle, which
// is what the shared-dot rules are read against.
type fleetFixture struct {
	gw  *storage.PG
	dsn string
	all scope.Set
}

func newFleetFixture(t *testing.T) *fleetFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f := &fleetFixture{gw: gw, dsn: dsn, all: scope.Set{All: true}}

	mk := func(name, kind string, parent *string) {
		t.Helper()
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: name, LocationType: kind, ParentName: parent}, f.all); err != nil {
			t.Fatalf("create location %s: %v", name, err)
		}
	}
	mk("hq", "campus", nil)
	mk("hq-b1", "building", ptrStr("hq"))
	mk("hq-r1", "room", ptrStr("hq-b1"))
	mk("depot", "building", nil)
	mk("depot-r1", "room", ptrStr("depot"))

	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "fleet-huddle", Label: "Fleet Huddle"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	std := "fleet-huddle"
	for _, s := range []struct{ name, room string }{{"hq-huddle", "hq-r1"}, {"depot-huddle", "depot-r1"}} {
		room := s.room
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s.name, StandardID: &std, LocationName: &room}, f.all, f.all); err != nil {
			t.Fatalf("create system %s: %v", s.name, err)
		}
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", "fleet-huddle", storage.SystemRoleSpec{
		Name: "table-mic", Label: "Table microphone", Quorum: 1,
		AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	bar := "kestrel-vroom"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &bar}, f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all, f.all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	// The same physical box also serves the depot room. This is the case the dot
	// grid exists to show honestly: one component, two systems depending on it.
	if err := gw.AddMember(ctx, "", "depot-huddle", "bar-1", f.all, f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return f
}

func (f *fleetFixture) project(t *testing.T, loc, sys, comp scope.Set) *storage.FleetView {
	t.Helper()
	v, err := f.gw.FleetProjection(context.Background(), loc, sys, comp)
	if err != nil {
		t.Fatalf("fleet projection: %v", err)
	}
	return v
}

func locNames(v *storage.FleetView) []string {
	out := make([]string, 0, len(v.Locations))
	for _, l := range v.Locations {
		out = append(out, l.Name)
	}
	return out
}

func sysNames(v *storage.FleetView) []string {
	out := make([]string, 0, len(v.Systems))
	for _, s := range v.Systems {
		out = append(out, s.Name)
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestFleetProjectionCarriesTheWholeInScopeTree(t *testing.T) {
	f := newFleetFixture(t)
	v := f.project(t, f.all, f.all, f.all)

	for _, want := range []string{"hq", "hq-b1", "hq-r1", "depot", "depot-r1"} {
		if !has(locNames(v), want) {
			t.Errorf("location %q missing from the projection: %v", want, locNames(v))
		}
	}
	for _, want := range []string{"hq-huddle", "depot-huddle"} {
		if !has(sysNames(v), want) {
			t.Errorf("system %q missing: %v", want, sysNames(v))
		}
	}

	// Every location carries its parent, because the client builds the tree and
	// the bands from this flat list rather than the server nesting it.
	byName := map[string]storage.FleetLocation{}
	for _, l := range v.Locations {
		byName[l.Name] = l
	}
	if byName["hq"].ParentID != nil {
		t.Error("a root location must carry no parent")
	}
	if p := byName["hq-b1"].ParentID; p == nil || *p != byName["hq"].ID {
		t.Error("hq-b1 must carry hq as its parent, or the client cannot build the tree")
	}
	if byName["hq"].LocationType == "" {
		t.Error("a location must carry its type: the band renders a type chip from it")
	}
}

// The whole point of a projection: dots, not rows. A regression here is silent
// (the canvas still draws) and expensive (a fleet-sized component list on
// every paint), so the shape is pinned rather than trusted.
func TestFleetProjectionCarriesDotsNotComponentRows(t *testing.T) {
	f := newFleetFixture(t)
	v := f.project(t, f.all, f.all, f.all)

	var dots int
	for _, s := range v.Systems {
		for _, d := range s.Dots {
			dots++
			if d.ComponentID == "" {
				t.Error("a dot must carry its component id: the canvas navigates on click")
			}
			if d.Verdict == "" {
				t.Error("a dot must carry a verdict: it is the only thing that colours it")
			}
		}
	}
	if dots == 0 {
		t.Fatal("no dots projected")
	}
}

// A shared component is drawn once as owned in its primary system's cluster and
// as a ghost everywhere else, so the fleet's component count does not
// double-count a box that exists once.
func TestFleetProjectionMarksASharedComponentOwnedOnceAndGhostElsewhere(t *testing.T) {
	f := newFleetFixture(t)
	v := f.project(t, f.all, f.all, f.all)

	owned, ghost, shared := 0, 0, 0
	for _, s := range v.Systems {
		for _, d := range s.Dots {
			if d.Shared {
				shared++
				if d.Primary {
					owned++
				} else {
					ghost++
				}
			}
		}
	}
	if shared != 2 {
		t.Fatalf("the shared component should appear in both its systems, got %d appearances", shared)
	}
	if owned != 1 || ghost != 1 {
		t.Errorf("owned=%d ghost=%d, want exactly one of each: a shared box is owned by its primary and a ghost elsewhere", owned, ghost)
	}
}

// A system's verdict comes from the same rollup the health read serves, so the
// canvas and the detail page can never disagree about a room.
func TestFleetProjectionSystemVerdictMatchesTheHealthRead(t *testing.T) {
	f := newFleetFixture(t)
	v := f.project(t, f.all, f.all, f.all)

	rep, err := f.gw.SystemHealth(context.Background(), "hq-huddle", 0, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	for _, s := range v.Systems {
		if s.Name == "hq-huddle" && s.Verdict != rep.Verdict {
			t.Fatalf("projection verdict %q disagrees with the health read %q", s.Verdict, rep.Verdict)
		}
	}
}

// Scope decides what exists, per tier, in the query rather than after it. A
// principal who can read no location sees an empty canvas, which is not an
// error: there is simply nothing of theirs to draw.
func TestFleetProjectionIsEmptyWithoutScopeRatherThanFailing(t *testing.T) {
	f := newFleetFixture(t)
	v := f.project(t, scope.Set{}, scope.Set{}, scope.Set{})
	if len(v.Locations) != 0 || len(v.Systems) != 0 {
		t.Fatalf("an unscoped principal must get an empty canvas, got %d locations and %d systems",
			len(v.Locations), len(v.Systems))
	}
}

// The three tiers are scoped independently, which is what lets a principal who
// may read the place tree but not its components see the shape of their fleet
// without its contents.
func TestFleetProjectionScopesEachTierOnItsOwn(t *testing.T) {
	f := newFleetFixture(t)
	v := f.project(t, f.all, f.all, scope.Set{})

	if len(v.Locations) == 0 {
		t.Fatal("locations must still project when only the component scope is empty")
	}
	for _, s := range v.Systems {
		if len(s.Dots) != 0 {
			t.Fatalf("system %q projected %d dots with no component read scope", s.Name, len(s.Dots))
		}
	}
}

// The projection reads the current verdict for every location and every system
// as a correlated subquery, so those two lookups must be index-backed or the
// canvas seq-scans the telemetry lane once per row (#632 review). The claim is
// put to the PLANNER, not to the catalog: an index that exists but is never
// chosen buys nothing, and asserting its existence alone is how a performance
// guard stays green while the query stays slow.
//
// seqscan is disabled for the question because the fixture's property table is
// tiny and Postgres would rightly scan it whatever exists; what the planner
// reaches for INSTEAD is what it will choose once the lane is real.
func TestFleetProjectionVerdictLookupsAreIndexed(t *testing.T) {
	f := newFleetFixture(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, "set enable_seqscan = off"); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}

	for _, tc := range []struct{ owner, table, index string }{
		// The system arc's index landed with #725; the location arc's mirrors
		// it in this slice's migration. Both are asserted here because the
		// projection is the read that pays for both at fleet width.
		{"system_id", "system", "property_system_owner_idx"},
		{"location_id", "location", "property_location_owner_idx"},
	} {
		rows, err := conn.Query(ctx, `explain select (select v.value #>> '{}' from property v
			where v.`+tc.owner+` = t.id
			  and v.property_type_id = (select id from property_type where name = 'health')
			  and v.instance = ''
			order by v.id desc limit 1) from `+tc.table+` t`)
		if err != nil {
			t.Fatalf("explain %s: %v", tc.owner, err)
		}
		var plan string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatalf("scan plan: %v", err)
			}
			plan += line + "\n"
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("plan rows: %v", err)
		}
		if !strings.Contains(plan, tc.index) {
			t.Errorf("the %s verdict subquery does not reach for %s, so it would scan the telemetry lane once per row.\nplan:\n%s",
				tc.table, tc.index, plan)
		}
	}
}
