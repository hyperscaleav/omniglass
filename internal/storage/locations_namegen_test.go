package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationPenIsOperatorOwnedAndFrozenByRename proves the half of #686 that
// lands on locations: the pen exists on this tier and every location holds it,
// because a location has no generator yet. A rename writes false over false,
// which is the point rather than a no-op: the row an operator names TODAY is
// already frozen when #687 gives its type a name rule, so a generator arriving
// later can never re-mint a name someone typed before it existed.
func TestLocationPenIsOperatorOwnedAndFrozenByRename(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "pen-campus", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if l.NameGenerated {
		t.Error("a created location reports NameGenerated = true, want false: nothing generates a location name yet")
	}
	renamed, err := gw.RenameLocation(ctx, "", l.ID, "pen-campus-west", all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.NameGenerated {
		t.Error("a renamed location reports NameGenerated = true, want false: typing a name claims the pen for good")
	}
}

// TestResetLocationNameRefuses pins what the verb does on this tier today: it
// exists, it is gated exactly as :rename is, and it refuses with the reason
// (ErrLocationTypeNoNameRule) rather than reporting a reset that did not
// happen. A location_type carries no stem, so there is nothing to regenerate
// from until #687's per-type name rule, and this is the test that flips when it
// lands.
func TestResetLocationNameRefuses(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	l, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "reset-campus", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := gw.ResetLocationName(ctx, "", l.ID, all, all); !errors.Is(err, storage.ErrLocationTypeNoNameRule) {
		t.Fatalf("ResetLocationName = %v, want ErrLocationTypeNoNameRule", err)
	}
	// The name is untouched by the refusal: a refusal is not a partial write.
	after, err := gw.GetLocation(ctx, l.ID, all)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if after.Name != "reset-campus" {
		t.Fatalf("name after a refused reset = %q, want reset-campus", after.Name)
	}

	// An unknown location is the ordinary non-disclosing 404, resolved before
	// the refusal, so the verb never reports a type's shortcoming about a row
	// the caller cannot even see.
	if _, err := gw.ResetLocationName(ctx, "", "no-such-location", all, all); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("ResetLocationName on an unknown location = %v, want ErrLocationNotFound", err)
	}
}

// #687 gives a location_type a name rule, so the tests below are the ones the
// two above were written to hand over to. They assert against the ORDINAL
// COLUMN as well as the name wherever both exist, for the reason #681's own
// cases do: a positional location's name IS its ordinal, so reading the number
// out of the string would be asserting the same fact twice.

// positionalType is the test's OWN positional location type: no stem, so the
// name is the ordinal alone.
//
// It is created by each test that needs it rather than taken from the seed,
// because no shipped location type carries a name rule any more. `floor` did,
// and ADR-0103 was reversed for it: a floor's designation is B2, LG, G, M, 12A,
// so modelling it as an ordinal is a category error rather than an imprecision.
// The generator itself is unchanged and still has to be covered, so the cases
// below reach it through a type they mint. A feature losing its tests because
// its last shipped user went away is the failure this guards against.
const positionalType = "deck"

// mustPositionalType installs positionalType, the stand-in for the shipped rule
// these cases used to borrow. A parking deck is a real positional place: the
// number IS the address rather than a designation read off a wall.
func mustPositionalType(t *testing.T, gw storage.Gateway) {
	t.Helper()
	if _, err := gw.CreateLocationType(context.Background(), "", storage.LocationType{
		Name: positionalType, DisplayName: "Deck", AllowedParentTypes: []string{"building", "campus"},
		NameRule: &storage.NameRule{},
	}); err != nil {
		t.Fatalf("create the positional location type: %v", err)
	}
}

// TestLocationTypeWithNoNameRuleGeneratesNothing is the first acceptance
// behavior, and now the ONLY case a shipped estate can reach: all four shipped
// place types carry no rule, so a nameless create is refused rather than guessed
// at. A campus is called what the estate calls it.
func TestLocationTypeWithNoNameRuleGeneratesNothing(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "campus"}, all)
	if !errors.Is(err, storage.ErrLocationTypeNoNameRule) {
		t.Fatalf("a nameless create under a rule-less type = %v, want ErrLocationTypeNoNameRule", err)
	}
	// And nothing was written by the refusal.
	locs, err := gw.ListLocations(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(locs) != 0 {
		t.Fatalf("%d locations exist after a refused create, want 0", len(locs))
	}
	// The same type still takes a name an operator types, unchanged.
	named, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create with a name: %v", err)
	}
	if named.NameGenerated || named.Ordinal != nil {
		t.Errorf("an operator-named location = (generated %v, ordinal %v), want (false, absent)", named.NameGenerated, ordstr(named.Ordinal))
	}
}

