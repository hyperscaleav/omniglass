package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func strp(s string) *string { return &s }

// TestComponentTypeRoundTrip creates a root and a child under it and checks
// Get resolves both, the child carrying its parent's id.
func TestComponentTypeRoundTrip(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "rt-mic", DisplayName: "Mic", Stem: strp("mic"), Icon: strp("mic"), Abbrev: strp("mc"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if root.Name != "rt-mic" || root.DisplayName != "Mic" {
		t.Fatalf("root = %+v, want name rt-mic display_name Mic", root)
	}
	if root.Official {
		t.Fatalf("new component_type official=true, want false")
	}
	if root.ParentID != nil {
		t.Fatalf("root ParentID = %v, want nil", root.ParentID)
	}

	child, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "rt-wireless-mic", DisplayName: "Wireless Mic", ParentID: &root.ID,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != root.ID {
		t.Fatalf("child ParentID = %v, want %v", child.ParentID, root.ID)
	}

	gotRoot, err := gw.GetComponentType(ctx, "rt-mic")
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	if gotRoot.ID != root.ID {
		t.Fatalf("get root id = %v, want %v", gotRoot.ID, root.ID)
	}

	gotChild, err := gw.GetComponentType(ctx, "rt-wireless-mic")
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if gotChild.ParentID == nil || *gotChild.ParentID != root.ID {
		t.Fatalf("get child ParentID = %v, want %v", gotChild.ParentID, root.ID)
	}

	all, err := gw.ListComponentTypes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var foundRoot, foundChild bool
	for _, ct := range all {
		if ct.ID == root.ID {
			foundRoot = true
		}
		if ct.ID == child.ID {
			foundChild = true
		}
	}
	if !foundRoot || !foundChild {
		t.Fatalf("list missing root or child; found root=%v child=%v", foundRoot, foundChild)
	}

	dn := "Wireless Microphone"
	upd, err := gw.UpdateComponentType(ctx, "", "rt-wireless-mic", storage.ComponentTypePatch{DisplayName: &dn})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.DisplayName != dn {
		t.Fatalf("update display_name = %q, want %q", upd.DisplayName, dn)
	}

	// Duplicate name is ErrTypeExists. Stem set (a root needs one) so the
	// write reaches the unique-name check this asserts, not the stem guard.
	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "rt-mic", DisplayName: "Dup", Stem: strp("dup")}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// Unknown ref is ErrTypeNotFound.
	if _, err := gw.GetComponentType(ctx, "rt-nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrTypeNotFound", err)
	}

	// Leaf delete (no children) succeeds.
	if err := gw.DeleteComponentType(ctx, "", "rt-wireless-mic"); err != nil {
		t.Fatalf("delete child: %v", err)
	}
	if _, err := gw.GetComponentType(ctx, "rt-wireless-mic"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTypeNotFound", err)
	}
}

