package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSystemTypeRoundTrip creates a root and a child under it and checks Get
// resolves both, the child carrying its parent's id, on the same shape
// component_type's round trip pins.
func TestSystemTypeRoundTrip(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "rt-space", DisplayName: "Space", Stem: strp("space"), Icon: strp("box"), Abbrev: strp("sp"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if root.Name != "rt-space" || root.DisplayName != "Space" {
		t.Fatalf("root = %+v, want name rt-space display_name Space", root)
	}
	if root.Official {
		t.Fatalf("new system_type official=true, want false")
	}
	if root.ParentID != nil {
		t.Fatalf("root ParentID = %v, want nil", root.ParentID)
	}

	child, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "rt-huddle", DisplayName: "Huddle Room", ParentID: &root.ID,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != root.ID {
		t.Fatalf("child ParentID = %v, want %v", child.ParentID, root.ID)
	}

	gotChild, err := gw.GetSystemType(ctx, "rt-huddle")
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if gotChild.ParentID == nil || *gotChild.ParentID != root.ID {
		t.Fatalf("get child ParentID = %v, want %v", gotChild.ParentID, root.ID)
	}

	all, err := gw.ListSystemTypes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var foundRoot, foundChild bool
	for _, st := range all {
		if st.ID == root.ID {
			foundRoot = true
		}
		if st.ID == child.ID {
			foundChild = true
		}
	}
	if !foundRoot || !foundChild {
		t.Fatalf("list missing root or child; found root=%v child=%v", foundRoot, foundChild)
	}

	dn := "Huddle Space"
	upd, err := gw.UpdateSystemType(ctx, "", "rt-huddle", storage.SystemTypePatch{DisplayName: &dn})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.DisplayName != dn {
		t.Fatalf("update display_name = %q, want %q", upd.DisplayName, dn)
	}

	// A duplicate name is ErrTypeExists. Stem set (a root needs one) so the
	// write reaches the unique-name check this asserts, not the stem guard.
	if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "rt-space", DisplayName: "Dup", Stem: strp("dup")}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// An unknown ref is ErrTypeNotFound.
	if _, err := gw.GetSystemType(ctx, "rt-nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrTypeNotFound", err)
	}

	// A parent naming no row is ErrParentSystemTypeNotFound.
	stray := uuid.New()
	if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "rt-orphan", DisplayName: "Orphan", ParentID: &stray}); !errors.Is(err, storage.ErrParentSystemTypeNotFound) {
		t.Fatalf("unknown parent err = %v, want ErrParentSystemTypeNotFound", err)
	}

	// Leaf delete (no children, classifying nothing) succeeds.
	if err := gw.DeleteSystemType(ctx, "", "rt-huddle"); err != nil {
		t.Fatalf("delete child: %v", err)
	}
	if _, err := gw.GetSystemType(ctx, "rt-huddle"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTypeNotFound", err)
	}
}

