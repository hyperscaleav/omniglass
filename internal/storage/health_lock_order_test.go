package storage_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The recompute takes one advisory lock per owner and holds it to commit, so
// the ONLY thing keeping two concurrent recomputes over overlapping chains
// from deadlocking is that they visit their owners in the same order. That is
// a total order or it is nothing: a comparison that leaves two owners tied
// leaves their relative order to the plan, and two recomputes reaching the
// same pair by different routes then take the two locks in opposite orders.
//
// Components and systems get their order from refSet.sorted(), which sorts by
// id. Locations came out of locationsOver ordered by NAME, which stopped being
// a key when #627 scoped name uniqueness to placement: two rooms under
// different buildings can both be 415a, and a tie has no defined order at all.
//
// lockOrderFixture is the smallest estate that makes both halves visible. The
// id order and the name order are deliberately reverses of each other (uuidv7
// is time-ordered, so creation order IS id order), and two rooms share a name,
// each holding a system so the chain can be reached down either arm of
// locationsOver's union: the join arm (a system's placement) or the explicit
// arm (a location named outright). Those are the two production trigger shapes,
// recomputeMovedSystem and recomputeMovedLocation.
type lockOrderFixture struct {
	gw   *storage.PG
	all  scope.Set
	ids  map[string]string // label -> location id
	room []string          // the two same-named room ids, in creation order
	sys  []string          // the system in each of those rooms, same order
}

func newLockOrderFixture(t *testing.T) *lockOrderFixture {
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
	f := &lockOrderFixture{gw: gw, all: scope.Set{All: true}, ids: map[string]string{}}

	// Created oldest first, so ids ascend down this list while names descend:
	// by name the order is 415a, 415a, aa-b1, bb-b2, zz-campus, the exact
	// reverse of the id order for every pair that is not tied.
	f.location(t, ctx, "campus", "zz-campus", nil)
	f.location(t, ctx, "building", "bb-b2", ptrStr("zz-campus"))
	f.location(t, ctx, "building", "aa-b1", ptrStr("zz-campus"))
	// The duplicate: legal since #627, and the tie the old ordering could not
	// break.
	f.location(t, ctx, "room", "415a", ptrStr("bb-b2"))
	f.room = append(f.room, f.ids["415a"])
	f.location(t, ctx, "room", "415a", ptrStr("aa-b1"))
	f.room = append(f.room, f.ids["415a"])
	if f.room[0] == f.room[1] {
		t.Fatalf("the two 415a rooms collapsed into one id (%s): the duplicate-name fixture is not what it claims", f.room[0])
	}
	// Placed by room id, not by name: "415a" is exactly the ambiguous reference
	// #627 made unresolvable.
	for i, name := range []string{"sys-in-first-415a", "sys-in-second-415a"} {
		room := f.room[i]
		s, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{Name: name, LocationName: &room}, f.all, f.all)
		if err != nil {
			t.Fatalf("create system %s: %v", name, err)
		}
		f.sys = append(f.sys, s.ID)
	}
	return f
}

func (f *lockOrderFixture) location(t *testing.T, ctx context.Context, kind, name string, parent *string) {
	t.Helper()
	loc, err := f.gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: name, LocationType: kind, ParentName: parent,
	}, f.all)
	if err != nil {
		t.Fatalf("create location %s/%s: %v", kind, name, err)
	}
	f.ids[name] = loc.ID
}

// allIDsAscending is every location id in the fixture, ascending, which is the
// order a recompute over the whole chain must visit them in.
func (f *lockOrderFixture) allIDsAscending() []string {
	ids := []string{f.ids["zz-campus"], f.ids["bb-b2"], f.ids["aa-b1"], f.room[0], f.room[1]}
	sort.Strings(ids)
	return ids
}

// TestLocationsOverOrdersByID pins the ordering contract recomputeChain and
// lockHealthOwner both state: components, then systems, then locations, each by
// id. Ordering by name satisfied neither, and not only under a tie: the name
// order and the id order are different orders even when every name is distinct.
func TestLocationsOverOrdersByID(t *testing.T) {
	f := newLockOrderFixture(t)
	ctx := context.Background()

	got, err := f.gw.ExportLocationsOver(ctx, nil, f.room)
	if err != nil {
		t.Fatalf("locations over: %v", err)
	}
	want := f.allIDsAscending()
	if len(got) != len(want) {
		t.Fatalf("locations over returned %d locations %v, want the %d in the chain %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("locations over visits %v, want ascending by id %v", got, want)
		}
	}
}

