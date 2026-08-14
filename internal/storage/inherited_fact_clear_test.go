package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The clearing half of the inherited registry facts (#716). `stem`, `abbrev`
// and `icon` on component_type and system_type are nullable and inherited: NULL
// means "walk to the nearest ancestor that sets one". #677 and #656 stopped both
// consoles writing "" over a NULL, which was destroying the walk; what neither
// could then express is the OPPOSITE move, a node that has its own value going
// back to inheriting its parent's.
//
// The instrument is the house three-state string sentinel already live on
// label_rule in these same two handlers ("" clears to NULL), not the
// update_mask, which ADR-0106 scopes to nullable OBJECT fields on the ground
// that an object has no empty value to overload. These tests assert the
// CONSEQUENCE (the walk resumes and the ancestor's value is what resolves)
// rather than that a column went null, because the column being null is only
// interesting for what the resolver then does with it.

// TestClearingAnInheritedComponentFactResumesTheWalk is the acceptance: a child
// carrying its own stem, icon and abbrev is patched with the sentinel on all
// three and every one of them resolves to the root's again.
func TestClearingAnInheritedComponentFactResumesTheWalk(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "cl-mic", DisplayName: "Mic", Stem: strp("mic"), Icon: strp("mic"), Abbrev: strp("mc"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "cl-wireless-mic", DisplayName: "Wireless Mic", ParentID: &root.ID,
		Stem: strp("wmic"), Icon: strp("radio"), Abbrev: strp("wm"),
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if _, err := gw.UpdateComponentType(ctx, "", child.Name, storage.ComponentTypePatch{
		Stem: strp(""), Icon: strp(""), Abbrev: strp(""),
	}); err != nil {
		t.Fatalf("clear the child's own facts: %v", err)
	}

	stem, icon, abbrev, _, err := gw.ResolveTypeFacts(ctx, child.ID)
	if err != nil {
		t.Fatalf("resolve facts: %v", err)
	}
	if stem != "mic" || icon != "mic" || abbrev != "mc" {
		t.Fatalf("resolved (stem,icon,abbrev) = (%q,%q,%q), want the root's (mic,mic,mc) back", stem, icon, abbrev)
	}

	// The row itself declares nothing now, which is what an operator reading
	// the blade's empty box is being told.
	got, err := gw.GetComponentType(ctx, child.Name)
	if err != nil {
		t.Fatalf("reload child: %v", err)
	}
	if got.Stem != nil || got.Icon != nil || got.Abbrev != nil {
		t.Fatalf("child's own facts = (%v,%v,%v), want all three null", got.Stem, got.Icon, got.Abbrev)
	}
}

// TestClearingOneComponentFactLeavesTheOthersAlone is the guard against a CASE
// written over the wrong parameter: an omitted field is still unchanged, so
// clearing the icon must not disturb the stem beside it.
func TestClearingOneComponentFactLeavesTheOthersAlone(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "cl1-mic", DisplayName: "Mic", Stem: strp("mic"), Icon: strp("mic"), Abbrev: strp("mc"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "cl1-boundary-mic", DisplayName: "Boundary Mic", ParentID: &root.ID,
		Stem: strp("bmic"), Icon: strp("radio"), Abbrev: strp("bm"),
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if _, err := gw.UpdateComponentType(ctx, "", child.Name, storage.ComponentTypePatch{Icon: strp("")}); err != nil {
		t.Fatalf("clear only the icon: %v", err)
	}
	got, err := gw.GetComponentType(ctx, child.Name)
	if err != nil {
		t.Fatalf("reload child: %v", err)
	}
	if got.Icon != nil {
		t.Fatalf("icon = %v, want cleared", got.Icon)
	}
	if got.Stem == nil || *got.Stem != "bmic" || got.Abbrev == nil || *got.Abbrev != "bm" {
		t.Fatalf("untouched facts = (%v,%v), want (bmic,bm)", got.Stem, got.Abbrev)
	}
}

// TestClearingARootComponentTypesStemIsRefused: the sentinel does not reach a
// root, which has no ancestor to inherit from. Create already refuses a stemless
// root (ErrRootComponentTypeNeedsStem); the clear is the second way to reach the
// same broken state and gets the same refusal, at the same tier, rather than a
// row the name generator later fails on.
func TestClearingARootComponentTypesStemIsRefused(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateComponentType(ctx, "", storage.ComponentType{
		Name: "cl2-root", DisplayName: "Root", Stem: strp("rootstem"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if _, err := gw.UpdateComponentType(ctx, "", root.Name, storage.ComponentTypePatch{
		Stem: strp(""),
	}); !errors.Is(err, storage.ErrRootComponentTypeNeedsStem) {
		t.Fatalf("clear a root's stem err = %v, want ErrRootComponentTypeNeedsStem", err)
	}
	got, err := gw.GetComponentType(ctx, root.Name)
	if err != nil {
		t.Fatalf("reload root: %v", err)
	}
	if got.Stem == nil || *got.Stem != "rootstem" {
		t.Fatalf("root stem = %v after the refusal, want it untouched", got.Stem)
	}

	// A root may still clear the two facts that have an ancestor-free meaning:
	// the refusal is about the stem a name is minted from, not about the row
	// being a root.
	if _, err := gw.UpdateComponentType(ctx, "", root.Name, storage.ComponentTypePatch{
		Icon: strp(""), Abbrev: strp(""),
	}); err != nil {
		t.Fatalf("clear a root's icon and abbrev: %v", err)
	}
}

// TestClearingAnInheritedFactOnAShippedTypeForks: the sentinel travels the fork
// leg too (#655, ADR-0095). A shipped row is never written, so clearing a fact
// on one stores a shadow that declares nothing, and :restore brings the shipped
// value back.
func TestClearingAnInheritedFactOnAShippedTypeForks(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	before, err := gw.GetComponentType(ctx, "mic")
	if err != nil {
		t.Fatalf("load the shipped mic type: %v", err)
	}
	if before.Abbrev == nil || *before.Abbrev == "" {
		t.Fatalf("the shipped mic type carries no abbrev to clear (%v); pick a seeded row that declares one", before.Abbrev)
	}
	shippedAbbrev := *before.Abbrev

	forked, err := gw.UpdateComponentType(ctx, "", "mic", storage.ComponentTypePatch{Abbrev: strp("")})
	if err != nil {
		t.Fatalf("clear a shipped type's abbrev: %v", err)
	}
	if !forked.Forked || forked.Abbrev != nil {
		t.Fatalf("forked = forked:%v abbrev:%v, want a fork declaring no abbrev", forked.Forked, forked.Abbrev)
	}
	restored, err := gw.RestoreComponentType(ctx, "", "mic")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Abbrev == nil || *restored.Abbrev != shippedAbbrev {
		t.Fatalf("restored abbrev = %v, want the shipped %q back", restored.Abbrev, shippedAbbrev)
	}
}

// TestClearingAnInheritedSystemFactResumesTheWalk is the system_type twin of the
// component acceptance. The registry differs (a shipped row here is read-only
// rather than forkable), the sentinel does not.
func TestClearingAnInheritedSystemFactResumesTheWalk(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "cl-space", DisplayName: "Space", Stem: strp("space"), Icon: strp("layers"), Abbrev: strp("sp"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "cl-huddle", DisplayName: "Huddle", ParentID: &root.ID,
		Stem: strp("huddle"), Icon: strp("door-open"), Abbrev: strp("hd"),
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if _, err := gw.UpdateSystemType(ctx, "", child.Name, storage.SystemTypePatch{
		Stem: strp(""), Icon: strp(""), Abbrev: strp(""),
	}); err != nil {
		t.Fatalf("clear the child's own facts: %v", err)
	}

	stem, icon, abbrev, err := gw.ResolveSystemTypeFacts(ctx, child.ID)
	if err != nil {
		t.Fatalf("resolve facts: %v", err)
	}
	if stem != "space" || icon != "layers" || abbrev != "sp" {
		t.Fatalf("resolved (stem,icon,abbrev) = (%q,%q,%q), want the root's (space,layers,sp) back", stem, icon, abbrev)
	}
	got, err := gw.GetSystemType(ctx, child.Name)
	if err != nil {
		t.Fatalf("reload child: %v", err)
	}
	if got.Stem != nil || got.Icon != nil || got.Abbrev != nil {
		t.Fatalf("child's own facts = (%v,%v,%v), want all three null", got.Stem, got.Icon, got.Abbrev)
	}
}

// TestClearingARootSystemTypesStemIsRefused is ErrRootComponentTypeNeedsStem's
// system twin on the clear path.
func TestClearingARootSystemTypesStemIsRefused(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	root, err := gw.CreateSystemType(ctx, "", storage.SystemType{
		Name: "cl2-space", DisplayName: "Space", Stem: strp("spacestem"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if _, err := gw.UpdateSystemType(ctx, "", root.Name, storage.SystemTypePatch{
		Stem: strp(""),
	}); !errors.Is(err, storage.ErrRootSystemTypeNeedsStem) {
		t.Fatalf("clear a root's stem err = %v, want ErrRootSystemTypeNeedsStem", err)
	}
	got, err := gw.GetSystemType(ctx, root.Name)
	if err != nil {
		t.Fatalf("reload root: %v", err)
	}
	if got.Stem == nil || *got.Stem != "spacestem" {
		t.Fatalf("root stem = %v after the refusal, want it untouched", got.Stem)
	}
}