// The refusal has to name the WAY OUT, not only the missing fact, because the
// place an operator meets it most is not a create: reclassifying a
// platform-named location into a type with no rule (the routine fix for a
// misclassification, and every SHIPPED type is such a type now) hits it, and
// there the fix is neither "supply a name" nor "give the type a rule", it is
// ":rename the location first, then reclassify it". The system tier's twin
// already names its escape (ErrSystemTypeRequiredForName); this one said only
// "the platform cannot generate a name for it", which is the diagnosis with the
// remedy left off.
func TestTheNoNameRuleRefusalNamesTheEscape(t *testing.T) {
	for _, part := range []string{":rename", "reclassif"} {
		if !strings.Contains(storage.ErrLocationTypeNoNameRule.Error(), part) {
			t.Errorf("ErrLocationTypeNoNameRule = %q, want it to mention %q: an operator reclassifying a platform-named location has to be told to claim the name first",
				storage.ErrLocationTypeNoNameRule.Error(), part)
		}
	}
}

// TestPositionalLocationTypeAllocatesWithinItsParent is the second and third
// acceptance behaviors together: a positional type allocates the next free
// ordinal, the bare-ordinal name that produces is legal, and the bucket is the
// PARENT rather than the estate, so two buildings each have their own deck 1.
func TestPositionalLocationTypeAllocatesWithinItsParent(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustPositionalType(t, gw)
	campus := mustPlace(t, gw, storage.LocationSpec{Name: "boi", LocationType: "campus"})
	a := mustPlace(t, gw, storage.LocationSpec{Name: "17c", LocationType: "building", ParentName: &campus.Name})
	b := mustPlace(t, gw, storage.LocationSpec{Name: "17d", LocationType: "building", ParentName: &campus.Name})

	for i, want := range []string{"1", "2", "3"} {
		got, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &a.Name}, all)
		if err != nil {
			t.Fatalf("create deck %d: %v", i+1, err)
		}
		if got.Name != want {
			t.Fatalf("deck %d = %q, want %q", i+1, got.Name, want)
		}
		if got.Ordinal == nil || *got.Ordinal != i+1 {
			t.Fatalf("deck %d ordinal = %v, want %d", i+1, ordstr(got.Ordinal), i+1)
		}
		if !got.NameGenerated {
			t.Fatalf("deck %d reports NameGenerated = false, want true", i+1)
		}
	}
	// The next building starts at 1 again: the bucket is the parent, which is
	// the whole reason a bare ordinal is a usable name at all.
	other, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &b.Name}, all)
	if err != nil {
		t.Fatalf("create a deck in the second building: %v", err)
	}
	if other.Name != "1" || other.Ordinal == nil || *other.Ordinal != 1 {
		t.Fatalf("the first deck of a second building = (%q, %v), want (\"1\", 1)", other.Name, ordstr(other.Ordinal))
	}
	// A hand-typed name in the mint's own shape holds its slot, exactly as it
	// does on the other two tiers: the unique index is on the NAME, so an
	// allocator reading only the ordinals it had recorded would remint it.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "4", LocationType: positionalType, ParentName: &a.Name}, all); err != nil {
		t.Fatalf("create a hand-typed deck named 4: %v", err)
	}
	fifth, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &a.Name}, all)
	if err != nil {
		t.Fatalf("create beside the hand-typed 4: %v", err)
	}
	if fifth.Name != "5" {
		t.Fatalf("the deck after a hand-typed 4 = %q, want 5", fifth.Name)
	}
}

