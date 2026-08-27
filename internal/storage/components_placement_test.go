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
	"github.com/jackc/pgx/v5"
)

// TestComponentReparent covers the reparent path on MoveComponent: a valid
// move, the cycle guard (under a descendant and under self), a clear-to-root
// (with the all-scoped grant that now guards it), and an unknown parent. The
// tree starts root-a > mid > leaf.
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
	after, err := gw.MoveComponent(ctx, "", "mid", storage.ComponentMove{ParentName: strptr("root-b")}, all, all, all)
	if err != nil {
		t.Fatalf("valid reparent: %v", err)
	}
	if after.ParentName == nil || *after.ParentName != "root-b" {
		t.Fatalf("after reparent parent = %v, want root-b", after.ParentName)
	}

	// Cycle: root-b cannot move under leaf, which is now its own descendant.
	if _, err := gw.MoveComponent(ctx, "", "root-b", storage.ComponentMove{ParentName: strptr("leaf")}, all, all, all); !errors.Is(err, storage.ErrComponentCycle) {
		t.Fatalf("move root-b under descendant leaf err = %v, want ErrComponentCycle", err)
	}
	// Cycle: a component cannot move under itself.
	if _, err := gw.MoveComponent(ctx, "", "mid", storage.ComponentMove{ParentName: strptr("mid")}, all, all, all); !errors.Is(err, storage.ErrComponentCycle) {
		t.Fatalf("move mid under itself err = %v, want ErrComponentCycle", err)
	}

	// Clear to root: an empty parent lifts mid to a root component. all is an
	// all-scoped grant, so the new authorization guard on clearing to root does
	// not fire here; TestScopedPrincipalCannotLiftToRoot covers the refusal.
	after, err = gw.MoveComponent(ctx, "", "mid", storage.ComponentMove{ParentName: strptr("")}, all, all, all)
	if err != nil {
		t.Fatalf("clear parent: %v", err)
	}
	if after.ParentName != nil || after.ParentID != nil {
		t.Fatalf("after clear parent = %v / %v, want nil / nil", after.ParentName, after.ParentID)
	}

	// Unknown parent is a by-name 422 (ErrParentComponentNotFound), not a generic miss.
	if _, err := gw.MoveComponent(ctx, "", "mid", storage.ComponentMove{ParentName: strptr("ghost")}, all, all, all); !errors.Is(err, storage.ErrParentComponentNotFound) {
		t.Fatalf("unknown parent err = %v, want ErrParentComponentNotFound", err)
	}
}

// TestComponentRelocate covers the location three-state on MoveComponent: set,
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

	after, err := gw.MoveComponent(ctx, "", "cam", storage.ComponentMove{LocationName: strptr("loc-2")}, all, all, all)
	if err != nil {
		t.Fatalf("relocate: %v", err)
	}
	if after.LocationName == nil || *after.LocationName != "loc-2" {
		t.Fatalf("relocated to %v, want loc-2", after.LocationName)
	}

	after, err = gw.MoveComponent(ctx, "", "cam", storage.ComponentMove{LocationName: strptr("")}, all, all, all)
	if err != nil {
		t.Fatalf("clear location: %v", err)
	}
	if after.LocationName != nil || after.LocationID != nil {
		t.Fatalf("after clear location = %v / %v, want nil / nil", after.LocationName, after.LocationID)
	}

	if _, err := gw.MoveComponent(ctx, "", "cam", storage.ComponentMove{LocationName: strptr("nowhere")}, all, all, all); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("unknown location err = %v, want ErrLocationNotFound", err)
	}
}