// TestComponentTypeUpdateTagsPatch covers both legs of ComponentTypePatch's
// DefaultTags field, the one field on the update path shaped differently from
// the rest (a *[]string against a text[] column, no precedent elsewhere in
// the gateway): a patch that omits it leaves the existing tags untouched, and
// a patch that sets it replaces the stored array, both confirmed by a
// separate Get so the assertion is against what actually persisted, not just
// the update call's return value.
func TestComponentTypeUpdateTagsPatch(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "tp-mic", DisplayName: "Mic", Stem: strp("mic"), DefaultTags: []string{"audio", "av"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(root.DefaultTags) != 2 || root.DefaultTags[0] != "audio" || root.DefaultTags[1] != "av" {
		t.Fatalf("create DefaultTags = %v, want [audio av]", root.DefaultTags)
	}

	// Leg 1: a patch that does not touch DefaultTags leaves it unchanged.
	dn := "Microphone"
	upd, err := gw.UpdateComponentType(ctx, "", "tp-mic", storage.ComponentTypePatch{DisplayName: &dn})
	if err != nil {
		t.Fatalf("update display_name only: %v", err)
	}
	if len(upd.DefaultTags) != 2 || upd.DefaultTags[0] != "audio" || upd.DefaultTags[1] != "av" {
		t.Fatalf("update (no tags patch) DefaultTags = %v, want unchanged [audio av]", upd.DefaultTags)
	}
	got, err := gw.GetComponentType(ctx, "tp-mic")
	if err != nil {
		t.Fatalf("get after display_name patch: %v", err)
	}
	if len(got.DefaultTags) != 2 || got.DefaultTags[0] != "audio" || got.DefaultTags[1] != "av" {
		t.Fatalf("get after display_name patch DefaultTags = %v, want unchanged [audio av]", got.DefaultTags)
	}

	// Leg 2: a patch that sets DefaultTags replaces the stored array, and the
	// replacement reads back on a fresh Get, not just the update's return.
	newTags := []string{"wireless"}
	upd2, err := gw.UpdateComponentType(ctx, "", "tp-mic", storage.ComponentTypePatch{DefaultTags: &newTags})
	if err != nil {
		t.Fatalf("update tags: %v", err)
	}
	if len(upd2.DefaultTags) != 1 || upd2.DefaultTags[0] != "wireless" {
		t.Fatalf("update tags DefaultTags = %v, want [wireless]", upd2.DefaultTags)
	}
	got2, err := gw.GetComponentType(ctx, "tp-mic")
	if err != nil {
		t.Fatalf("get after tags patch: %v", err)
	}
	if len(got2.DefaultTags) != 1 || got2.DefaultTags[0] != "wireless" {
		t.Fatalf("get after tags patch DefaultTags = %v, want [wireless]", got2.DefaultTags)
	}
	// display_name from the first patch survived the second, untouched patch.
	if got2.DisplayName != dn {
		t.Fatalf("get after tags patch DisplayName = %q, want %q (unaffected by the tags-only patch)", got2.DisplayName, dn)
	}

	// An explicit empty slice is a legal replacement (clears back to no tags),
	// distinct from a nil patch field (leave unchanged, leg 1 above).
	empty := []string{}
	upd3, err := gw.UpdateComponentType(ctx, "", "tp-mic", storage.ComponentTypePatch{DefaultTags: &empty})
	if err != nil {
		t.Fatalf("update tags to empty: %v", err)
	}
	if len(upd3.DefaultTags) != 0 {
		t.Fatalf("update tags to empty DefaultTags = %v, want empty", upd3.DefaultTags)
	}
	got3, err := gw.GetComponentType(ctx, "tp-mic")
	if err != nil {
		t.Fatalf("get after empty tags patch: %v", err)
	}
	if len(got3.DefaultTags) != 0 {
		t.Fatalf("get after empty tags patch DefaultTags = %v, want empty", got3.DefaultTags)
	}
}

// TestComponentTypeFactsInherit checks ResolveTypeFacts walks parent_id: a
// child with a null stem resolves the parent's, while a child's own non-null
// abbrev overrides the parent's.
func TestComponentTypeFactsInherit(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "fi-mic", DisplayName: "Mic", Stem: strp("mic"), Icon: strp("mic"), Abbrev: strp("mc"),
		DefaultTags: []string{"audio"},
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}

	child, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "fi-wireless-mic", DisplayName: "Wireless Mic", ParentID: &root.ID,
		Abbrev: strp("wm"), // override; stem, icon, tags null/empty so they inherit
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	stem, icon, abbrev, tags, err := gw.ResolveTypeFacts(ctx, child.ID)
	if err != nil {
		t.Fatalf("resolve facts: %v", err)
	}
	if stem != "mic" {
		t.Errorf("stem = %q, want inherited %q", stem, "mic")
	}
	if icon != "mic" {
		t.Errorf("icon = %q, want inherited %q", icon, "mic")
	}
	if abbrev != "wm" {
		t.Errorf("abbrev = %q, want the child's own override %q", abbrev, "wm")
	}
	if len(tags) != 1 || tags[0] != "audio" {
		t.Errorf("tags = %v, want inherited [audio]", tags)
	}

	// Resolving the root itself returns its own facts, no walk needed.
	rStem, rIcon, rAbbrev, rTags, err := gw.ResolveTypeFacts(ctx, root.ID)
	if err != nil {
		t.Fatalf("resolve root facts: %v", err)
	}
	if rStem != "mic" || rIcon != "mic" || rAbbrev != "mc" || len(rTags) != 1 || rTags[0] != "audio" {
		t.Errorf("root facts = (%q,%q,%q,%v), want (mic,mic,mc,[audio])", rStem, rIcon, rAbbrev, rTags)
	}
}

// TestComponentTypeDeleteRestricted proves the RESTRICT lesson from #507: a
// component_type with a child cannot be deleted.
func TestComponentTypeDeleteRestricted(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "dr-mic", DisplayName: "Mic", Stem: strp("mic")})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "dr-wireless-mic", DisplayName: "Wireless Mic", ParentID: &root.ID,
	}); err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := gw.DeleteComponentType(ctx, "", "dr-mic"); err == nil {
		t.Fatalf("delete parent with a child succeeded, want a refusal")
	}

	if _, err := gw.GetComponentType(ctx, "dr-mic"); err != nil {
		t.Fatalf("root should still exist after the refused delete: %v", err)
	}
}

