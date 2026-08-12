package storage_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
)

// The round-trip cost of the label recompute and its cascade (#685), measured
// with #650's counting instrument rather than asserted in a comment.
//
// This is the property the slice most needs held. Both halves walk a set whose
// size is the estate's (every component in scope, every row placed at one
// location), both run inside a write transaction, and the obvious
// implementation of either is one query per row. #648 exists because a per-row
// version of exactly this walk was an N+1, and a comment saying "resolved once
// per distinct product" is worth nothing next to a number that does not move
// when the row count quadruples.
//
// The seam is the POOL (storagetest.NewCountingDB), not a wrapped querier: the
// recompute begins its own transaction off p.pool, so a counter handed to it as
// an argument would observe nothing at all and report a flat, fictional number
// forever. See storagetest/querycount's doc comment for that hazard in full.

// labelEstate is a seeded counting gateway plus a building and rooms.
func labelEstate(t *testing.T, roomCount int) (storage.Gateway, *querycount.Counter, []string) {
	t.Helper()
	ctx := context.Background()
	gw, counter := storagetest.NewCountingDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "building"}, all); err != nil {
		t.Fatalf("create building: %v", err)
	}
	names := make([]string, 0, roomCount)
	for i := range roomCount {
		name := fmt.Sprintf("room-%d", i)
		if _, err := gw.CreateLocation(ctx, "",
			storage.LocationSpec{Name: name, LocationType: "room", ParentName: strptr("hq")}, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		names = append(names, name)
	}
	counter.Reset()
	return gw, counter, names
}

// TestTheBulkRecomputeCostIsFlatInEstateSize: a recompute over four components
// and a recompute over twenty must cost the same number of statements.
//
// The rows are spread across rooms and across two products, because both are
// dimensions the implementation memoizes on: a fixture whose rows all shared
// one room and one product would hold both memo tables at size one and a
// per-row loop would look exactly as flat as a batch. Distinct products stay
// FIXED between the two measurements while row count grows, which is the honest
// shape of the claim: cost is bounded by the registry, not by the estate.
func TestTheBulkRecomputeCostIsFlatInEstateSize(t *testing.T) {
	gw, counter, rooms := labelEstate(t, 4)
	ctx := context.Background()
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "my-mic", DisplayName: "My Mic", ComponentType: "mic",
	}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	products := []string{qm55, "my-mic"}
	stock := func(from, to int) {
		for i := from; i < to; i++ {
			if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
				ProductName:  strptr(products[i%len(products)]),
				LocationName: strptr(rooms[i%len(rooms)]),
			}, all); err != nil {
				t.Fatalf("create component %d: %v", i, err)
			}
		}
	}
	recompute := func() (int, error) {
		// A rule that reads every placement fact, so no row is skipped for
		// having nothing to re-render.
		changes, err := gw.RecomputeLabels(ctx, "", "component", all, all)
		return len(changes), err
	}

	stock(0, 4)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}} {{.Ordinal}}"); err != nil {
		t.Fatalf("set rule: %v", err)
	}
	small := measure(t, counter, recompute)

	stock(4, 24)
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.TypeName}} at {{.LocationLabel}} #{{.Ordinal}}"); err != nil {
		t.Fatalf("set the second rule: %v", err)
	}
	large := measure(t, counter, recompute)

	// Twelve is calibration, not doctrine: the transaction pair, the advisory
	// lock, the row scan, the placement batch, the global rule, one
	// classification resolve per distinct product (each of which walks its
	// component_type chain), the batched UPDATE and the audit row. It is here
	// so a change that keeps the read flat but doubles its constant is still
	// visible.
	assertFlatCost(t, small, large, 12)
}

