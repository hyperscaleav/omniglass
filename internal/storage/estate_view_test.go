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

// The estate projection is the read behind the estate canvas: every root
// location, the systems beneath it, and one dot per component in each system.
// It is deliberately NOT a component list. The canvas paints thousands of
// squares, and shipping full rows to do it is the thing this exists to avoid,
// so the tests below pin what it carries as hard as what it excludes.
//
// The fixture is two roots of different shape, which is the estate the canvas
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
type estateFixture struct {
	gw  *storage.PG
	all scope.Set
}

func newEstateFixture(t *testing.T) *estateFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f := &estateFixture{gw: gw, all: scope.Set{All: true}}

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

	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "estate-huddle", DisplayName: "Estate Huddle"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	std := "estate-huddle"
	for _, s := range []struct{ name, room string }{{"hq-huddle", "hq-r1"}, {"depot-huddle", "depot-r1"}} {
		room := s.room
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s.name, StandardID: &std, LocationName: &room}, f.all); err != nil {
			t.Fatalf("create system %s: %v", s.name, err)
		}
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", "estate-huddle", storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table microphone", Quorum: 1,
		AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &bar}, f.all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	// The same physical box also serves the depot room. This is the case the dot
	// grid exists to show honestly: one component, two systems depending on it.
	if err := gw.AddMember(ctx, "", "depot-huddle", "bar-1", f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return f
}

func (f *estateFixture) project(t *testing.T, loc, sys, comp scope.Set) *storage.EstateView {
	t.Helper()
	v, err := f.gw.EstateProjection(context.Background(), loc, sys, comp)
	if err != nil {
		t.Fatalf("estate projection: %v", err)
	}
	return v
}

func locNames(v *storage.EstateView) []string {
	out := make([]string, 0, len(v.Locations))
	for _, l := range v.Locations {
		out = append(out, l.Name)
	}
	return out
}

func sysNames(v *storage.EstateView) []string {
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

func TestEstateProjectionCarriesTheWholeInScopeTree(t *testing.T) {
	f := newEstateFixture(t)
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
	byName := map[string]storage.EstateLocation{}
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
// (the canvas still draws) and expensive (an estate-sized component list on
// every paint), so the shape is pinned rather than trusted.
func TestEstateProjectionCarriesDotsNotComponentRows(t *testing.T) {
	f := newEstateFixture(t)
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
// as a ghost everywhere else, so the estate's component count does not
// double-count a box that exists once.
func TestEstateProjectionMarksASharedComponentOwnedOnceAndGhostElsewhere(t *testing.T) {
	f := newEstateFixture(t)
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
func TestEstateProjectionSystemVerdictMatchesTheHealthRead(t *testing.T) {
	f := newEstateFixture(t)
	v := f.project(t, f.all, f.all, f.all)

	rep, err := f.gw.SystemHealth(context.Background(), "hq-huddle", time.Time{}, f.all)
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
func TestEstateProjectionIsEmptyWithoutScopeRatherThanFailing(t *testing.T) {
	f := newEstateFixture(t)
	v := f.project(t, scope.Set{}, scope.Set{}, scope.Set{})
	if len(v.Locations) != 0 || len(v.Systems) != 0 {
		t.Fatalf("an unscoped principal must get an empty canvas, got %d locations and %d systems",
			len(v.Locations), len(v.Systems))
	}
}

// The three tiers are scoped independently, which is what lets a principal who
// may read the place tree but not its components see the shape of their estate
// without its contents.
func TestEstateProjectionScopesEachTierOnItsOwn(t *testing.T) {
	f := newEstateFixture(t)
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