// TestPositionalTypeAtRootAllocatesAcrossTheEstate is the ruling this slice
// owes: a positional type PERMITTED AT ROOT allocates 1, 2 across the whole
// estate, and that is legal.
//
// It is legal because the root bucket is not a special case, it is just a large
// one. Two positional types under one parent already share an ordinal space
// (the bucket is the placement, never the placement and the type), so refusing
// at root would refuse the estate-sized instance of a rule that already holds
// everywhere else, and the size of a bucket is not a property the platform can
// or should police: a building with two hundred floors is the same shape. The
// operator asked for this by making one type both positional and root-placeable.
// The second half of the test is what makes the argument rather than asserting
// it: the shared space at root is shown to be the same shared space under a
// parent.
func TestPositionalTypeAtRootAllocatesAcrossTheEstate(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{
		Name: "pad", DisplayName: "Pad", AllowedParentTypes: []string{storage.RootPlacement, "campus"},
		NameRule: &storage.NameRule{},
	}); err != nil {
		t.Fatalf("create the root-placeable positional type: %v", err)
	}
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{
		Name: "berth", DisplayName: "Berth", AllowedParentTypes: []string{storage.RootPlacement, "campus"},
		NameRule: &storage.NameRule{},
	}); err != nil {
		t.Fatalf("create the second positional type: %v", err)
	}

	first, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "pad"}, all)
	if err != nil {
		t.Fatalf("create the first root pad: %v", err)
	}
	if first.Name != "1" || first.ParentID != nil {
		t.Fatalf("the first root pad = (%q, parent %v), want (\"1\", root)", first.Name, first.ParentID)
	}
	second, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "pad"}, all)
	if err != nil {
		t.Fatalf("create the second root pad: %v", err)
	}
	if second.Name != "2" {
		t.Fatalf("the second root pad = %q, want 2", second.Name)
	}
	// A DIFFERENT positional type at root shares the space, because the bucket
	// is the placement. This is the fact that would look alarming at root and is
	// proven below to be the ordinary rule.
	berth, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "berth"}, all)
	if err != nil {
		t.Fatalf("create a root berth: %v", err)
	}
	if berth.Name != "3" {
		t.Fatalf("a berth created beside two pads at root = %q, want 3: positional types share the placement's ordinal space", berth.Name)
	}

	// The same two types under a parent behave identically, which is the proof
	// that root is not a special case to be refused.
	campus := mustPlace(t, gw, storage.LocationSpec{Name: "boi", LocationType: "campus"})
	nested, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "pad", ParentName: &campus.Name}, all)
	if err != nil {
		t.Fatalf("create a nested pad: %v", err)
	}
	if nested.Name != "1" {
		t.Fatalf("the first pad under a campus = %q, want 1", nested.Name)
	}
	nestedBerth, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "berth", ParentName: &campus.Name}, all)
	if err != nil {
		t.Fatalf("create a nested berth: %v", err)
	}
	if nestedBerth.Name != "2" {
		t.Fatalf("a berth beside a pad under one campus = %q, want 2: the shared space at root is the shared space here", nestedBerth.Name)
	}
}

