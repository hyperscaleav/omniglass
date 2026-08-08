package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestSystemReparent covers the reparent path on MoveSystem: a valid move, the
// cycle guard (under a descendant and under self), a clear-to-root (with the
// all-scoped grant that now guards it), and an unknown parent. The tree starts
// sys-a > sys-mid > sys-leaf.
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
	// This is also TestMoveSystemReparents: the regression a naive plan that
	// registered :move with {location} only would have introduced (deleting
	// the only entry point for a system reparent and making the cycle guard
	// below unreachable).
	after, err := gw.MoveSystem(ctx, "", "sys-mid", storage.SystemMove{ParentName: strptr("sys-b")}, all, all)
	if err != nil {
		t.Fatalf("valid reparent: %v", err)
	}
	if after.ParentName == nil || *after.ParentName != "sys-b" {
		t.Fatalf("after reparent parent = %v, want sys-b", after.ParentName)
	}

	// Cycle: sys-b cannot move under sys-leaf, now its own descendant.
	if _, err := gw.MoveSystem(ctx, "", "sys-b", storage.SystemMove{ParentName: strptr("sys-leaf")}, all, all); !errors.Is(err, storage.ErrSystemCycle) {
		t.Fatalf("move sys-b under descendant err = %v, want ErrSystemCycle", err)
	}
	// Cycle: a system cannot move under itself.
	if _, err := gw.MoveSystem(ctx, "", "sys-mid", storage.SystemMove{ParentName: strptr("sys-mid")}, all, all); !errors.Is(err, storage.ErrSystemCycle) {
		t.Fatalf("move sys-mid under itself err = %v, want ErrSystemCycle", err)
	}

	// Clear to root. all is an all-scoped grant, so the new authorization guard
	// on clearing to root does not fire here.
	after, err = gw.MoveSystem(ctx, "", "sys-mid", storage.SystemMove{ParentName: strptr("")}, all, all)
	if err != nil {
		t.Fatalf("clear parent: %v", err)
	}
	if after.ParentName != nil || after.ParentID != nil {
		t.Fatalf("after clear parent = %v / %v, want nil / nil", after.ParentName, after.ParentID)
	}

	// Unknown parent is a by-name 422.
	if _, err := gw.MoveSystem(ctx, "", "sys-mid", storage.SystemMove{ParentName: strptr("ghost")}, all, all); !errors.Is(err, storage.ErrParentSystemNotFound) {
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
	if _, err := gw.MoveSystem(ctx, "", "ss-move", storage.SystemMove{ParentName: strptr("ss-root-b")}, actorA, actorA); !errors.Is(err, storage.ErrSystemForbidden) {
		t.Fatalf("reparent onto out-of-scope parent err = %v, want ErrSystemForbidden", err)
	}
	// Onto an in-scope parent (ss-in-a): allowed.
	if _, err := gw.MoveSystem(ctx, "", "ss-move", storage.SystemMove{ParentName: strptr("ss-in-a")}, actorA, actorA); err != nil {
		t.Fatalf("reparent onto in-scope parent: %v", err)
	}
}

// TestSystemScopedPrincipalCannotLiftToRoot is TestScopedPrincipalCannotLiftToRoot's
// system twin: a system-scoped (not all-scoped) principal is refused a
// clear-to-root move even though the system itself sits inside its own move
// scope, and remains free to reparent within its own subtree.
func TestSystemScopedPrincipalCannotLiftToRoot(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rootA := mustCreateSystem(t, gw, storage.SystemSpec{Name: "sys-lift-root-a"}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "sys-lift-sibling", ParentName: strptr("sys-lift-root-a")}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "sys-lift-target", ParentName: strptr("sys-lift-root-a")}, all)

	scoped := scope.Set{IDs: []string{rootA.ID}}

	if _, err := gw.MoveSystem(ctx, "", "sys-lift-target", storage.SystemMove{ParentName: strptr("")}, scoped, scoped); !errors.Is(err, storage.ErrSystemForbidden) {
		t.Fatalf("scoped clear-to-root err = %v, want ErrSystemForbidden", err)
	}
	if _, err := gw.MoveSystem(ctx, "", "sys-lift-target", storage.SystemMove{ParentName: strptr("sys-lift-sibling")}, scoped, scoped); err != nil {
		t.Fatalf("scoped reparent within own subtree: %v", err)
	}
	if _, err := gw.MoveSystem(ctx, "", "sys-lift-target", storage.SystemMove{ParentName: strptr("")}, all, all); err != nil {
		t.Fatalf("all-scoped clear-to-root: %v", err)
	}
}

// TestMoveSystemAudited is TestMoveComponentAudited's system twin: :move
// writes its own DISTINCT audit verb, "move", not the generic "update" a
// PATCH writes.
func TestMoveSystemAudited(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mustCreateSystem(t, gw, storage.SystemSpec{Name: "sys-audit-root"}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "sys-audit-leaf"}, all)

	if _, err := gw.MoveSystem(ctx, "", "sys-audit-leaf", storage.SystemMove{ParentName: strptr("sys-audit-root")}, all, all); err != nil {
		t.Fatalf("move: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var verb string
	if err := conn.QueryRow(ctx,
		`select verb from audit_log where resource = 'system' and resource_id = (select id::text from system where name = 'sys-audit-leaf') order by ts desc limit 1`,
	).Scan(&verb); err != nil {
		t.Fatalf("query audit row: %v", err)
	}
	if verb != "move" {
		t.Fatalf("audit verb = %q, want move", verb)
	}
}
