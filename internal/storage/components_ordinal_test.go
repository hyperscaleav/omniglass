package storage_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The ordinal is a stored fact (#681). Everything below asserts against the
// COLUMN rather than the digits in the name, because the point of the slice is
// that the two stop being the same statement: a name is a string an operator
// may take ownership of, an ordinal is a number the platform allocated and
// still owns.

// TestGeneratedNameRecordsItsOrdinal proves the first acceptance behavior: the
// number the generator picked is written down beside the name it was formatted
// into, and it is the number the name shows.
func TestGeneratedNameRecordsItsOrdinal(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	qm55 := "samsung-qm55"
	first, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if first.Name != "display-1" {
		t.Fatalf("first name = %q, want display-1", first.Name)
	}
	if first.Ordinal == nil || *first.Ordinal != 1 {
		t.Fatalf("first ordinal = %v, want 1", ordstr(first.Ordinal))
	}

	second, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if second.Ordinal == nil || *second.Ordinal != 2 {
		t.Fatalf("second ordinal = %v, want 2", ordstr(second.Ordinal))
	}

	// The read path carries it too, not just the write's returned row.
	got, err := gw.GetComponent(ctx, second.ID, all)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Ordinal == nil || *got.Ordinal != 2 {
		t.Fatalf("ordinal on read = %v, want 2", ordstr(got.Ordinal))
	}
}

// TestOperatorTypedNameHasNoOrdinal proves the nullable half of the column:
// a name the operator typed carries no number the platform owns, and absent is
// how that is written down. Nothing is inferred from the digits: the name here
// is deliberately shaped exactly like one the generator would mint.
func TestOperatorTypedNameHasNoOrdinal(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	qm55 := "samsung-qm55"
	typed, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", ProductName: &qm55}, all, all, all)
	if err != nil {
		t.Fatalf("create typed: %v", err)
	}
	if typed.NameGenerated {
		t.Fatalf("precondition: an explicitly-named create reports NameGenerated = true")
	}
	if typed.Ordinal != nil {
		t.Fatalf("ordinal on an operator-typed name = %v, want absent", ordstr(typed.Ordinal))
	}
}

// TestOperatorTypedNameHoldsItsSlot is the database-level twin of
// TestPickOrdinalOperatorTypedGeneratedShapeBlocks, and the case that decides
// what allocation may read. The hand-typed "display-1" has no ordinal (the
// platform never named it) but it does hold the NAME, and the scoped-name
// unique index is on the name. An allocator reading only the ordinals the
// platform recorded would see an empty set, pick 1, and mint a name this row
// already holds: a 23505 that aborts the whole create transaction.
func TestOperatorTypedNameHoldsItsSlot(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	qm55 := "samsung-qm55"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", ProductName: &qm55}, all, all, all); err != nil {
		t.Fatalf("create the hand-typed occupant: %v", err)
	}

	generated, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all)
	if err != nil {
		t.Fatalf("create generated beside a hand-typed name in the mint's shape: %v", err)
	}
	if generated.Name != "display-2" {
		t.Fatalf("generated name = %q, want display-2 (display-1 is held by a hand-typed row)", generated.Name)
	}
	if generated.Ordinal == nil || *generated.Ordinal != 2 {
		t.Fatalf("generated ordinal = %v, want 2", ordstr(generated.Ordinal))
	}
}

