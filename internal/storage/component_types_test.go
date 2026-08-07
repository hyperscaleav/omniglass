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

	// Duplicate name is ErrTypeExists.
	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "rt-mic", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
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
		Name: "tp-mic", DisplayName: "Mic", DefaultTags: []string{"audio", "av"},
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

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "dr-mic", DisplayName: "Mic"})
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

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "st-mic", DisplayName: "Mic"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "st-wireless-mic", DisplayName: "Wireless Mic", ParentID: &root.ID,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	sibling, err := gw.CreateComponentType(ctx, "", storage.ComponentType{Name: "st-camera", DisplayName: "Camera"})
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