// TestComponentTypeSubtree proves TypeIsWithin's self-inclusive descendant
// test: a child is within its root, a sibling tree's root is not.
func TestComponentTypeSubtree(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "st-mic", DisplayName: "Mic", Stem: strp("mic")})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "st-wireless-mic", DisplayName: "Wireless Mic", ParentID: &root.ID,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	sibling, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "st-camera", DisplayName: "Camera", Stem: strp("camera")})
	if err != nil {
		t.Fatalf("create sibling root: %v", err)
	}

	within, err := gw.TypeIsWithin(ctx, child.ID, root.ID)
	if err != nil {
		t.Fatalf("TypeIsWithin(child, root): %v", err)
	}
	if !within {
		t.Errorf("TypeIsWithin(child, root) = false, want true")
	}

	within, err = gw.TypeIsWithin(ctx, root.ID, root.ID)
	if err != nil {
		t.Fatalf("TypeIsWithin(root, root): %v", err)
	}
	if !within {
		t.Errorf("TypeIsWithin(root, root) = false, want true (self-inclusive)")
	}

	within, err = gw.TypeIsWithin(ctx, child.ID, sibling.ID)
	if err != nil {
		t.Fatalf("TypeIsWithin(child, sibling): %v", err)
	}
	if within {
		t.Errorf("TypeIsWithin(child, sibling) = true, want false")
	}
}

// TestSeedComponentTypesIdempotent proves the seed is idempotent and
// authoritative: seeding twice leaves the row count stable and updates the
// existing official rows in place rather than duplicating them.
func TestSeedComponentTypesIdempotent(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first, err := gw.ListComponentTypes(ctx)
	if err != nil {
		t.Fatalf("list after first seed: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("no component_types after seeding; the seed shipped nothing")
	}

	mic, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get seeded mic: %v", err)
	}
	if !mic.Official {
		t.Fatalf("seeded mic official=false, want true")
	}

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	second, err := gw.ListComponentTypes(ctx)
	if err != nil {
		t.Fatalf("list after second seed: %v", err)
	}
	if len(second) != len(first) {
		t.Fatalf("count after re-seed = %d, want %d (stable, updated not duplicated)", len(second), len(first))
	}

	micAgain, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("get seeded mic after re-seed: %v", err)
	}
	if micAgain.ID != mic.ID {
		t.Fatalf("mic id changed across re-seed: %v -> %v, want stable (upsert, not insert)", mic.ID, micAgain.ID)
	}

	// A seeded child resolves its parent by the real id the seed loader
	// looked up, and its official row is read-only.
	wireless, err := gw.GetComponentType(ctx, "wireless-mic")
	if err != nil {
		t.Fatalf("get seeded wireless-mic: %v", err)
	}
	if wireless.ParentID == nil || *wireless.ParentID != mic.ID {
		t.Fatalf("wireless-mic ParentID = %v, want %v (mic)", wireless.ParentID, mic.ID)
	}
	if err := gw.DeleteComponentType(ctx, "", "mic"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official mic err = %v, want ErrTypeOfficial", err)
	}
}

// TestComponentTypeStemMustBeAValidName proves the #627 Task 14 fix: a stem
// is a name prefix (the generator mints it straight into a component's
// name, internal/storage/namegen.go), so it is validated by the same rule
// name is, on both create and update. A nil stem (inherit from the parent)
// is untouched: there is nothing to validate.
func TestComponentTypeStemMustBeAValidName(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "stem-guard-create", DisplayName: "Stem Guard Create", Stem: strp("Bad Stem"),
	}); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Fatalf("create with stem %q err = %v, want ErrInvalidEntityName", "Bad Stem", err)
	}

	ct, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "stem-guard-ok", DisplayName: "Stem Guard OK", Stem: strp("good-stem"),
	})
	if err != nil {
		t.Fatalf("create with a valid stem: %v", err)
	}
	if ct.Stem == nil || *ct.Stem != "good-stem" {
		t.Fatalf("created stem = %v, want good-stem", ct.Stem)
	}

	// A nil stem (inherit) is unaffected by the guard: nothing to validate.
	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "stem-guard-nil", DisplayName: "Stem Guard Nil", ParentID: &ct.ID,
	}); err != nil {
		t.Fatalf("create with a nil (inherited) stem: %v", err)
	}

	if _, err := gw.UpdateComponentType(ctx, "", ct.Name, storage.ComponentTypePatch{Stem: strp("also bad")}); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Fatalf("update stem to %q err = %v, want ErrInvalidEntityName", "also bad", err)
	}
	if _, err := gw.UpdateComponentType(ctx, "", ct.Name, storage.ComponentTypePatch{Stem: strp("better-stem")}); err != nil {
		t.Fatalf("update with a valid stem: %v", err)
	}
}