// TestRenameClearsTheOrdinalAndResetReMints proves the second and third
// acceptance behaviors together, because they are one round trip: a rename
// hands the name to the operator and the platform gives up the number with it,
// and :resetName takes both back.
func TestRenameClearsTheOrdinalAndResetReMints(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	qm55 := "samsung-qm55"
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Ordinal == nil {
		t.Fatalf("precondition: a generated create recorded no ordinal")
	}

	renamed, err := gw.RenameComponent(ctx, "", c.ID, "front-desk-display", all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Ordinal != nil {
		t.Fatalf("ordinal after rename = %v, want absent (the operator holds the name, the platform owns no number for it)", ordstr(renamed.Ordinal))
	}
	// And on a re-read, not just in the returned row.
	reread, err := gw.GetComponent(ctx, c.ID, all)
	if err != nil {
		t.Fatalf("re-read after rename: %v", err)
	}
	if reread.Ordinal != nil {
		t.Fatalf("ordinal on re-read after rename = %v, want absent", ordstr(reread.Ordinal))
	}

	reset, err := gw.ResetComponentName(ctx, "", c.ID, all, all)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.Name != "display-1" {
		t.Fatalf("name after reset = %q, want display-1", reset.Name)
	}
	if reset.Ordinal == nil || *reset.Ordinal != 1 {
		t.Fatalf("ordinal after reset = %v, want 1 (the pen came back, and the number with it)", ordstr(reset.Ordinal))
	}
}

// TestMoveReRecordsTheOrdinal proves a still-platform-owned name that
// recomputes on a placement change re-records the number in the same write,
// rather than leaving the old bucket's ordinal behind beside the new name.
func TestMoveReRecordsTheOrdinal(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	qm55 := "samsung-qm55"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all); err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if second.Ordinal == nil || *second.Ordinal != 2 {
		t.Fatalf("precondition: second ordinal = %v, want 2", ordstr(second.Ordinal))
	}

	root := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "ordinal-move-root"}, all)
	moved, err := gw.MoveComponent(ctx, "", second.ID, storage.ComponentMove{ParentName: &root.Name}, all, all, all)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.Name != "display-1" {
		t.Fatalf("name after move = %q, want display-1 (a fresh bucket)", moved.Name)
	}
	if moved.Ordinal == nil || *moved.Ordinal != 1 {
		t.Fatalf("ordinal after move = %v, want 1 (re-recorded with the new name, not carried over)", ordstr(moved.Ordinal))
	}
}