// TestResetLocationNameMintsOnceTheTypeCarriesARule is the seam #686 left,
// closed: the verb that could only ever refuse now returns the pen for a type
// that can generate, and still refuses for one that cannot.
func TestResetLocationNameMintsOnceTheTypeCarriesARule(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustPositionalType(t, gw)
	campus := mustPlace(t, gw, storage.LocationSpec{Name: "boi", LocationType: "campus"})
	building := mustPlace(t, gw, storage.LocationSpec{Name: "17c", LocationType: "building", ParentName: &campus.Name})
	deck := mustPlace(t, gw, storage.LocationSpec{Name: "ground", LocationType: positionalType, ParentName: &building.Name})
	if deck.NameGenerated {
		t.Fatal("precondition: an operator-typed deck reports NameGenerated = true")
	}

	reset, err := gw.ResetLocationName(ctx, "", deck.ID, all, all)
	if err != nil {
		t.Fatalf("reset a deck's name: %v", err)
	}
	if reset.Name != "1" || !reset.NameGenerated {
		t.Fatalf("reset = (%q, generated %v), want (\"1\", true)", reset.Name, reset.NameGenerated)
	}
	if reset.Ordinal == nil || *reset.Ordinal != 1 {
		t.Fatalf("ordinal after reset = %v, want 1", ordstr(reset.Ordinal))
	}
	// A rename takes the pen back AND the number with it, then a second reset
	// re-mints: the same round trip the component and system tiers assert.
	renamed, err := gw.RenameLocation(ctx, "", deck.ID, "ground", all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.NameGenerated || renamed.Ordinal != nil {
		t.Fatalf("after rename = (generated %v, ordinal %v), want (false, absent)", renamed.NameGenerated, ordstr(renamed.Ordinal))
	}

	// The refusal survives for a type with no rule, which is the same behavior
	// this verb had for every type one slice ago.
	if _, err := gw.ResetLocationName(ctx, "", building.ID, all, all); !errors.Is(err, storage.ErrLocationTypeNoNameRule) {
		t.Fatalf("reset a building's name = %v, want ErrLocationTypeNoNameRule", err)
	}
}

// TestLocationMoveReMintsInTheDestinationBucket proves the placement half of
// the derivation: the bucket is an input to the mint, so a platform-named
// location re-mints when it moves, and records the new number with the new name
// rather than carrying the old bucket's over.
func TestLocationMoveReMintsInTheDestinationBucket(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustPositionalType(t, gw)
	campus := mustPlace(t, gw, storage.LocationSpec{Name: "boi", LocationType: "campus"})
	a := mustPlace(t, gw, storage.LocationSpec{Name: "17c", LocationType: "building", ParentName: &campus.Name})
	b := mustPlace(t, gw, storage.LocationSpec{Name: "17d", LocationType: "building", ParentName: &campus.Name})
	// One deck already sitting in the destination, so a move that failed to
	// re-mint could not pass by landing on the same ordinal it already held.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &b.Name}, all); err != nil {
		t.Fatalf("create the sitting deck: %v", err)
	}
	moving, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &a.Name}, all)
	if err != nil {
		t.Fatalf("create the moving deck: %v", err)
	}
	if moving.Name != "1" {
		t.Fatalf("precondition: the moving deck = %q, want 1", moving.Name)
	}

	moved, err := gw.MoveLocation(ctx, "", moving.ID, storage.LocationMove{ParentName: &b.Name}, all, all)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if moved.Name != "2" {
		t.Fatalf("name after the move = %q, want 2 (the destination's ordinal 1 is taken)", moved.Name)
	}
	if moved.Ordinal == nil || *moved.Ordinal != 2 {
		t.Fatalf("ordinal after the move = %v, want 2 (re-recorded with the new name, not carried over)", ordstr(moved.Ordinal))
	}

	// An operator-named location is left exactly where the operator put it.
	typed := mustPlace(t, gw, storage.LocationSpec{Name: "mezzanine", LocationType: positionalType, ParentName: &a.Name})
	movedTyped, err := gw.MoveLocation(ctx, "", typed.ID, storage.LocationMove{ParentName: &b.Name}, all, all)
	if err != nil {
		t.Fatalf("move the operator-named deck: %v", err)
	}
	if movedTyped.Name != "mezzanine" {
		t.Fatalf("an operator-named deck is called %q after a move, want mezzanine", movedTyped.Name)
	}
}

