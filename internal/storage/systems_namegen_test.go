package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// newSystemNamegenDB is the fixture every case below shares: a seeded registry
// (system_type "board" carries stem "boardroom", "class" carries "classroom")
// and one room to place systems in, since a location is the bucket an operator
// actually creates rooms' systems into.
// It hands back the concrete *PG rather than the Gateway interface because the
// invariant case below drives the generator through the test-only export shim,
// which is deliberately not part of the gateway contract.
func newSystemNamegenDB(t *testing.T) (*storage.PG, context.Context, string) {
	t.Helper()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "namegen-room", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create location: %v", err)
	}
	return gw, ctx, "namegen-room"
}

// TestSystemCreateGeneratesFromTypeStem is the acceptance case: a system
// created with a type and a placement gets a name without the operator typing
// one. The stem is the system_type chain's (ADR-0096's registry, resolved by
// the same walk a label's facts use), and the ordinal is per placement bucket,
// so an identical room elsewhere starts again at the bare name.
func TestSystemCreateGeneratesFromTypeStem(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)

	first, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if first.Name != "boardroom" {
		t.Fatalf("first generated name = %q, want boardroom (the first of its stem carries no ordinal)", first.Name)
	}
	if !first.NameGenerated {
		t.Fatal("first.NameGenerated = false, want true")
	}
	if first.Ordinal == nil || *first.Ordinal != 1 {
		t.Fatalf("first.Ordinal = %s, want 1: a suppressed name still records the number it was minted from", ordstr(first.Ordinal))
	}

	second, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if second.Name != "boardroom-2" {
		t.Fatalf("second generated name = %q, want boardroom-2", second.Name)
	}
	if second.Ordinal == nil || *second.Ordinal != 2 {
		t.Fatalf("second.Ordinal = %s, want 2", ordstr(second.Ordinal))
	}

	// A different stem in the SAME bucket is its own allocation space.
	class, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("class"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create classroom: %v", err)
	}
	if class.Name != "classroom" {
		t.Fatalf("a different stem in the same bucket = %q, want classroom", class.Name)
	}

	// A different LOCATION is a different bucket: the ordinal restarts, so the
	// second room's only boardroom is bare again.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "namegen-room-b", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create second location: %v", err)
	}
	elsewhere, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: strp("namegen-room-b")}, all)
	if err != nil {
		t.Fatalf("create in the second room: %v", err)
	}
	if elsewhere.Name != "boardroom" {
		t.Fatalf("the second room's only boardroom = %q, want boardroom (a placement bucket counts alone)", elsewhere.Name)
	}

	// And the parent bucket is its own too, which is what makes system need
	// THREE buckets where a location needs two.
	nested, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), ParentName: &first.ID}, all)
	if err != nil {
		t.Fatalf("create under a parent system: %v", err)
	}
	if nested.Name != "boardroom" {
		t.Fatalf("the only boardroom under a parent = %q, want boardroom", nested.Name)
	}
}

// TestSystemSuppressedFirstOrdinal is D3 (ADR-0101) end to end, in the epic's
// own literal values: with a stem of "br", the only one is "br" and the second
// is "br-2". "br-1" is a name this rule never produces.
func TestSystemSuppressedFirstOrdinal(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)
	if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "br-type", DisplayName: "Br", Stem: strp("br"),
	}); err != nil {
		t.Fatalf("create system_type with stem br: %v", err)
	}

	names := make([]string, 0, 3)
	for range 3 {
		s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("br-type"), LocationName: &room}, all)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		names = append(names, s.Name)
	}
	want := []string{"br", "br-2", "br-3"}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("system %d generated %q, want %q", i+1, names[i], w)
		}
	}
}