// TestScopedPrincipalCannotLiftToRoot is the load-bearing new case (#627 Task
// 13): the ADR's whole justification is that a PATCH clearing parent_id used
// to lift a row out of every subtree scope with no check, while CREATING the
// same root already required an all-scoped grant. A component-scoped (not
// all-scoped) principal must be refused the clear-to-root move even though the
// component itself sits inside its own move scope, and the same principal must
// still be able to reparent within its own subtree (proving the refusal is
// specific to root, not a blanket denial).
func TestScopedPrincipalCannotLiftToRoot(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rootA := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "lift-root-a"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "lift-sibling", ParentName: strptr("lift-root-a")}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "lift-target", ParentName: strptr("lift-root-a")}, all)

	// Scoped to root-a's own subtree only, never all: exactly the principal the
	// ADR describes, who can already write everything inside root-a.
	scoped := scope.Set{IDs: []string{rootA.ID}}

	// Clearing to root is refused: this is the gap the ADR closes. Before this
	// task's guard, lift-target would walk straight out of root-a's subtree with
	// no check at all.
	if _, err := gw.MoveComponent(ctx, "", "lift-target", storage.ComponentMove{ParentName: strptr("")}, scoped, scoped, all); !errors.Is(err, storage.ErrComponentForbidden) {
		t.Fatalf("scoped clear-to-root err = %v, want ErrComponentForbidden", err)
	}

	// The same principal can still reparent WITHIN its own subtree: the refusal
	// above is specific to root, not a blanket denial of the move capability.
	if _, err := gw.MoveComponent(ctx, "", "lift-target", storage.ComponentMove{ParentName: strptr("lift-sibling")}, scoped, scoped, all); err != nil {
		t.Fatalf("scoped reparent within own subtree: %v", err)
	}

	// An all-scoped principal CAN clear the same component to root: the guard is
	// specifically about scope, not a general prohibition on lift-to-root.
	if _, err := gw.MoveComponent(ctx, "", "lift-target", storage.ComponentMove{ParentName: strptr("")}, all, all, all); err != nil {
		t.Fatalf("all-scoped clear-to-root: %v", err)
	}
}

// TestMoveComponentAudited proves :move writes its own DISTINCT audit verb,
// "move", not the generic "update" a PATCH writes, and that the old/new images
// carry the containment change. Without this, an acceptance criterion phrased
// as "the move is auditable" would already be satisfied by the old
// verb="update" row whose JSON happened to contain parent_id, forcing nothing.
func TestMoveComponentAudited(t *testing.T) {
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

	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "audit-root"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "audit-leaf"}, all)

	if _, err := gw.MoveComponent(ctx, "", "audit-leaf", storage.ComponentMove{ParentName: strptr("audit-root")}, all, all, all); err != nil {
		t.Fatalf("move: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var verb string
	var oldJSON, newJSON []byte
	if err := conn.QueryRow(ctx,
		`select verb, old, new from audit_log where resource = 'component' and resource_id = (select id::text from component where name = 'audit-leaf') order by ts desc limit 1`,
	).Scan(&verb, &oldJSON, &newJSON); err != nil {
		t.Fatalf("query audit row: %v", err)
	}
	if verb != "move" {
		t.Fatalf("audit verb = %q, want move (a generic update row would satisfy a loose 'the move is auditable' check while proving nothing)", verb)
	}
	var old, new struct {
		ParentID *string `json:"ParentID"`
	}
	if err := json.Unmarshal(oldJSON, &old); err != nil {
		t.Fatalf("decode old image: %v", err)
	}
	if err := json.Unmarshal(newJSON, &new); err != nil {
		t.Fatalf("decode new image: %v", err)
	}
	if old.ParentID != nil {
		t.Errorf("old image parent_id = %v, want nil (audit-leaf started at root)", old.ParentID)
	}
	if new.ParentID == nil {
		t.Errorf("new image parent_id = nil, want the resolved parent id")
	}
}

// TestMoveCollisionNamesBothParties proves the 409 a name collision at the move
// destination produces names both the moved component and the destination,
// using the per-index sentinels the 23505 mapper splits on (ErrComponentExistsUnderParent),
// not a generic ErrComponentExists.
//
// A move never renames, so a real repro needs two components ALREADY sharing
// the literal name "dup" in different placement buckets (#627: uniqueness is
// scoped to placement, so this is legal), one moving into the other's bucket.
// With both under an all-scoped caller, "dup" is ambiguous fleet-wide before
// the move is ever attempted (ErrAmbiguousName, proving nothing about the
// collision this test targets), so the mover is addressed with a READ scope
// narrowed to its own bucket (mover-root's subtree, where only one "dup"
// lives) while the MOVE scope covers both mover-root and the destination, the
// same "scope decides before ambiguity does" resolution every other bare-name
// reference in this codebase relies on.
func TestMoveCollisionNamesBothParties(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	collideDest := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "collide-dest"}, all)
	// An operator-owned duplicate already sits at the destination: a component
	// also named "dup" already exists under collide-dest.
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "dup", ParentName: strptr("collide-dest")}, all)
	// The mover's own bucket: a separate root, holding the second "dup".
	moverRoot := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "mover-root"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "dup", ParentName: strptr("mover-root")}, all)

	read := scope.Set{IDs: []string{moverRoot.ID}}
	action := scope.Set{IDs: []string{moverRoot.ID, collideDest.ID}}

	_, err := gw.MoveComponent(ctx, "", "dup", storage.ComponentMove{ParentName: strptr("collide-dest")}, read, action, all)
	if !errors.Is(err, storage.ErrComponentExistsUnderParent) {
		t.Fatalf("collision err = %v, want ErrComponentExistsUnderParent", err)
	}
}