// TestReclassifyReRecordsTheOrdinal is the same guarantee on the other
// recompute trigger: a product reclassify moves a platform-owned name to the
// new type's stem, and the number moves with it.
func TestReclassifyReRecordsTheOrdinal(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	qm55, mic := "samsung-qm55", "shure-mxa920"
	// A mic already sitting in the bucket, so the reclassified row cannot land
	// back on ordinal 1 and pass by accident.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &mic}, all, all, all); err != nil {
		t.Fatalf("create the sitting mic: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all)
	if err != nil {
		t.Fatalf("create the display: %v", err)
	}
	if c.Ordinal == nil || *c.Ordinal != 1 {
		t.Fatalf("precondition: display ordinal = %v, want 1", ordstr(c.Ordinal))
	}

	after, err := gw.UpdateComponent(ctx, "", c.ID, storage.ComponentPatch{ProductName: &mic}, all, all)
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if after.Name != "mic-2" {
		t.Fatalf("name after reclassify = %q, want mic-2", after.Name)
	}
	if after.Ordinal == nil || *after.Ordinal != 2 {
		t.Fatalf("ordinal after reclassify = %v, want 2", ordstr(after.Ordinal))
	}
}

// TestBareRenderReadsTheStoredOrdinal proves the render consumes the fact: a
// generated row compacts to <abbrev><ordinal>, and a renamed row whose name
// still looks exactly like a generated one is left alone, which is the #654
// guarantee surviving the mechanism change. samsung-qm55 classifies under
// component_type "display" (stem "display", abbrev "fp").
func TestBareRenderReadsTheStoredOrdinal(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	qm55 := "samsung-qm55"
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := gw.GetComponent(ctx, c.ID, all)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Renders.Bare != "fp1" {
		t.Fatalf("bare render of a generated unplaced component = %q, want fp1", got.Renders.Bare)
	}

	// Renamed to a string in the generator's own shape. The digits are not
	// evidence; the absent column is.
	if _, err := gw.RenameComponent(ctx, "", c.ID, "display-9", all, all); err != nil {
		t.Fatalf("rename: %v", err)
	}
	renamed, err := gw.GetComponent(ctx, c.ID, all)
	if err != nil {
		t.Fatalf("get after rename: %v", err)
	}
	if renamed.Renders.Bare != "display-9" {
		t.Fatalf("bare render of an operator-renamed component = %q, want display-9 (never restamped)", renamed.Renders.Bare)
	}
}

// TestStemlessAllocationWorks is the risk this slice exists to retire. Slice 7
// of #657 needs a positional type (a floor, a rack unit) whose ordinal IS its
// name, and the old allocator could not count one: with an empty stem its
// sibling filter was a bare "-" prefix that no name ever matched, so it
// returned 1 forever and the second row collided with the first.
//
// Nothing calls this with an empty stem yet, so it is asserted here at the
// storage layer, against a real database, through the same advisory lock and
// the same placement scoping every other generation goes through.
func TestStemlessAllocationWorks(t *testing.T) {
	ctx := context.Background()
	pg, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(pg.Close)
	if err := seed.Run(ctx, pg); err != nil {
		t.Fatalf("seed: %v", err)
	}

	name, ordinal, err := pg.ExportGenerateName(ctx, "", nil, nil, nil)
	if err != nil {
		t.Fatalf("stem-less generate on an empty bucket: %v", err)
	}
	if name != "1" || ordinal != 1 {
		t.Fatalf("stem-less generate on an empty bucket = (%q, %d), want (\"1\", 1)", name, ordinal)
	}

	// Occupy it for real, through the ordinary create path, so the second
	// allocation reads a row the database actually holds.
	qm55 := "samsung-qm55"
	if _, err := pg.CreateComponent(ctx, "", storage.ComponentSpec{Name: "1", ProductName: &qm55}, all, all, all); err != nil {
		t.Fatalf("create a component named %q: %v", "1", err)
	}
	name, ordinal, err = pg.ExportGenerateName(ctx, "", nil, nil, nil)
	if err != nil {
		t.Fatalf("stem-less generate beside an occupied ordinal: %v", err)
	}
	if name != "2" || ordinal != 2 {
		t.Fatalf("stem-less generate beside %q = (%q, %d), want (\"2\", 2)", "1", name, ordinal)
	}

	// A stemmed sibling in the same bucket occupies nothing in the stem-less
	// space, the same separation stems already have from each other.
	if _, err := pg.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all); err != nil {
		t.Fatalf("create a stemmed sibling: %v", err)
	}
	name, ordinal, err = pg.ExportGenerateName(ctx, "", nil, nil, nil)
	if err != nil {
		t.Fatalf("stem-less generate beside a stemmed sibling: %v", err)
	}
	if name != "2" || ordinal != 2 {
		t.Fatalf("stem-less generate beside display-1 = (%q, %d), want (\"2\", 2)", name, ordinal)
	}
}