// TestSystemGeneratorYieldsToAHandTypedName is the acceptance case that would
// otherwise be a 23505: an operator has already typed "br" in this room, so the
// generator's first candidate is taken and the next generated system is "br-2".
// The hand-typed row holds the NAME and owns no ordinal at all, which is
// exactly the row an ordinal-reading allocator cannot see (ADR-0097).
func TestSystemGeneratorYieldsToAHandTypedName(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)
	if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "br-type", DisplayName: "Br", Stem: strp("br"),
	}); err != nil {
		t.Fatalf("create system_type: %v", err)
	}

	typed, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "br", SystemTypeID: strp("br-type"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create hand-typed br: %v", err)
	}
	if typed.NameGenerated || typed.Ordinal != nil {
		t.Fatalf("a hand-typed system = (generated %v, ordinal %s), want (false, absent)", typed.NameGenerated, ordstr(typed.Ordinal))
	}

	generated, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("br-type"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("generate beside a hand-typed br: %v", err)
	}
	if generated.Name != "br-2" {
		t.Fatalf("generated beside a hand-typed br = %q, want br-2", generated.Name)
	}
}

// TestSystemDeleteFreesTheBareName pins the order dependence D3 accepts rather
// than leaving it as prose: allocation is lowest-free, so deleting the only
// "br" while "br-2" survives lets the next create take "br" again, and "br-1"
// never exists at any point.
func TestSystemDeleteFreesTheBareName(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)
	if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "br-type", DisplayName: "Br", Stem: strp("br"),
	}); err != nil {
		t.Fatalf("create system_type: %v", err)
	}
	first, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("br-type"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("br-type"), LocationName: &room}, all); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := gw.DeleteSystem(ctx, "", first.ID, all, all); err != nil {
		t.Fatalf("delete the bare one: %v", err)
	}

	again, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("br-type"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create after the delete: %v", err)
	}
	if again.Name != "br" {
		t.Fatalf("after deleting the bare one, the next create = %q, want br (allocation is lowest-free, and this is the order dependence ADR-0101 accepts)", again.Name)
	}

	systems, err := gw.ListSystems(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, s := range systems {
		if s.Name == "br-1" {
			t.Error("a system is named br-1: a suppressing mint never produces the ordinal-1 form")
		}
	}
}

// TestSystemRenameFreezesAndResetReturnsThePen proves both halves of the pen on
// this tier: :rename clears name_generated for good (a later move leaves the
// operator's name alone) and nulls the ordinal with it, and :resetName is the
// only way back.
func TestSystemRenameFreezesAndResetReturnsThePen(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)

	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	renamed, err := gw.RenameSystem(ctx, "", s.ID, "exec-suite", all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.NameGenerated {
		t.Error("renamed.NameGenerated = true, want false: typing a name claims the pen")
	}
	if renamed.Ordinal != nil {
		t.Errorf("renamed.Ordinal = %s, want absent: the platform owns no number for a name it did not pick", ordstr(renamed.Ordinal))
	}

	// A move must not disturb an operator-owned name, which is the whole point
	// of the pen being permanent.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "namegen-room-c", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create the second location: %v", err)
	}
	moved, err := gw.MoveSystem(ctx, "", renamed.ID, storage.SystemMove{LocationName: strp("namegen-room-c")}, all, all)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.Name != "exec-suite" || moved.NameGenerated {
		t.Fatalf("after a move an operator-named system = (%q, generated %v), want (exec-suite, false)", moved.Name, moved.NameGenerated)
	}

	reset, err := gw.ResetSystemName(ctx, "", moved.ID, all, all)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.Name != "boardroom" || !reset.NameGenerated {
		t.Fatalf("reset = (%q, generated %v), want (boardroom, true)", reset.Name, reset.NameGenerated)
	}
	if reset.Ordinal == nil || *reset.Ordinal != 1 {
		t.Fatalf("reset.Ordinal = %s, want 1", ordstr(reset.Ordinal))
	}
}