// TestMoveRecomputesGeneratedName proves a still-platform-owned name is
// recomputed at its destination, in both directions the rule can move: the
// ordinal alone (a same-typed sibling scope with one already occupied) and
// the stem itself (a reparent under a differently-typed subtree). An
// operator-typed name at the same destination is left completely alone,
// proving the recompute is conditional on name_generated, not automatic for
// every mover.
func TestMoveRecomputesGeneratedName(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	edge55 := "boreal-edge-55"
	mic := "lyra-arc-a2"

	// A generated display under root-1 becomes display-1. A second generated
	// display already sits under root-2, so root-2's display bucket has
	// display-1 taken before the mover arrives: the destination forces a
	// new ordinal.
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "mv-root-1"}, all)
	root2 := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "mv-root-2"}, all)
	mover, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ParentName: strptr("mv-root-1"), ProductName: &edge55}, all, all, all, all)
	if err != nil {
		t.Fatalf("create generated mover: %v", err)
	}
	if mover.Name != "display-1" || !mover.NameGenerated {
		t.Fatalf("mover before move = %q (generated=%v), want display-1/true", mover.Name, mover.NameGenerated)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ParentName: strptr("mv-root-2"), ProductName: &edge55}, all, all, all, all); err != nil {
		t.Fatalf("occupy destination's display-1: %v", err)
	}

	after, err := gw.MoveComponent(ctx, "", mover.ID, storage.ComponentMove{ParentName: strptr("mv-root-2")}, all, all, all)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if after.Name != "display-2" {
		t.Fatalf("name after move to occupied bucket = %q, want display-2 (destination's display-1 was already taken)", after.Name)
	}
	if !after.NameGenerated {
		t.Fatalf("name_generated after move = false, want true (still platform-owned)")
	}
	if after.ParentID == nil || *after.ParentID != root2.ID {
		t.Fatalf("parent after move = %v, want %q", after.ParentID, root2.ID)
	}

	// A reparent under a differently-typed subtree recomputes the STEM too:
	// a mic-classified component moving under a fresh, empty root becomes
	// mic-1, not display-anything.
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "mv-mic-root"}, all)
	genMic, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ParentName: strptr("mv-root-1"), ProductName: &mic}, all, all, all, all)
	if err != nil {
		t.Fatalf("create generated mic: %v", err)
	}
	if genMic.Name != "mic-1" {
		t.Fatalf("generated mic name = %q, want mic-1 (lyra-arc-a2 classifies under ceiling-mic)", genMic.Name)
	}
	afterReparent, err := gw.MoveComponent(ctx, "", genMic.ID, storage.ComponentMove{ParentName: strptr("mv-mic-root")}, all, all, all)
	if err != nil {
		t.Fatalf("reparent: %v", err)
	}
	if afterReparent.Name != "mic-1" {
		t.Fatalf("name after reparent = %q, want mic-1 (stem is a property of the product, unaffected by placement)", afterReparent.Name)
	}

	// An operator-typed name at the SAME kind of move is left untouched: the
	// recompute is conditional on name_generated, not unconditional for
	// every mover.
	typed := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "keep-my-name", ParentName: strptr("mv-root-1"), ProductName: &edge55}, all)
	afterTyped, err := gw.MoveComponent(ctx, "", typed.ID, storage.ComponentMove{ParentName: strptr("mv-root-2")}, all, all, all)
	if err != nil {
		t.Fatalf("move operator-named component: %v", err)
	}
	if afterTyped.Name != "keep-my-name" {
		t.Fatalf("operator-typed name after move = %q, want unchanged keep-my-name", afterTyped.Name)
	}
	if afterTyped.NameGenerated {
		t.Fatalf("operator-typed component reports name_generated=true after move")
	}
}