// TestLocationUpdateWithoutAClassificationChangeKeepsTheName is the defect
// review caught on the system tier one slice ago (ADR-0101), refused entry
// here: the console sends location_type on EVERY save, so a guard on the field
// being present rather than on the classification changing would re-mint a
// display-name edit, and with a lower ordinal freed by an earlier rename that
// re-mint MOVES the name under location:update.
func TestLocationUpdateWithoutAClassificationChangeKeepsTheName(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustPositionalType(t, gw)
	campus := mustPlace(t, gw, storage.LocationSpec{Name: "boi", LocationType: "campus"})
	building := mustPlace(t, gw, storage.LocationSpec{Name: "17c", LocationType: "building", ParentName: &campus.Name})

	first, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &building.Name}, all)
	if err != nil {
		t.Fatalf("create the first deck: %v", err)
	}
	second, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &building.Name}, all)
	if err != nil {
		t.Fatalf("create the second deck: %v", err)
	}
	if second.Name != "2" {
		t.Fatalf("precondition: the second deck = %q, want 2", second.Name)
	}
	// Free ordinal 1, so a spurious re-mint has somewhere lower to move to and
	// the defect is observable rather than merely possible.
	if _, err := gw.RenameLocation(ctx, "", first.ID, "ground", all, all); err != nil {
		t.Fatalf("rename the first deck: %v", err)
	}

	// Exactly what web/src/pages/Locations.tsx sends on every save: the label,
	// and the type it already has.
	same := positionalType
	after, err := gw.UpdateLocation(ctx, "", second.ID, storage.LocationPatch{
		DisplayName: strptr("Level Two"), LocationType: &same,
	}, all, all)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if after.Name != "2" {
		t.Fatalf("a label edit renamed the location to %q: the re-mint guard reads the field's presence rather than the classification changing", after.Name)
	}
	if after.Ordinal == nil || *after.Ordinal != 2 {
		t.Fatalf("ordinal after a label edit = %v, want 2", ordstr(after.Ordinal))
	}

	// The same patch written in the OTHER form a registry reference takes
	// (ADR-0062): the type's uuid rather than its name. It addresses the row the
	// location already carries, so it is still not a reclassify, and the
	// comparison that says so has to be on the RESOLVED id rather than on the
	// string the caller happened to type.
	byUUID := after.LocationTypeID
	again, err := gw.UpdateLocation(ctx, "", second.ID, storage.LocationPatch{
		DisplayName: strptr("Level Two"), LocationType: &byUUID,
	}, all, all)
	if err != nil {
		t.Fatalf("update by type uuid: %v", err)
	}
	if again.Name != "2" {
		t.Fatalf("restating the type by uuid renamed the location to %q: the guard compares the ref rather than the resolved row", again.Name)
	}

	// A REAL reclassify does re-mint, into the new type's rule. "room" carries
	// no rule at all, so it refuses rather than silently freezing the pen.
	room := "room"
	if _, err := gw.UpdateLocation(ctx, "", second.ID, storage.LocationPatch{LocationType: &room}, all, all); !errors.Is(err, storage.ErrLocationTypeNoNameRule) {
		t.Fatalf("reclassifying a platform-named deck as a rule-less room = %v, want ErrLocationTypeNoNameRule", err)
	}
	// And into a type that CAN generate, it takes that type's shape.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{
		Name: "wing", DisplayName: "Wing", AllowedParentTypes: []string{"building"},
		NameRule: &storage.NameRule{Stem: "wing", BareFirst: true},
	}); err != nil {
		t.Fatalf("create the wing type: %v", err)
	}
	wing := "wing"
	reclassified, err := gw.UpdateLocation(ctx, "", second.ID, storage.LocationPatch{LocationType: &wing}, all, all)
	if err != nil {
		t.Fatalf("reclassify as a wing: %v", err)
	}
	if reclassified.Name != "wing" {
		t.Fatalf("name after reclassify = %q, want wing (the new rule suppresses the first ordinal)", reclassified.Name)
	}
	if reclassified.Ordinal == nil || *reclassified.Ordinal != 1 {
		t.Fatalf("ordinal after reclassify = %v, want 1", ordstr(reclassified.Ordinal))
	}
}

// TestEditingANameRuleRenamesNothing is the fourth acceptance behavior, and it
// is asserted rather than only stated. A rule's blast radius is the ESTATE, not
// a placement, so it does not cascade, which is ADR-0100's line applied to the
// name side and ADR-0101's already applied to a system_type's stem.
//
// The asymmetry with the LABEL side is deliberate and worth being explicit
// about: a label rule change has a preview-then-apply verb (#685) precisely
// because relabelling in bulk is recoverable. Renaming is not: a name is in a
// unique index, it is the address an operator types, and it is what runbooks and
// integrations outside this system store. There is no name-side recompute verb,
// and this test is the statement of that.
func TestEditingANameRuleRenamesNothing(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustPositionalType(t, gw)
	campus := mustPlace(t, gw, storage.LocationSpec{Name: "boi", LocationType: "campus"})
	building := mustPlace(t, gw, storage.LocationSpec{Name: "17c", LocationType: "building", ParentName: &campus.Name})
	existing, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &building.Name}, all)
	if err != nil {
		t.Fatalf("create the deck: %v", err)
	}
	if existing.Name != "1" {
		t.Fatalf("precondition: the deck = %q, want 1", existing.Name)
	}

	if _, err := gw.UpdateLocationType(ctx, "", positionalType, storage.LocationTypePatch{
		NameRule: &storage.NameRule{Stem: "level"},
	}); err != nil {
		t.Fatalf("edit the deck type's name rule: %v", err)
	}

	after, err := gw.GetLocation(ctx, existing.ID, all)
	if err != nil {
		t.Fatalf("re-read the deck: %v", err)
	}
	if after.Name != "1" {
		t.Fatalf("the existing deck is called %q after the rule changed, want 1: a rule edit renames nothing", after.Name)
	}
	if after.Ordinal == nil || *after.Ordinal != 1 || !after.NameGenerated {
		t.Fatalf("the existing deck = (ordinal %v, generated %v), want (1, true): a rule edit changes no stored fact about a row", ordstr(after.Ordinal), after.NameGenerated)
	}

	// The NEXT create takes the new rule, which is what makes the assertion
	// above a statement about renaming rather than about the rule not landing.
	next, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &building.Name}, all)
	if err != nil {
		t.Fatalf("create under the new rule: %v", err)
	}
	if next.Name != "level-1" {
		t.Fatalf("the next deck = %q, want level-1: the edited rule applies to new rows", next.Name)
	}
	// So does the next :resetName, which is the one act an operator can use to
	// bring a row onto the new rule deliberately, one row at a time.
	reset, err := gw.ResetLocationName(ctx, "", after.ID, all, all)
	if err != nil {
		t.Fatalf("reset onto the new rule: %v", err)
	}
	if reset.Name != "level-2" {
		t.Fatalf("reset onto the new rule = %q, want level-2", reset.Name)
	}
}