// TestSystemTypeFactsInheritOverrideMidChain is the load-bearing inheritance
// assertion, and its fixture is built so it can actually fail. Three levels,
// each fact set at a DIFFERENT depth:
//
//	root  stem, icon, abbrev  all set
//	mid   icon only           an override BETWEEN the root and the leaf
//	leaf  abbrev only         an override at the bottom
//
// Resolving the leaf therefore has one right answer per fact and each one
// discriminates a different defect: stem must walk two levels to the root (a
// walk that stops at the first parent returns ""), icon must stop at the MID
// row (a walk that keeps going and lets the last value win returns the root's),
// and abbrev must be the leaf's own (a walk that ignores the starting row
// returns the root's). A fixture that overrode only at the leaf, or that gave
// every level the same value, would pass whether or not any of that works.
func TestSystemTypeFactsInheritOverrideMidChain(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "fi-root", DisplayName: "Root", Stem: strp("root-stem"), Icon: strp("root-icon"), Abbrev: strp("rt"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	mid, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "fi-mid", DisplayName: "Mid", ParentID: &root.ID,
		Icon: strp("mid-icon"), // the mid-chain override; stem and abbrev inherit
	})
	if err != nil {
		t.Fatalf("create mid: %v", err)
	}
	leaf, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "fi-leaf", DisplayName: "Leaf", ParentID: &mid.ID,
		Abbrev: strp("lf"), // the leaf override; stem and icon inherit
	})
	if err != nil {
		t.Fatalf("create leaf: %v", err)
	}

	stem, icon, abbrev, err := gw.ResolveSystemTypeFacts(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("resolve leaf facts: %v", err)
	}
	if stem != "root-stem" {
		t.Errorf("leaf stem = %q, want %q: neither the leaf nor the mid sets one, so the walk must reach the root", stem, "root-stem")
	}
	if icon != "mid-icon" {
		t.Errorf("leaf icon = %q, want %q: the MID row overrides, so the nearest ancestor that sets one wins over the root's %q", icon, "mid-icon", "root-icon")
	}
	if abbrev != "lf" {
		t.Errorf("leaf abbrev = %q, want the leaf's own %q", abbrev, "lf")
	}

	// The mid row itself: its own icon, the root's stem and abbrev. This is the
	// other side of the same override, and it fails if the walk ever prefers a
	// descendant's value or skips the starting row.
	mStem, mIcon, mAbbrev, err := gw.ResolveSystemTypeFacts(ctx, mid.ID)
	if err != nil {
		t.Fatalf("resolve mid facts: %v", err)
	}
	if mStem != "root-stem" || mIcon != "mid-icon" || mAbbrev != "rt" {
		t.Errorf("mid facts = (%q,%q,%q), want (root-stem,mid-icon,rt)", mStem, mIcon, mAbbrev)
	}

	// And the root resolves its own, no walk needed.
	rStem, rIcon, rAbbrev, err := gw.ResolveSystemTypeFacts(ctx, root.ID)
	if err != nil {
		t.Fatalf("resolve root facts: %v", err)
	}
	if rStem != "root-stem" || rIcon != "root-icon" || rAbbrev != "rt" {
		t.Errorf("root facts = (%q,%q,%q), want (root-stem,root-icon,rt)", rStem, rIcon, rAbbrev)
	}
}

// TestSeededSystemTypeInheritsMidChain runs the same override-mid-chain check
// against the SHIPPED tree rather than a fixture, because the seed is what
// #657's name and label rules will read. `board` sets its own stem and abbrev
// but no icon, and its parent `room` overrides the root `av`'s icon, so a
// boardroom's resolved icon must be room's and never av's. It fails if the seed
// stops setting an icon mid-chain, or if the walk takes the root's value.
func TestSeededSystemTypeInheritsMidChain(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	av, err := gw.GetSystemType(ctx, "av")
	if err != nil {
		t.Fatalf("get seeded av: %v", err)
	}
	room, err := gw.GetSystemType(ctx, "room")
	if err != nil {
		t.Fatalf("get seeded room: %v", err)
	}
	board, err := gw.GetSystemType(ctx, "board")
	if err != nil {
		t.Fatalf("get seeded board: %v", err)
	}

	// The fixture's own premise: the chain really is three deep, and room
	// really does override av's icon. Without these the assertion below could
	// pass on a flat or uniform tree.
	if board.ParentID == nil || *board.ParentID != room.ID {
		t.Fatalf("board's parent = %v, want room (%v)", board.ParentID, room.ID)
	}
	if room.ParentID == nil || *room.ParentID != av.ID {
		t.Fatalf("room's parent = %v, want av (%v)", room.ParentID, av.ID)
	}
	if board.Icon != nil {
		t.Fatalf("board sets its own icon (%q); this test needs it to inherit one", *board.Icon)
	}
	if room.Icon == nil || av.Icon == nil || *room.Icon == *av.Icon {
		t.Fatalf("room icon %v and av icon %v must both be set and differ, or the walk cannot be told apart", room.Icon, av.Icon)
	}

	stem, icon, abbrev, err := gw.ResolveSystemTypeFacts(ctx, board.ID)
	if err != nil {
		t.Fatalf("resolve board facts: %v", err)
	}
	if icon != *room.Icon {
		t.Errorf("board icon = %q, want room's %q (the nearest ancestor that sets one), not av's %q", icon, *room.Icon, *av.Icon)
	}
	if stem != "boardroom" {
		t.Errorf("board stem = %q, want its own %q: this is what #657's name rule mints from", stem, "boardroom")
	}
	if abbrev != "br" {
		t.Errorf("board abbrev = %q, want its own %q: this is what #657's label rule renders", abbrev, "br")
	}
}