// TestStoredOrdinalsRecomputeToThemselves is the recompute-and-compare
// invariant: for every component the platform named, re-running allocation
// against the live estate (excluding the row's own name from the bucket, as
// every recompute site does) must return the number and the name the row
// already carries. A stored ordinal that has gone stale, or a name that no
// longer matches the number recorded beside it, fails here rather than
// surviving unnoticed until a label is printed from it.
//
// The estate is built without deletes, deliberately: allocation reuses the
// lowest free ordinal, so deleting a row genuinely does free its number for
// the next create, and a recompute after a delete is entitled to differ from
// what was stored. The invariant this asserts is that nothing DRIFTS, not that
// ordinals are immutable.
func TestStoredOrdinalsRecomputeToThemselves(t *testing.T) {
	ctx := context.Background()
	pg, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(pg.Close)
	if err := seed.Run(ctx, pg); err != nil {
		t.Fatalf("seed: %v", err)
	}

	qm55, mic := "samsung-qm55", "shure-mxa920"
	if _, err := pg.CreateLocation(ctx, "", storage.LocationSpec{Name: "invariant-room", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create location: %v", err)
	}
	room := "invariant-room"
	parent := mustCreateComponent(t, pg, storage.ComponentSpec{Name: "invariant-rack"}, all)

	// Every bucket, both stems, and both pens: generated rows, an
	// operator-typed one, a renamed one, a reset one, and a moved one.
	for range 3 {
		if _, err := pg.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all); err != nil {
			t.Fatalf("create orphan display: %v", err)
		}
	}
	for range 2 {
		if _, err := pg.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &mic, LocationName: &room}, all, all, all); err != nil {
			t.Fatalf("create located mic: %v", err)
		}
	}
	// A display already sitting in the room, so the child display moved into it
	// below must land on ordinal 2. Without it the moved row's OLD ordinal (1,
	// from its parent bucket) would still equal what a recompute produces, and
	// a move that forgot to re-record would slip through unnoticed.
	if _, err := pg.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55, LocationName: &room}, all, all, all); err != nil {
		t.Fatalf("create located display: %v", err)
	}
	child, err := pg.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55, ParentName: &parent.Name}, all, all, all)
	if err != nil {
		t.Fatalf("create child display: %v", err)
	}
	if _, err := pg.CreateComponent(ctx, "", storage.ComponentSpec{Name: "hand-typed-display", ProductName: &qm55}, all, all, all); err != nil {
		t.Fatalf("create hand-typed: %v", err)
	}
	// Generated and then renamed, and LEFT renamed: the row that catches a
	// rename which hands over the name without giving up the number.
	frozen, err := pg.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &qm55}, all, all, all)
	if err != nil {
		t.Fatalf("create to-be-frozen: %v", err)
	}
	if _, err := pg.RenameComponent(ctx, "", frozen.ID, "frozen-display", all, all); err != nil {
		t.Fatalf("rename the frozen row: %v", err)
	}
	renamed, err := pg.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &mic}, all, all, all)
	if err != nil {
		t.Fatalf("create to-be-renamed: %v", err)
	}
	if _, err := pg.RenameComponent(ctx, "", renamed.ID, "operator-mic", all, all); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := pg.ResetComponentName(ctx, "", renamed.ID, all, all); err != nil {
		t.Fatalf("reset: %v", err)
	}
	// Out of the parent bucket and into the room's, which is a real bucket
	// change: a parent wins over a location in nameGenScopeKey, so clearing it
	// is what makes this a move between buckets rather than within one.
	if _, err := pg.MoveComponent(ctx, "", child.ID, storage.ComponentMove{ParentName: strptr(""), LocationName: &room}, all, all, all); err != nil {
		t.Fatalf("move: %v", err)
	}

	components, err := pg.ListComponents(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(components) == 0 {
		t.Fatal("the estate under test is empty, so this invariant proves nothing")
	}
	checked := 0
	for _, c := range components {
		if c.Ordinal == nil {
			if c.NameGenerated {
				t.Errorf("component %q is platform-named but records no ordinal", c.Name)
			}
			continue
		}
		if !c.NameGenerated {
			t.Errorf("component %q is operator-named but still records ordinal %d", c.Name, *c.Ordinal)
			continue
		}
		if c.ProductID == nil {
			t.Errorf("component %q has an ordinal but no product to resolve a stem from", c.Name)
			continue
		}
		stem, err := pg.ExportStemForProduct(ctx, *c.ProductID)
		if err != nil {
			t.Fatalf("resolve stem for %q: %v", c.Name, err)
		}
		name, ordinal, err := pg.ExportGenerateName(ctx, stem, c.ParentID, c.LocationID, &c.ID)
		if err != nil {
			t.Fatalf("recompute the name of %q: %v", c.Name, err)
		}
		if name != c.Name || ordinal != *c.Ordinal {
			t.Errorf("component %q (ordinal %d) recomputes to (%q, %d): the stored value has drifted from what allocation produces", c.Name, *c.Ordinal, name, ordinal)
		}
		checked++
	}
	// Three orphan displays, two located mics, the located display, the child
	// display that was then moved, and the mic that was renamed and reset. The
	// three operator-named rows (two hand-typed, one renamed and left frozen)
	// carry no ordinal and are not among them, which is the point of counting
	// rather than trusting the loop ran.
	if checked != 8 {
		t.Fatalf("%d platform-named components were checked, want 8: the estate did not build as intended", checked)
	}
}

// ordstr renders a nullable ordinal for a failure message, since "%v" on a
// *int prints an address rather than the number or its absence.
func ordstr(o *int) string {
	if o == nil {
		return "absent"
	}
	return strconv.Itoa(*o)
}