// TestLocationsOverOrderIsIndependentOfItsRoute is the deadlock precondition
// stated as an ordering fact rather than raced for: two recomputes reaching the
// same pair of same-named locations down DIFFERENT arms of locationsOver's
// union must visit them in the same order, or they take the same two locks in
// opposite orders and wait on each other.
//
// The two routes are the two production trigger shapes. Under `order by name`
// the tie went to whatever order the recursive CTE handed the sort, and the
// join arm feeds it in a different order than the explicit arm does, so these
// two really did disagree: the pure-named route visited the two 415a rooms
// newest-first and the mixed route oldest-first.
//
// Its teeth are real but PARTIAL, and worth stating rather than assuming: run
// against the unfixed ordering it caught the disagreement in three runs out of
// five, because the tie order is a property of the plan and the plan is free to
// vary between runs. Ordering by id it is green every time, since an id order is
// total and cannot depend on the route, so it is a detector that sometimes
// misses rather than a test that sometimes fails. TestLocationsOverOrdersByID
// above is the deterministic half.
func TestLocationsOverOrderIsIndependentOfItsRoute(t *testing.T) {
	f := newLockOrderFixture(t)
	ctx := context.Background()

	// recomputeMovedLocation's shape: both rooms named outright.
	viaNames, err := f.gw.ExportLocationsOver(ctx, nil, f.room)
	if err != nil {
		t.Fatalf("locations over (named): %v", err)
	}
	// recomputeMovedSystem's shape: one room reached through the system placed
	// in it, the other named as the one that system left.
	viaSystem, err := f.gw.ExportLocationsOver(ctx, f.sys[:1], f.room[1:])
	if err != nil {
		t.Fatalf("locations over (system plus named): %v", err)
	}
	if len(viaNames) != len(viaSystem) {
		t.Fatalf("the same chain resolved to %d and %d locations: %v vs %v", len(viaNames), len(viaSystem), viaNames, viaSystem)
	}
	for i := range viaNames {
		if viaNames[i] != viaSystem[i] {
			t.Fatalf("the same chain locks in two orders: %v by name, %v through a system", viaNames, viaSystem)
		}
	}
}

// TestConcurrentRecomputesOverDuplicateNames is the acceptance #670 writes:
// two recomputes whose chains share two same-named locations, driven at once,
// both completing rather than deadlocking. The two are seeded down the two
// routes the test above shows can disagree, which is the only reason this one
// could fail at all.
//
// READ IT AS A SMOKE TEST, NOT AS THE GUARD, and that is a measured claim
// rather than a hedge: driven against the unfixed ordering it survived more
// than four thousand paired rounds without a single deadlock. Nothing here
// forces the fatal interleaving. Each recompute takes both room locks with only
// a verdict read and a conditional write between them, and the system-seeded
// side spends three extra statements on its system tier before it reaches the
// rooms at all, so the two arrive at the contested pair systematically out of
// phase and the loser simply queues. Turning that into a reliable failure needs
// a pause injected between the two acquisitions, which would be testing the
// Postgres lock manager rather than this ordering.
//
// The property that actually rules the deadlock out is the total order the two
// tests above assert, both of which fail deterministically without it. This one
// is deliberately small and paired (one pair in flight, never a fan-out) so it
// stays a cheap liveness check and cannot fail for an unrelated reason like a
// starved connection pool.
func TestConcurrentRecomputesOverDuplicateNames(t *testing.T) {
	f := newLockOrderFixture(t)
	ctx := context.Background()

	for round := range 25 {
		var wg sync.WaitGroup
		var byName, bySystem error
		wg.Add(2)
		// recomputeMovedLocation's shape.
		go func() { defer wg.Done(); byName = f.gw.ExportRecompute(ctx, nil, f.room) }()
		// recomputeMovedSystem's shape, over the same two rooms.
		go func() { defer wg.Done(); bySystem = f.gw.ExportRecompute(ctx, f.sys[:1], f.room[1:]) }()
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatalf("round %d: concurrent recomputes did not finish in 30s, so the lock order is not total", round)
		}
		if byName != nil {
			t.Fatalf("round %d: recompute seeded by names: %v", round, byName)
		}
		if bySystem != nil {
			t.Fatalf("round %d: recompute seeded through a system: %v", round, bySystem)
		}
	}
}