// TestSystemTypeDeleteRestricted proves both refusals the registry owes: a type
// that still parents another type, and a type that still classifies a system.
// The #507 lesson is that a registry delete must never cascade through
// classified rows, so each refusal leaves the world exactly as it was.
func TestSystemTypeDeleteRestricted(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	parent, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "dr-room", DisplayName: "Room", Stem: strp("room")})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "dr-huddle", DisplayName: "Huddle", ParentID: &parent.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := gw.DeleteSystemType(ctx, "", "dr-room"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete a type that still parents another = %v, want ErrTypeInUse", err)
	}
	if _, err := gw.GetSystemType(ctx, "dr-room"); err != nil {
		t.Fatalf("the parent should survive the refused delete: %v", err)
	}

	// A system classified by the leaf pins it just as hard.
	childName := child.Name
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "dr-system", SystemTypeID: &childName}, all); err != nil {
		t.Fatalf("create classified system: %v", err)
	}
	if err := gw.DeleteSystemType(ctx, "", "dr-huddle"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete a type that still classifies a system = %v, want ErrTypeInUse", err)
	}
	sys, err := gw.GetSystem(ctx, "dr-system", all)
	if err != nil {
		t.Fatalf("the system should survive the refused delete: %v", err)
	}
	if sys.SystemTypeID == nil || *sys.SystemTypeID != child.ID.String() {
		t.Fatalf("system's system_type = %v, want %v: the refused delete must leave the classification alone", sys.SystemTypeID, child.ID)
	}
}

// TestSystemCarriesItsType covers the classification round trip on the system
// itself: set on create, read back with both forms, repointed by a patch, and
// cleared by the house empty-string sentinel. A patch that omits the field
// leaves it alone, which is the leg a coalesce-only implementation gets wrong.
func TestSystemCarriesItsType(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	board, err := gw.GetSystemType(ctx, "board")
	if err != nil {
		t.Fatalf("get seeded board: %v", err)
	}

	name := "board"
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sct-room", DisplayName: "Room 101", SystemTypeID: &name}, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	if sys.SystemTypeID == nil || *sys.SystemTypeID != board.ID.String() {
		t.Fatalf("created system_type_id = %v, want %v", sys.SystemTypeID, board.ID)
	}
	if sys.SystemTypeName == nil || *sys.SystemTypeName != "board" {
		t.Fatalf("created system_type name = %v, want board", sys.SystemTypeName)
	}

	// An omitted field leaves the classification alone.
	dn := "Room 101A"
	kept, err := gw.UpdateSystem(ctx, "", "sct-room", storage.SystemPatch{DisplayName: &dn}, all, all)
	if err != nil {
		t.Fatalf("patch display_name: %v", err)
	}
	if kept.SystemTypeID == nil || *kept.SystemTypeID != board.ID.String() {
		t.Fatalf("system_type_id after an unrelated patch = %v, want it unchanged (%v)", kept.SystemTypeID, board.ID)
	}

	// A named type repoints it.
	huddle, err := gw.GetSystemType(ctx, "huddle")
	if err != nil {
		t.Fatalf("get seeded huddle: %v", err)
	}
	newType := "huddle"
	moved, err := gw.UpdateSystem(ctx, "", "sct-room", storage.SystemPatch{SystemTypeID: &newType}, all, all)
	if err != nil {
		t.Fatalf("patch system_type: %v", err)
	}
	if moved.SystemTypeID == nil || *moved.SystemTypeID != huddle.ID.String() {
		t.Fatalf("system_type_id after reclassify = %v, want %v", moved.SystemTypeID, huddle.ID)
	}

	// The empty string clears it (nullable for now: an unclassified system is
	// legal until the floor slice lands).
	clear := ""
	cleared, err := gw.UpdateSystem(ctx, "", "sct-room", storage.SystemPatch{SystemTypeID: &clear}, all, all)
	if err != nil {
		t.Fatalf("clear system_type: %v", err)
	}
	if cleared.SystemTypeID != nil {
		t.Fatalf("system_type_id after clear = %v, want nil", cleared.SystemTypeID)
	}

	// An unknown type is refused on both write paths, not silently dropped.
	bogus := "no-such-system-type"
	if _, err := gw.UpdateSystem(ctx, "", "sct-room", storage.SystemPatch{SystemTypeID: &bogus}, all, all); !errors.Is(err, storage.ErrUnknownSystemType) {
		t.Fatalf("patch to an unknown type = %v, want ErrUnknownSystemType", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sct-bogus", SystemTypeID: &bogus}, all); !errors.Is(err, storage.ErrUnknownSystemType) {
		t.Fatalf("create with an unknown type = %v, want ErrUnknownSystemType", err)
	}
}