// TestTheLocationCascadeCostIsFlatInWhatIsPlacedThere: a rename of a room
// holding one component must cost the same as a rename of a room holding
// twenty. This is the cascade's own blast radius, and the one an operator can
// reach by accident: renaming a busy room is an ordinary afternoon.
func TestTheLocationCascadeCostIsFlatInWhatIsPlacedThere(t *testing.T) {
	gw, counter, rooms := labelEstate(t, 2)
	ctx := context.Background()
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.LocationLabel}} {{.TypeName}} {{.Ordinal}}"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	if _, err := gw.SetLabelRule(ctx, "", "system", "{{.LocationLabel}} {{.TypeName}}"); err != nil {
		t.Fatalf("system rule: %v", err)
	}
	stock := func(room string, n int, tag string) {
		for range n {
			if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
				ProductName: strptr(qm55), LocationName: &room,
			}, all); err != nil {
				t.Fatalf("create component: %v", err)
			}
		}
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
			Name: "sys-" + tag, SystemTypeID: strptr("board"), LocationName: &room,
		}, all); err != nil {
			t.Fatalf("create system: %v", err)
		}
	}
	stock(rooms[0], 1, "quiet")
	stock(rooms[1], 20, "busy")

	// The measured window holds the rename and NOTHING else. Counting the rows
	// placed at the room is the dimension flatness is claimed in, so it has to
	// come from the database rather than from the fixture's own arithmetic, but
	// it must not be charged to the reading: a list whose cost grows with the
	// estate inside the window would swamp what is being measured.
	measureRename := func(from, to string) reading {
		t.Helper()
		placed := 0
		cs, err := gw.ListComponents(ctx, all)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, c := range cs {
			if c.LocationName != nil && *c.LocationName == from {
				placed++
			}
		}
		counter.Reset()
		if _, err := gw.RenameLocation(ctx, "", from, to, all, all); err != nil {
			t.Fatalf("rename %s: %v", from, err)
		}
		return reading{rows: placed, n: counter.N(), stmts: counter.Summary()}
	}
	quiet := measureRename(rooms[0], "room-quiet")
	busy := measureRename(rooms[1], "room-busy")
	// Calibration: the transaction pair, the resolve, the rename UPDATE, the
	// location's own stamp (settings, global rule, type facts, and the compare
	// that finds nothing to write), then the cascade's two arms, each an
	// advisory lock, a scoped scan, a placement batch, its classification
	// resolves and a batched UPDATE, and finally the audit row.
	assertFlatCost(t, quiet, busy, 26)
}

// TestAPlacedCreateStillCostsAFixedNumberOfStatements pins the write path the
// placement keys made more expensive, so the next slice to touch it sees the
// number move rather than discovering it at estate scale.
//
// It is a CEILING and an equality, not a flatness claim: a create is one row,
// so there is no dimension to be flat in. What it catches is a placement
// resolve that grows a second round trip, and a create whose cost depends on
// how full the room already is.
func TestAPlacedCreateStillCostsAFixedNumberOfStatements(t *testing.T) {
	gw, counter, rooms := labelEstate(t, 1)
	ctx := context.Background()
	if _, err := gw.SetLabelRule(ctx, "", "component", "{{.SystemTypeLabel}} {{.LocationLabel}} {{.TypeName}} {{.Ordinal}}"); err != nil {
		t.Fatalf("component rule: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "sys-a", SystemTypeID: strptr("board"), LocationName: &rooms[0],
	}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	create := func() (int, error) {
		_, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
			ProductName: strptr(qm55), LocationName: &rooms[0], SystemName: strptr("sys-a"),
		}, all)
		return 1, err
	}
	first := measure(t, counter, create)
	// Nineteen more so the room is busy, then measure again: a create must not
	// pay for its neighbours.
	for range 19 {
		if _, err := create(); err != nil {
			t.Fatalf("stock the room: %v", err)
		}
	}
	twentyFirst := measure(t, counter, create)

	if first.n != twentyFirst.n {
		t.Errorf("a create into an empty room costs %d statements and into a room of twenty costs %d: the create is paying for its neighbours\n  empty: %q\n  busy:  %q",
			first.n, twentyFirst.n, first.stmts, twentyFirst.stmts)
	}
	// Nineteen, and every one of them named, because an unexplained ceiling is
	// a number nobody dares lower: begin; resolve the system, the location and
	// the product; the product's component_type and that type's chain walk; the
	// name generator's advisory lock and its sibling-name scan; the insert; the
	// membership lock and the membership insert; the re-read that picks up the
	// primary; the acronym-dictionary read; the label chain and its SECOND walk
	// of the same component_type; the placement resolve; the label UPDATE; the
	// audit row; commit.
	//
	// EXACTLY ONE of those is this slice's: the placement resolve. The
	// duplicated component_type walk is slice 2's, still removable and still
	// not worth the coupling. A create that costs more than this is a round
	// trip somebody should have to justify in a review.
	const ceiling = 19
	if twentyFirst.n > ceiling {
		t.Errorf("a placed, system-bound, generated create costs %d statements, want at most %d\n  %q",
			twentyFirst.n, ceiling, twentyFirst.stmts)
	}
	t.Logf("a placed generated create costs %d statements", twentyFirst.n)
}
