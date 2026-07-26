package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestComponentReparent covers the reparent path added for mutable placement: a
// valid move, the cycle guard (under a descendant and under self), a clear-to-root,
// and an unknown parent. The tree starts root-a > mid > leaf.
func TestComponentReparent(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "root-a"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "root-b"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "mid", ParentName: strptr("root-a")}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "leaf", ParentName: strptr("mid")}, all)

	// Valid move: mid reparents from root-a to root-b (tree becomes root-b > mid > leaf).
	after, err := gw.UpdateComponent(ctx, "", "mid", storage.ComponentPatch{ParentName: strptr("root-b")}, all, all)
	if err != nil {
		t.Fatalf("valid reparent: %v", err)
	}
	if after.ParentName == nil || *after.ParentName != "root-b" {
		t.Fatalf("after reparent parent = %v, want root-b", after.ParentName)
	}

	// Cycle: root-b cannot move under leaf, which is now its own descendant.
	if _, err := gw.UpdateComponent(ctx, "", "root-b", storage.ComponentPatch{ParentName: strptr("leaf")}, all, all); !errors.Is(err, storage.ErrComponentCycle) {
		t.Fatalf("move root-b under descendant leaf err = %v, want ErrComponentCycle", err)
	}
	// Cycle: a component cannot move under itself.
	if _, err := gw.UpdateComponent(ctx, "", "mid", storage.ComponentPatch{ParentName: strptr("mid")}, all, all); !errors.Is(err, storage.ErrComponentCycle) {
		t.Fatalf("move mid under itself err = %v, want ErrComponentCycle", err)
	}

	// Clear to root: an empty parent lifts mid to a root component.
	after, err = gw.UpdateComponent(ctx, "", "mid", storage.ComponentPatch{ParentName: strptr("")}, all, all)
	if err != nil {
		t.Fatalf("clear parent: %v", err)
	}
	if after.ParentName != nil || after.ParentID != nil {
		t.Fatalf("after clear parent = %v / %v, want nil / nil", after.ParentName, after.ParentID)
	}

	// Unknown parent is a by-name 422 (ErrParentComponentNotFound), not a generic miss.
	if _, err := gw.UpdateComponent(ctx, "", "mid", storage.ComponentPatch{ParentName: strptr("ghost")}, all, all); !errors.Is(err, storage.ErrParentComponentNotFound) {
		t.Fatalf("unknown parent err = %v, want ErrParentComponentNotFound", err)
	}
}

// TestComponentRelocate covers the location three-state added to the patch: set,
// clear, and an unknown location.
func TestComponentRelocate(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mustCreate(t, gw, storage.LocationSpec{Name: "loc-1", LocationType: "campus"}, all)
	mustCreate(t, gw, storage.LocationSpec{Name: "loc-2", LocationType: "campus"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "cam", LocationName: strptr("loc-1")}, all)

	after, err := gw.UpdateComponent(ctx, "", "cam", storage.ComponentPatch{LocationName: strptr("loc-2")}, all, all)
	if err != nil {
		t.Fatalf("relocate: %v", err)
	}
	if after.LocationName == nil || *after.LocationName != "loc-2" {
		t.Fatalf("relocated to %v, want loc-2", after.LocationName)
	}

	after, err = gw.UpdateComponent(ctx, "", "cam", storage.ComponentPatch{LocationName: strptr("")}, all, all)
	if err != nil {
		t.Fatalf("clear location: %v", err)
	}
	if after.LocationName != nil || after.LocationID != nil {
		t.Fatalf("after clear location = %v / %v, want nil / nil", after.LocationName, after.LocationID)
	}

	if _, err := gw.UpdateComponent(ctx, "", "cam", storage.ComponentPatch{LocationName: strptr("nowhere")}, all, all); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("unknown location err = %v, want ErrLocationNotFound", err)
	}
}