// TestBadStemNeverProducesAnInvalidComponentName is the consequence-level
// regression the #627 Task 14 review caught a green suite missing: it does
// not assert HOW the invariant holds (the guard could move, or gain a
// second layer), it asserts the invariant itself, so it stays meaningful
// even if the fix's shape changes later. Either CreateComponentType refuses
// the bad stem outright (today's fix, the common case below), or, if it
// somehow did not, a component generated under it would still have to
// produce a name satisfying the same rule an operator-typed name does.
// Reverting the storage-layer guard alone (leaving the generator as
// written, which deliberately skips ValidateName by design) makes this test
// fail: CreateComponentType then succeeds with the bad stem, the component
// generates "Bad Stem-1", and the final ValidateName check catches it.
func TestBadStemNeverProducesAnInvalidComponentName(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ct, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "bad-stem-type", DisplayName: "Bad Stem Type", Stem: strp("Bad Stem"),
	})
	if err != nil {
		if !errors.Is(err, storage.ErrInvalidEntityName) {
			t.Fatalf("create component_type with a bad stem failed with %v, want ErrInvalidEntityName", err)
		}
		return // the guard closed this at the source; nothing downstream can see it
	}

	prod, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "bad-stem-product", DisplayName: "Bad Stem Product", Kind: "device", ComponentType: ct.Name,
	})
	if err != nil {
		t.Fatalf("create product under the unrefused bad-stem type: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &prod.Name}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component under the unrefused bad-stem product: %v", err)
	}
	if err := storage.ValidateName("component", c.Name); err != nil {
		t.Fatalf("a component generated under an unrefused bad stem produced an invalid name %q: %v", c.Name, err)
	}
}

// TestEmptyStemRefusesGeneration is the consequence-level regression for the
// ABSENT half of the stem invariant (TestBadStemNeverProducesAnInvalidComponentName
// covers the malformed half). CreateComponentType now refuses a stemless
// ROOT type outright (ErrRootComponentTypeNeedsStem), so the operator-facing
// create path can no longer reach this case; the only way left to construct
// it is UpsertComponentType, the trusted boot-seed primitive that
// deliberately bypasses every write-time guard here, the same reasoning
// that lets a seeded row skip character validation too. This is exactly the
// shape a root row from before this guard existed, or a future seed bug,
// would produce: ResolveTypeFacts's walk resolving "" all the way to the
// root with nothing to inherit. The generator must refuse to mint "-1" from
// that (ErrComponentTypeNoStem), not silently hand back an invalid name.
//
// Reverting the empty-stem guard in generateNameForProduct alone (leaving
// everything else as written) makes this test fail: CreateComponent then
// succeeds, minting the component name "-1", which is not what this test
// asserts and is not a legal entity name either.
func TestEmptyStemRefusesGeneration(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := gw.UpsertComponentType(ctx, storage.ComponentType{
		Name: "stemless-root", DisplayName: "Stemless Root", Official: true,
	}); err != nil {
		t.Fatalf("upsert a stemless root type: %v", err)
	}
	prod, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "stemless-product", DisplayName: "Stemless Product", Kind: "device", ComponentType: "stemless-root",
	})
	if err != nil {
		t.Fatalf("create product under the stemless type: %v", err)
	}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: &prod.Name}, all, all, all, all); !errors.Is(err, storage.ErrComponentTypeNoStem) {
		t.Fatalf("create component under a stemless type err = %v, want ErrComponentTypeNoStem", err)
	}
}

// TestRootComponentTypeRequiresStem is the direct, structural test of the
// third fix (#627 Task 14 review): a root component_type (no parent) is
// refused at create time with no stem, since it has no ancestor to inherit
// one from, while a CHILD with no stem still works exactly as before
// (TestComponentTypeStemMustBeAValidName's stem-guard-nil case already
// covers that; this test is specifically the root side of the same
// invariant).
func TestRootComponentTypeRequiresStem(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "root-no-stem", DisplayName: "Root No Stem",
	}); !errors.Is(err, storage.ErrRootComponentTypeNeedsStem) {
		t.Fatalf("create root with no stem err = %v, want ErrRootComponentTypeNeedsStem", err)
	}

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "root-with-stem", DisplayName: "Root With Stem", Stem: strp("rootstem"),
	})
	if err != nil {
		t.Fatalf("create root with a stem: %v", err)
	}
	// A child with no stem is unaffected: it has an ancestor to inherit from.
	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "child-no-stem", DisplayName: "Child No Stem", ParentID: &root.ID,
	}); err != nil {
		t.Fatalf("create child with no stem under a stemmed root: %v", err)
	}
}