// TestComponentUpdateEmptyProductReclassifiesToGeneric proves UpdateComponent
// stays consistent with CreateComponent now that product_id is NOT NULL (the
// #614 floor): an explicit empty-string product patch used to clear the
// column to null (the old "productless component" state); that state no
// longer exists, so the patch must not attempt the null write the column can
// no longer accept. This calls the gateway directly (the API refuses an
// empty-string product before it ever reaches here), so it is the only place
// this path is still exercised.
func TestComponentUpdateEmptyProductReclassifiesToGeneric(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	bar := "kestrel-vroom"
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "reclass", ProductName: &bar}, all)

	after, err := gw.UpdateComponent(ctx, "", "reclass", storage.ComponentPatch{ProductName: strptr("")}, all, all)
	if err != nil {
		t.Fatalf("empty-string product patch: %v (want a clean reclassify to generic-device, not a not-null violation)", err)
	}
	if after.ProductHandle == nil || *after.ProductHandle != "generic-device" {
		t.Fatalf("product after empty-string patch = %v, want generic-device", after.ProductHandle)
	}
	if after.ProductID == nil || *after.ProductID == "" {
		t.Fatalf("product_id after empty-string patch = %v, want a resolved generic-device id", after.ProductID)
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

	// Two products declaring firmware-version with different defaults; prod-b also
	// declares serial-number, which prod-a does not.
	for _, p := range []struct{ name, def string }{{"prod-a", `"A-default"`}, {"prod-b", `"B-default"`}} {
		if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: p.name, Label: p.name, Kind: "device"}); err != nil {
			t.Fatalf("create %s: %v", p.name, err)
		}
		if _, err := gw.SetProductProperty(ctx, "", p.name, storage.ProductPropertySpec{
			PropertyTypeName: "firmware-version", DefaultValue: json.RawMessage(p.def),
		}); err != nil {
			t.Fatalf("contract %s: %v", p.name, err)
		}
	}
	if _, err := gw.SetProductProperty(ctx, "", "prod-b", storage.ProductPropertySpec{PropertyTypeName: "serial-number"}); err != nil {
		t.Fatalf("prod-b serial contract: %v", err)
	}

	pa := "prod-a"
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "dev", ProductName: &pa}, all)

	// Pin firmware-version on the component, overriding prod-a's default.
	if _, err := gw.SetProperty(ctx, "", "component", "dev", "firmware-version", "", json.RawMessage(`"pinned-1.2.3"`), all, all); err != nil {
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
	if fw := idx["firmware-version"]; string(fw.Value) != `"pinned-1.2.3"` || !fw.IsSet {
		t.Fatalf("firmware after swap = %+v, want pinned-1.2.3, is_set=true", fw)
	}
	// prod-b's new contract property resolves from B's contract, unset.
	if sn, ok := idx["serial-number"]; !ok || sn.IsSet || !sn.FromContract {
		t.Fatalf("serial-number after swap = %+v, want from_contract unset", sn)
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
	if _, err := gw.MoveComponent(ctx, "", "sc-move", storage.ComponentMove{ParentName: strptr("sc-root-b")}, actorA, actorA, all); !errors.Is(err, storage.ErrComponentForbidden) {
		t.Fatalf("reparent onto out-of-scope parent err = %v, want ErrComponentForbidden", err)
	}
	// Onto an in-scope parent (sc-in-a, under root-a): allowed.
	if _, err := gw.MoveComponent(ctx, "", "sc-move", storage.ComponentMove{ParentName: strptr("sc-in-a")}, actorA, actorA, all); err != nil {
		t.Fatalf("reparent onto in-scope parent: %v", err)
	}
}