// TestNameRuleIsRefusedAtEditTime is the acceptance behavior that a bad rule is
// caught where it is written. Both write paths refuse, and nothing is stored, so
// a later create never meets it.
func TestNameRuleIsRefusedAtEditTime(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustPositionalType(t, gw)

	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{
		Name: "wing", DisplayName: "Wing", NameRule: &storage.NameRule{Stem: "Wing"},
	}); !errors.Is(err, storage.ErrInvalidNameRule) {
		t.Fatalf("create a type with an upper-case stem = %v, want ErrInvalidNameRule", err)
	}
	types, err := gw.ListLocationTypes(ctx)
	if err != nil {
		t.Fatalf("list types: %v", err)
	}
	for _, lt := range types {
		if lt.Name == "wing" {
			t.Fatal("the refused type was stored anyway")
		}
	}

	if _, err := gw.UpdateLocationType(ctx, "", positionalType, storage.LocationTypePatch{
		NameRule: &storage.NameRule{Stem: "not a slug"},
	}); !errors.Is(err, storage.ErrInvalidNameRule) {
		t.Fatalf("patch a type with an illegal stem = %v, want ErrInvalidNameRule", err)
	}
	// The deck type's own rule is untouched by the refusal, so a create still works.
	campus := mustPlace(t, gw, storage.LocationSpec{Name: "boi", LocationType: "campus"})
	building := mustPlace(t, gw, storage.LocationSpec{Name: "17c", LocationType: "building", ParentName: &campus.Name})
	deck, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &building.Name}, all)
	if err != nil {
		t.Fatalf("create a deck after the refused patch: %v", err)
	}
	if deck.Name != "1" {
		t.Fatalf("deck = %q, want 1: the refused patch reached the stored rule", deck.Name)
	}
}