// TestComponentProductSwapKeepsSetValues is the quiet-wrong-answer guard: swapping a
// component's product keeps every explicitly-set property value (they key by
// component and property_type, not by product) while the new product's contract
// defaults take over for the properties nobody set.
func TestComponentProductSwapKeepsSetValues(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Two products declaring firmware_version with different defaults; prod-b also
	// declares serial_number, which prod-a does not.
	for _, p := range []struct{ name, def string }{{"prod-a", `"A-default"`}, {"prod-b", `"B-default"`}} {
		if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: p.name, DisplayName: p.name, Kind: "device"}); err != nil {
			t.Fatalf("create %s: %v", p.name, err)
		}
		if _, err := gw.SetProductProperty(ctx, "", p.name, storage.ProductPropertySpec{
			PropertyTypeName: "firmware_version", DefaultValue: json.RawMessage(p.def),
		}); err != nil {
			t.Fatalf("contract %s: %v", p.name, err)
		}
	}
	if _, err := gw.SetProductProperty(ctx, "", "prod-b", storage.ProductPropertySpec{PropertyTypeName: "serial_number"}); err != nil {
		t.Fatalf("prod-b serial contract: %v", err)
	}

	pa := "prod-a"
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "dev", ProductName: &pa}, all)

	// Pin firmware_version on the component, overriding prod-a's default.
	if _, err := gw.SetProperty(ctx, "", "component", "dev", "firmware_version", "", json.RawMessage(`"pinned-1.2.3"`), all); err != nil {
		t.Fatalf("set firmware: %v", err)
	}

	if _, err := gw.UpdateComponent(ctx, "", "dev", storage.ComponentPatch{ProductName: strptr("prod-b")}, all, all); err != nil {
		t.Fatalf("swap product: %v", err)
	}

	got, err := gw.EffectiveProperties(ctx, "component", "dev", all)
	if err != nil {
		t.Fatalf("effective properties: %v", err)
	}
	idx := byName(got)

	// The pinned value survives the product swap untouched.
	if fw := idx["firmware_version"]; string(fw.Value) != `"pinned-1.2.3"` || !fw.IsSet {
		t.Fatalf("firmware after swap = %+v, want pinned-1.2.3, is_set=true", fw)
	}
	// prod-b's new contract property resolves from B's contract, unset.
	if sn, ok := idx["serial_number"]; !ok || sn.IsSet || !sn.FromContract {
		t.Fatalf("serial_number after swap = %+v, want from_contract unset", sn)
	}
}

// TestComponentReparentScope proves the new parent is scope-injected: a reparent onto
// a parent outside the caller's action scope is forbidden even when the moved
// component is itself in scope, and a reparent onto an in-scope parent succeeds.
func TestComponentReparentScope(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rootA := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "sc-root-a"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "sc-root-b"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "sc-in-a", ParentName: strptr("sc-root-a")}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "sc-move", ParentName: strptr("sc-root-a")}, all)

	// The actor can act only within root-a's subtree (which holds sc-move and sc-in-a,
	// but not the separate root sc-root-b).
	actorA := scope.Set{IDs: []string{rootA.ID}}

	// Onto an out-of-scope parent (sc-root-b): forbidden, even though sc-move itself
	// is in scope. The new parent is gated too.
	if _, err := gw.UpdateComponent(ctx, "", "sc-move", storage.ComponentPatch{ParentName: strptr("sc-root-b")}, actorA, actorA); !errors.Is(err, storage.ErrComponentForbidden) {
		t.Fatalf("reparent onto out-of-scope parent err = %v, want ErrComponentForbidden", err)
	}
	// Onto an in-scope parent (sc-in-a, under root-a): allowed.
	if _, err := gw.UpdateComponent(ctx, "", "sc-move", storage.ComponentPatch{ParentName: strptr("sc-in-a")}, actorA, actorA); err != nil {
		t.Fatalf("reparent onto in-scope parent: %v", err)
	}
}
