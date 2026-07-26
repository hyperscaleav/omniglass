package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSystemReparent covers the reparent path added for mutable system placement: a
// valid move, the cycle guard (under a descendant and under self), a clear-to-root,
// and an unknown parent. The tree starts sys-a > sys-mid > sys-leaf.
func TestSystemReparent(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mustCreateSystem(t, gw, storage.SystemSpec{Name: "sys-a"}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "sys-b"}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "sys-mid", ParentName: strptr("sys-a")}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "sys-leaf", ParentName: strptr("sys-mid")}, all)

	// Valid move: sys-mid reparents from sys-a to sys-b (tree becomes sys-b > sys-mid > sys-leaf).
	after, err := gw.UpdateSystem(ctx, "", "sys-mid", storage.SystemPatch{ParentName: strptr("sys-b")}, all, all)
	if err != nil {
		t.Fatalf("valid reparent: %v", err)
	}
	if after.ParentName == nil || *after.ParentName != "sys-b" {
		t.Fatalf("after reparent parent = %v, want sys-b", after.ParentName)
	}

	// Cycle: sys-b cannot move under sys-leaf, now its own descendant.
	if _, err := gw.UpdateSystem(ctx, "", "sys-b", storage.SystemPatch{ParentName: strptr("sys-leaf")}, all, all); !errors.Is(err, storage.ErrSystemCycle) {
		t.Fatalf("move sys-b under descendant err = %v, want ErrSystemCycle", err)
	}
	// Cycle: a system cannot move under itself.
	if _, err := gw.UpdateSystem(ctx, "", "sys-mid", storage.SystemPatch{ParentName: strptr("sys-mid")}, all, all); !errors.Is(err, storage.ErrSystemCycle) {
		t.Fatalf("move sys-mid under itself err = %v, want ErrSystemCycle", err)
	}

	// Clear to root.
	after, err = gw.UpdateSystem(ctx, "", "sys-mid", storage.SystemPatch{ParentName: strptr("")}, all, all)
	if err != nil {
		t.Fatalf("clear parent: %v", err)
	}
	if after.ParentName != nil || after.ParentID != nil {
		t.Fatalf("after clear parent = %v / %v, want nil / nil", after.ParentName, after.ParentID)
	}

	// Unknown parent is a by-name 422.
	if _, err := gw.UpdateSystem(ctx, "", "sys-mid", storage.SystemPatch{ParentName: strptr("ghost")}, all, all); !errors.Is(err, storage.ErrParentSystemNotFound) {
		t.Fatalf("unknown parent err = %v, want ErrParentSystemNotFound", err)
	}
}

// TestSystemReparentScope proves the new parent is scope-injected: a reparent onto a
// parent outside the caller's action scope is forbidden even when the moved system is
// itself in scope, and a reparent onto an in-scope parent succeeds.
func TestSystemReparentScope(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rootA := mustCreateSystem(t, gw, storage.SystemSpec{Name: "ss-root-a"}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "ss-root-b"}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "ss-in-a", ParentName: strptr("ss-root-a")}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "ss-move", ParentName: strptr("ss-root-a")}, all)

	actorA := scope.Set{IDs: []string{rootA.ID}}

	// Onto an out-of-scope parent (ss-root-b): forbidden.
	if _, err := gw.UpdateSystem(ctx, "", "ss-move", storage.SystemPatch{ParentName: strptr("ss-root-b")}, actorA, actorA); !errors.Is(err, storage.ErrSystemForbidden) {
		t.Fatalf("reparent onto out-of-scope parent err = %v, want ErrSystemForbidden", err)
	}
	// Onto an in-scope parent (ss-in-a): allowed.
	if _, err := gw.UpdateSystem(ctx, "", "ss-move", storage.SystemPatch{ParentName: strptr("ss-in-a")}, actorA, actorA); err != nil {
		t.Fatalf("reparent onto in-scope parent: %v", err)
	}
}