// TestNameRuleRoundTripsThroughTheRegistry pins what is stored and read back,
// including the one normalization: a positional rule carries no suppression
// flag, so a rule written with one comes back without it and two rules that
// mint identically compare equal.
//
// It also pins the shipped set, which is now the same answer for all four of
// them: NO shipped location type carries a name rule. `floor` carried one for
// two slices and ADR-0103 reversed it, so this is the assertion that fails if a
// rule is ever put back into the seed without the argument being made again.
func TestNameRuleRoundTripsThroughTheRegistry(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustPositionalType(t, gw)
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{
		Name: "wing", DisplayName: "Wing", NameRule: &storage.NameRule{Stem: "", BareFirst: true},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	types, err := gw.ListLocationTypes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byName := map[string]storage.LocationType{}
	for _, lt := range types {
		byName[lt.Name] = lt
	}
	if got := byName["wing"].NameRule; got == nil || *got != (storage.NameRule{}) {
		t.Errorf("the stored wing rule = %+v, want the zero rule: an ignored flag is normalized away, not stored", got)
	}
	// The type the TEST created is what round-trips a positional rule now.
	if got := byName[positionalType].NameRule; got == nil || got.Stem != "" {
		t.Errorf("the stored %s rule = %+v, want a positional rule (an empty stem)", positionalType, got)
	}
	for _, shipped := range []string{"campus", "building", "floor", "room"} {
		if got := byName[shipped].NameRule; got != nil {
			t.Errorf("the shipped %s rule = %+v, want absent: every shipped place is named by the operator, a floor included (ADR-0103)", shipped, got)
		}
	}
}

// TestRemovingAShippedNameRuleReachesNewEstatesOnly is the honest half of
// dropping `floor`'s rule from the seed, asserted rather than assumed.
//
// SeedLocationType is insert-when-absent (the forked-template half of the seed
// model: a location type is operator space, so a restart never reverts an edit),
// so a rule cannot be UN-shipped by deleting the line any more than it could be
// shipped to an existing estate by adding one. An estate that already seeded the
// positional `floor` keeps it, name-generating floors and all, and its floors go
// on being named `1`.
//
// The second half is the sharper cost: there is no request that removes it
// either. A patch cannot spell "clear", because an omitted key and an explicit
// null both decode to a nil pointer, so `name_rule = coalesce($6::jsonb,
// name_rule)` leaves the stored rule standing (ADR-0102 recorded that limit for
// the wire; this is what it means when a shipped default has to be withdrawn).
// Such an estate reaches the new default only by a direct write.
func TestRemovingAShippedNameRuleReachesNewEstatesOnly(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// The estate that seeded the old shipped rule, in the one shape an existing
	// estate can be in: the row carries a positional rule the seed no longer has.
	if _, err := gw.UpdateLocationType(ctx, "", "floor", storage.LocationTypePatch{
		NameRule: &storage.NameRule{},
	}); err != nil {
		t.Fatalf("install the rule an upgraded estate already has: %v", err)
	}

	// Boot again with the rule gone from the seed. Insert-when-absent means the
	// row is not touched at all.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	types, err := gw.ListLocationTypes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byName := map[string]storage.LocationType{}
	for _, lt := range types {
		byName[lt.Name] = lt
	}
	if got := byName["floor"].NameRule; got == nil || got.Stem != "" {
		t.Fatalf("floor's rule after re-seeding without one = %+v, want the estate's own positional rule intact: a boot seed that reverted an operator-owned row would be the defect", got)
	}
	// And it still generates, which is what "reaches new estates only" costs.
	campus := mustPlace(t, gw, storage.LocationSpec{Name: "boi", LocationType: "campus"})
	building := mustPlace(t, gw, storage.LocationSpec{Name: "17c", LocationType: "building", ParentName: &campus.Name})
	floor, err := gw.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "floor", ParentName: &building.Name}, all)
	if err != nil {
		t.Fatalf("create a floor on the upgraded estate: %v", err)
	}
	if floor.Name != "1" || !floor.NameGenerated {
		t.Fatalf("a floor on the upgraded estate = (%q, generated %v), want (\"1\", true)", floor.Name, floor.NameGenerated)
	}

	// A patch cannot take the rule back off: an omitted rule is "leave it".
	if _, err := gw.UpdateLocationType(ctx, "", "floor", storage.LocationTypePatch{
		DisplayName: strptr("Floor"),
	}); err != nil {
		t.Fatalf("patch the floor type without a rule: %v", err)
	}
	after, err := gw.ListLocationTypes(ctx)
	if err != nil {
		t.Fatalf("re-list: %v", err)
	}
	for _, lt := range after {
		if lt.Name == "floor" && lt.NameRule == nil {
			t.Fatal("a patch that omitted name_rule cleared it; the wire has no spelling for \"clear\", and pretending it does is worse than the limit")
		}
	}
}