// TestSystemMoveReMintsTheOrdinal proves a platform-owned name follows its
// placement: moved into a room that already holds a boardroom, the bare name is
// taken, so the row becomes boardroom-2 and records the 2.
func TestSystemMoveReMintsTheOrdinal(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "namegen-room-d", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create the destination: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: strp("namegen-room-d")}, all); err != nil {
		t.Fatalf("occupy the destination: %v", err)
	}
	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.Name != "boardroom" {
		t.Fatalf("pre-move name = %q, want boardroom", s.Name)
	}

	moved, err := gw.MoveSystem(ctx, "", s.ID, storage.SystemMove{LocationName: strp("namegen-room-d")}, all, all)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.Name != "boardroom-2" {
		t.Fatalf("after moving into an occupied bucket = %q, want boardroom-2", moved.Name)
	}
	if moved.Ordinal == nil || *moved.Ordinal != 2 {
		t.Fatalf("moved.Ordinal = %s, want 2: the old bucket's number is no more true of the row than the old name was", ordstr(moved.Ordinal))
	}
}

// TestSystemReclassifyReMintsTheStem proves a reclassify recomputes a
// still-platform-owned name from the NEW type's stem, the system-tier twin of a
// component's product reclassify.
func TestSystemReclassifyReMintsTheStem(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)

	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	after, err := gw.UpdateSystem(ctx, "", s.ID, storage.SystemPatch{SystemTypeID: strp("class")}, all, all)
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if after.Name != "classroom" {
		t.Fatalf("after a reclassify = %q, want classroom", after.Name)
	}
	if after.Ordinal == nil || *after.Ordinal != 1 {
		t.Fatalf("after.Ordinal = %s, want 1", ordstr(after.Ordinal))
	}

	// An operator-typed name is not touched by the same act.
	typed, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "the-atrium", SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create typed: %v", err)
	}
	still, err := gw.UpdateSystem(ctx, "", typed.ID, storage.SystemPatch{SystemTypeID: strp("class")}, all, all)
	if err != nil {
		t.Fatalf("reclassify the typed one: %v", err)
	}
	if still.Name != "the-atrium" {
		t.Fatalf("a reclassify renamed an operator-typed system to %q", still.Name)
	}
}

// TestSystemUpdateWithoutAClassificationChangeKeepsTheName is the case the
// console actually drives: it sends system_type_id on EVERY save (the key is
// always keyed so an unclassify can clear), so a re-mint gated on the field
// being present rather than on the classification changing would rename a
// system whenever an operator edited its label.
//
// The estate here is the one that makes it visible rather than silent: a lower
// ordinal has been freed by a rename, so a spurious re-mint does not recompute
// to the same answer, it MOVES the name to "boardroom" under system:update,
// with no rename requested and possibly no system:rename grant held.
func TestSystemUpdateWithoutAClassificationChangeKeepsTheName(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)

	first, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create the first: %v", err)
	}
	second, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create the second: %v", err)
	}
	if second.Name != "boardroom-2" {
		t.Fatalf("the second create = %q, want boardroom-2", second.Name)
	}
	// Frees "boardroom" in this bucket.
	if _, err := gw.RenameSystem(ctx, "", first.ID, "main-room", all, all); err != nil {
		t.Fatalf("rename the first: %v", err)
	}

	after, err := gw.UpdateSystem(ctx, "", second.ID, storage.SystemPatch{
		DisplayName:  strp("The Small Boardroom"),
		SystemTypeID: strp("board"), // unchanged, and always sent by the console
	}, all, all)
	if err != nil {
		t.Fatalf("update the label: %v", err)
	}
	if after.Name != "boardroom-2" {
		t.Fatalf("a label edit renamed the system to %q: a patch that re-states the classification changes neither input to the mint", after.Name)
	}
	if after.Ordinal == nil || *after.Ordinal != 2 {
		t.Fatalf("after.Ordinal = %s, want 2 (unchanged)", ordstr(after.Ordinal))
	}
	if after.DisplayName != "The Small Boardroom" {
		t.Fatalf("after.DisplayName = %q, want the label the patch set", after.DisplayName)
	}
	// A REAL reclassify still re-mints, so the guard narrows the trigger without
	// disabling it.
	reclassified, err := gw.UpdateSystem(ctx, "", second.ID, storage.SystemPatch{SystemTypeID: strp("class")}, all, all)
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if reclassified.Name != "classroom" {
		t.Fatalf("a real reclassify = %q, want classroom", reclassified.Name)
	}
}