// TestSystemTypeRenameKeepsItsSystems proves the classification survives a
// rename of the registry row: system.system_type_id holds the uuid (ADR-0062),
// never a copy of the name, so the pointer cannot go stale and the projected
// handle follows the row to its new name. The rename runs as raw SQL because
// the registry has no rename leg on the gateway (component_type has none
// either); the invariant under test is the storage shape, not the verb.
func TestSystemTypeRenameKeepsItsSystems(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	st, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "rn-before", DisplayName: "Before", Stem: strp("before")})
	if err != nil {
		t.Fatalf("create type: %v", err)
	}
	ref := "rn-before"
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "rn-system", SystemTypeID: &ref}, all); err != nil {
		t.Fatalf("create classified system: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `update system_type set name = $2 where id = $1`, st.ID, "rn-after"); err != nil {
		t.Fatalf("rename system_type: %v", err)
	}

	sys, err := gw.GetSystem(ctx, "rn-system", all)
	if err != nil {
		t.Fatalf("get system after the rename: %v", err)
	}
	if sys.SystemTypeID == nil || *sys.SystemTypeID != st.ID.String() {
		t.Fatalf("system_type_id after the rename = %v, want the unchanged %v (no orphan)", sys.SystemTypeID, st.ID)
	}
	if sys.SystemTypeName == nil || *sys.SystemTypeName != "rn-after" {
		t.Fatalf("projected system_type handle = %v, want the new name rn-after (the projection follows the row)", sys.SystemTypeName)
	}
}