// TestStoredLocationOrdinalsRecomputeToThemselves is the recompute-and-compare
// invariant on this tier, the same shape TestStoredOrdinalsRecomputeToThemselves
// holds the component and system tiers to: for every location the platform
// named, re-running allocation against the live estate (excluding the row's own
// name, as every recompute site does) must return the number and the name the
// row already carries.
//
// The estate is built without deletes, deliberately: allocation reuses the
// lowest free ordinal, so a delete genuinely frees a number and a recompute
// after one is entitled to differ. What this asserts is that nothing DRIFTS.
func TestStoredLocationOrdinalsRecomputeToThemselves(t *testing.T) {
	ctx := context.Background()
	pg, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(pg.Close)
	if err := seed.Run(ctx, pg); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustPositionalType(t, pg)
	if _, err := pg.CreateLocationType(ctx, "", storage.LocationType{
		Name: "wing", DisplayName: "Wing", AllowedParentTypes: []string{"building"},
		NameRule: &storage.NameRule{Stem: "wing", BareFirst: true},
	}); err != nil {
		t.Fatalf("create the wing type: %v", err)
	}
	campus := mustPlace(t, pg, storage.LocationSpec{Name: "boi", LocationType: "campus"})
	a := mustPlace(t, pg, storage.LocationSpec{Name: "17c", LocationType: "building", ParentName: &campus.Name})
	b := mustPlace(t, pg, storage.LocationSpec{Name: "17d", LocationType: "building", ParentName: &campus.Name})

	// Both buckets, both mints, and every pen: generated rows, an
	// operator-typed one, a renamed one, a reset one, and a moved one.
	for range 3 {
		if _, err := pg.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &a.Name}, all); err != nil {
			t.Fatalf("create a deck in 17c: %v", err)
		}
	}
	for range 2 {
		if _, err := pg.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "wing", ParentName: &a.Name}, all); err != nil {
			t.Fatalf("create a wing in 17c: %v", err)
		}
	}
	// A deck already sitting in 17d, so the deck moved there below must land
	// on ordinal 2: without it the moved row's OLD ordinal would still equal
	// what a recompute produces and a move that forgot to re-record would slip
	// through.
	if _, err := pg.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &b.Name}, all); err != nil {
		t.Fatalf("create the sitting deck in 17d: %v", err)
	}
	moving, err := pg.CreateLocation(ctx, "", storage.LocationSpec{LocationType: positionalType, ParentName: &a.Name}, all)
	if err != nil {
		t.Fatalf("create the moving deck: %v", err)
	}
	if _, err := pg.CreateLocation(ctx, "", storage.LocationSpec{Name: "penthouse", LocationType: positionalType, ParentName: &a.Name}, all); err != nil {
		t.Fatalf("create the hand-typed deck: %v", err)
	}
	// Generated and then renamed, and LEFT renamed: the row that catches a
	// rename which hands over the name without giving up the number.
	frozen, err := pg.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "wing", ParentName: &a.Name}, all)
	if err != nil {
		t.Fatalf("create the to-be-frozen wing: %v", err)
	}
	if _, err := pg.RenameLocation(ctx, "", frozen.ID, "east-wing", all, all); err != nil {
		t.Fatalf("freeze the wing: %v", err)
	}
	reset, err := pg.CreateLocation(ctx, "", storage.LocationSpec{LocationType: "wing", ParentName: &a.Name}, all)
	if err != nil {
		t.Fatalf("create the to-be-reset wing: %v", err)
	}
	if _, err := pg.RenameLocation(ctx, "", reset.ID, "west-wing", all, all); err != nil {
		t.Fatalf("rename the wing: %v", err)
	}
	if _, err := pg.ResetLocationName(ctx, "", reset.ID, all, all); err != nil {
		t.Fatalf("reset the wing: %v", err)
	}
	if _, err := pg.MoveLocation(ctx, "", moving.ID, storage.LocationMove{ParentName: &b.Name}, all, all); err != nil {
		t.Fatalf("move: %v", err)
	}

	locs, err := pg.ListLocations(ctx, all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	checked := 0
	for _, l := range locs {
		if l.Ordinal == nil {
			if l.NameGenerated {
				t.Errorf("location %q is platform-named but records no ordinal", l.Name)
			}
			continue
		}
		if !l.NameGenerated {
			t.Errorf("location %q is operator-named but still records ordinal %d", l.Name, *l.Ordinal)
			continue
		}
		name, ordinal, err := pg.ExportGenerateLocationName(ctx, l.LocationTypeID, l.ParentID, &l.ID)
		if err != nil {
			t.Fatalf("recompute the name of %q: %v", l.Name, err)
		}
		if name != l.Name || ordinal != *l.Ordinal {
			t.Errorf("location %q (ordinal %d) recomputes to (%q, %d): the stored value has drifted from what allocation produces", l.Name, *l.Ordinal, name, ordinal)
		}
		checked++
	}
	// Three decks and two wings in 17c, the deck that sits in 17d, the deck
	// that moved there, and the wing that was renamed and reset. The four
	// operator-named rows (the campus, the two buildings, the hand-typed deck)
	// and the one left frozen carry no ordinal and are not among them, which is
	// the point of counting rather than trusting the loop ran.
	if checked != 8 {
		t.Fatalf("%d platform-named locations were checked, want 8: the estate did not build as intended", checked)
	}
}

// mustLocation creates a location or fails the test, for the fixtures above
// whose creation is setup rather than the thing under test.
func mustPlace(t *testing.T, gw storage.Gateway, spec storage.LocationSpec) *storage.Location {
	t.Helper()
	l, err := gw.CreateLocation(context.Background(), "", spec, all)
	if err != nil {
		t.Fatalf("create location %q: %v", spec.Name, err)
	}
	return l
}