// TestSystemWithNoTypeCannotGenerate proves the refusal is typed and names the
// reason, on both the paths that can reach it: a nameless create with no
// system_type, and un-classifying a system whose name the platform owns (which
// would leave a platform-owned name with no source).
func TestSystemWithNoTypeCannotGenerate(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)

	_, err := gw.CreateSystem(ctx, "", storage.SystemSpec{LocationName: &room}, all)
	if !errors.Is(err, storage.ErrSystemTypeRequiredForName) {
		t.Fatalf("a nameless create with no system_type = %v, want ErrSystemTypeRequiredForName", err)
	}

	s, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = gw.UpdateSystem(ctx, "", s.ID, storage.SystemPatch{SystemTypeID: strp("")}, all, all)
	if !errors.Is(err, storage.ErrSystemTypeRequiredForName) {
		t.Fatalf("un-classifying a platform-named system = %v, want ErrSystemTypeRequiredForName", err)
	}

	// The way out is to claim the name first: after a :rename the pen is the
	// operator's, so the same unclassify goes through untouched.
	renamed, err := gw.RenameSystem(ctx, "", s.ID, "the-boardroom", all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	unclassified, err := gw.UpdateSystem(ctx, "", renamed.ID, storage.SystemPatch{SystemTypeID: strp("")}, all, all)
	if err != nil {
		t.Fatalf("un-classify after a rename: %v", err)
	}
	if unclassified.Name != "the-boardroom" || unclassified.SystemTypeID != nil {
		t.Fatalf("un-classified = (%q, type %v), want (the-boardroom, none)", unclassified.Name, unclassified.SystemTypeID)
	}
}

// TestSystemGeneratedNamesAreUniquePerBucket proves the uniqueness that
// actually matters, per bucket rather than per estate: the same generated name
// lands in all three of a system's placement buckets at once (under a parent,
// at a location, unplaced) and each is a distinct row.
func TestSystemGeneratedNamesAreUniquePerBucket(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)

	orphan, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board")}, all)
	if err != nil {
		t.Fatalf("create unplaced: %v", err)
	}
	placed, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all)
	if err != nil {
		t.Fatalf("create placed: %v", err)
	}
	child, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), ParentName: &orphan.ID}, all)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	for _, s := range []*storage.System{orphan, placed, child} {
		if s.Name != "boardroom" {
			t.Errorf("bucket-local name = %q, want boardroom in every bucket", s.Name)
		}
	}
	if orphan.ID == placed.ID || placed.ID == child.ID || orphan.ID == child.ID {
		t.Fatal("three buckets returned fewer than three rows")
	}

	// A second in each bucket takes that bucket's ordinal 2, proving the
	// buckets count separately rather than sharing one counter.
	for _, spec := range []storage.SystemSpec{
		{SystemTypeID: strp("board")},
		{SystemTypeID: strp("board"), LocationName: &room},
		{SystemTypeID: strp("board"), ParentName: &orphan.ID},
	} {
		s, err := gw.CreateSystem(ctx, "", spec, all)
		if err != nil {
			t.Fatalf("create the second in a bucket: %v", err)
		}
		if s.Name != "boardroom-2" {
			t.Errorf("the second in a bucket = %q, want boardroom-2", s.Name)
		}
	}
}