// TestSeedSystemTypesIdempotent proves the shipped tree seeds twice with no
// duplicate rows and no id churn, that the rows are official (authoritative
// upsert, like component_type's, not location_type's seed-if-absent), and that
// an operator row alongside them is left completely alone by a re-seed.
func TestSeedSystemTypesIdempotent(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first, err := gw.ListSystemTypes(ctx)
	if err != nil {
		t.Fatalf("list after first seed: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("no system_types after seeding; the seed shipped nothing")
	}

	room, err := gw.GetSystemType(ctx, "room")
	if err != nil {
		t.Fatalf("get seeded room: %v", err)
	}
	if !room.Official {
		t.Fatalf("seeded room official=false, want true")
	}

	// An operator row the seed must never touch.
	mine, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "sid-mine", DisplayName: "Mine", ParentID: &room.ID, Abbrev: strp("mn"),
	})
	if err != nil {
		t.Fatalf("create operator row: %v", err)
	}

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	second, err := gw.ListSystemTypes(ctx)
	if err != nil {
		t.Fatalf("list after second seed: %v", err)
	}
	if len(second) != len(first)+1 {
		t.Fatalf("count after re-seed = %d, want %d (the shipped set updated in place, plus the one operator row)", len(second), len(first)+1)
	}

	roomAgain, err := gw.GetSystemType(ctx, "room")
	if err != nil {
		t.Fatalf("get seeded room after re-seed: %v", err)
	}
	if roomAgain.ID != room.ID {
		t.Fatalf("room id changed across re-seed: %v -> %v, want stable (upsert, not insert)", room.ID, roomAgain.ID)
	}

	mineAgain, err := gw.GetSystemType(ctx, "sid-mine")
	if err != nil {
		t.Fatalf("get operator row after re-seed: %v", err)
	}
	if mineAgain.ID != mine.ID || mineAgain.Official || mineAgain.DisplayName != "Mine" || mineAgain.Abbrev == nil || *mineAgain.Abbrev != "mn" {
		t.Fatalf("operator row after re-seed = %+v, want it untouched (%+v)", mineAgain, mine)
	}

	// A seeded child resolves its parent by the real id the loader looked up,
	// and the official rows are read-only on both mutating verbs.
	if room.ParentID == nil {
		t.Fatal("seeded room has no parent; the shipped tree must nest under av")
	}
	x := "X"
	if _, err := gw.UpdateSystemType(ctx, "", "room", storage.SystemTypePatch{DisplayName: &x}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("patch official room err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteSystemType(ctx, "", "room"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official room err = %v, want ErrTypeOfficial", err)
	}
}

// TestRootSystemTypeRequiresStem mirrors component_type's structural guard: a
// root has no ancestor to inherit a stem from, so a nil stem there is not
// "inherit", it is "nothing", and every stemless descendant would resolve to
// "". A child with no stem is fine, which is the whole point of the tree.
func TestRootSystemTypeRequiresStem(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "rs-root", DisplayName: "Root"}); !errors.Is(err, storage.ErrRootSystemTypeNeedsStem) {
		t.Fatalf("stemless root err = %v, want ErrRootSystemTypeNeedsStem", err)
	}

	root, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "rs-root", DisplayName: "Root", Stem: strp("root")})
	if err != nil {
		t.Fatalf("create root with a stem: %v", err)
	}
	if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "rs-child", DisplayName: "Child", ParentID: &root.ID}); err != nil {
		t.Fatalf("a stemless CHILD must be legal (it inherits): %v", err)
	}
}

// TestSystemTypeStemMustBeAValidName pins the same rule component_type's stem
// carries: a stem is a name prefix #657's generator will mint straight into a
// system name, so it follows the entity name rule on create and on update. A
// nil stem (inherit) is untouched, since there is nothing to validate.
func TestSystemTypeStemMustBeAValidName(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, bad := range []string{"Bad Stem", "with.dot", "UPPER", "-leading"} {
		if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "sv-" + "x", DisplayName: "X", Stem: strp(bad)}); err == nil {
			t.Fatalf("create accepted the invalid stem %q", bad)
		}
	}

	ok, err := gw.CreateSystemType(ctx, "", storage.SystemType{Name: "sv-ok", DisplayName: "OK", Stem: strp("good-stem")})
	if err != nil {
		t.Fatalf("create with a valid stem: %v", err)
	}
	if ok.Stem == nil || *ok.Stem != "good-stem" {
		t.Fatalf("stem = %v, want good-stem", ok.Stem)
	}
	if _, err := gw.UpdateSystemType(ctx, "", "sv-ok", storage.SystemTypePatch{Stem: strp("also bad")}); err == nil {
		t.Fatal("update accepted an invalid stem")
	}
}