// TestStoredSystemOrdinalsRecomputeToThemselves is the recompute-and-compare
// invariant on the system tier: for every system the platform named, re-running
// allocation against the live estate (excluding the row's own name from the
// bucket, as every recompute site does) must return the number and the name the
// row already carries. A stored ordinal that has gone stale, or a name that no
// longer matches the number recorded beside it, fails here rather than
// surviving unnoticed.
//
// Built without deletes, deliberately: allocation reuses the lowest free
// ordinal, so a delete genuinely frees a number and a recompute afterwards is
// entitled to differ. The invariant is that nothing DRIFTS, not that ordinals
// are immutable.
func TestStoredSystemOrdinalsRecomputeToThemselves(t *testing.T) {
	gw, ctx, room := newSystemNamegenDB(t)
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "invariant-room-b", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create the second room: %v", err)
	}

	// Every bucket, several stems, and both pens.
	parent, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board")}, all)
	if err != nil {
		t.Fatalf("create the parent: %v", err)
	}
	for range 2 {
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: &room}, all); err != nil {
			t.Fatalf("create a placed boardroom: %v", err)
		}
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("class"), LocationName: &room}, all); err != nil {
		t.Fatalf("create a classroom: %v", err)
	}
	// One already in the destination room, so the moved row below must land on
	// ordinal 2: without it the moved row's old ordinal would still equal what
	// a recompute produces and a move that forgot to re-record would pass.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), LocationName: strp("invariant-room-b")}, all); err != nil {
		t.Fatalf("occupy the destination: %v", err)
	}
	child, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("board"), ParentName: &parent.ID}, all)
	if err != nil {
		t.Fatalf("create the child: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "hand-typed-room", SystemTypeID: strp("board"), LocationName: &room}, all); err != nil {
		t.Fatalf("create hand-typed: %v", err)
	}
	frozen, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("class"), LocationName: strp("invariant-room-b")}, all)
	if err != nil {
		t.Fatalf("create the to-be-frozen row: %v", err)
	}
	if _, err := gw.RenameSystem(ctx, "", frozen.ID, "frozen-room", all, all); err != nil {
		t.Fatalf("rename and leave frozen: %v", err)
	}
	reset, err := gw.CreateSystem(ctx, "", storage.SystemSpec{SystemTypeID: strp("class"), LocationName: strp("invariant-room-b")}, all)
	if err != nil {
		t.Fatalf("create the to-be-reset row: %v", err)
	}
	if _, err := gw.RenameSystem(ctx, "", reset.ID, "operator-room", all, all); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := gw.ResetSystemName(ctx, "", reset.ID, all, all); err != nil {
		t.Fatalf("reset: %v", err)
	}
	// Out of the parent bucket and into a location's, a real bucket change: a
	// parent wins over a location, so clearing it is what makes this a move
	// BETWEEN buckets rather than within one.
	if _, err := gw.MoveSystem(ctx, "", child.ID, storage.SystemMove{ParentName: strp(""), LocationName: strp("invariant-room-b")}, all, all); err != nil {
		t.Fatalf("move: %v", err)
	}

	systems, err := gw.ListSystems(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(systems) == 0 {
		t.Fatal("the estate under test is empty, so this invariant proves nothing")
	}
	checked := 0
	for _, s := range systems {
		if s.Ordinal == nil {
			if s.NameGenerated {
				t.Errorf("system %q is platform-named but records no ordinal", s.Name)
			}
			continue
		}
		if !s.NameGenerated {
			t.Errorf("system %q is operator-named but still records ordinal %d", s.Name, *s.Ordinal)
			continue
		}
		name, ordinal, err := gw.ExportGenerateSystemName(ctx, s.SystemTypeID, s.ParentID, s.LocationID, &s.ID)
		if err != nil {
			t.Fatalf("recompute the name of %q: %v", s.Name, err)
		}
		if name != s.Name || ordinal != *s.Ordinal {
			t.Errorf("system %q (ordinal %d) recomputes to (%q, %d): the stored value has drifted from what allocation produces", s.Name, *s.Ordinal, name, ordinal)
		}
		checked++
	}
	// The parent, two placed boardrooms, the classroom, the boardroom that
	// occupied the destination, the moved child, and the row that was renamed
	// and then reset. The two operator-named rows (one hand-typed, one renamed
	// and left frozen) carry no ordinal and are not among them, which is the
	// point of counting rather than trusting the loop ran.
	if checked != 7 {
		t.Fatalf("%d platform-named systems were checked, want 7: the estate did not build as intended", checked)
	}
}
