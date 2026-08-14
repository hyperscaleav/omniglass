package storage_test

import (
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A node binds a placement it can READ (#705), the guard #700 put on the
// component and the system create and move paths, on the one caller-supplied
// reference those two left behind.
//
// The node tier is the older shape and the quieter one: no node label rule
// exists, so a create cannot stamp and hand back a label built from a location
// the caller cannot see, which is the disclosure that made #700 urgent. What is
// left is the plain invariant this repository states as an invariant rather than
// a preference, that ABAC scope is injected by the Storage Gateway on every
// applicable query, and a reference resolved scope-blind is an exception to it
// rather than a considered carve-out.
//
// This drives the gateway seam rather than the wire, because the wire cannot
// reach it: node:create resolves only from an "all" grant (applicableKinds
// admits no tier for a node), and every seeded role carrying node:* inherits the
// viewer read floor, so a principal that can create a node today also reads
// every location. The seam is where the two sets are separate arguments, so it
// is where a narrow placement read is expressible at all, and it is where the
// invariant is written. A role with node:create and no location read floor is an
// ordinary custom role, so this is a shape the platform admits, not one it
// forbids.

// TestANodeBindsOnlyALocationTheCallerCanRead is the create and the update
// halves of #705, with the owner running the identical body in the same test:
// an assertion that a narrow caller is refused proves nothing on its own, since
// a broken path refuses everyone.
func TestANodeBindsOnlyALocationTheCallerCanRead(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ctx := seededGateway(t)
	closet := makeRoomIn(t, gw, ctx, "node-closet")
	vault := makeRoomIn(t, gw, ctx, "node-vault")
	// The caller reads one room of the two. Its node scope is estate-wide,
	// because a node create requires that and this test is about the OTHER set.
	narrow := scope.Set{IDs: []string{closet.ID}}

	// Inside its own read scope the bind works exactly as it did.
	n, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-in", LocationName: &closet.Name}, all, narrow)
	if err != nil {
		t.Fatalf("create naming a location the caller reads: %v", err)
	}
	if n.LocationName == nil || *n.LocationName != closet.Name {
		t.Fatalf("in-scope create bound %v, want %q", n.LocationName, closet.Name)
	}

	// The room it cannot read is refused, and the refusal is the SAME error a
	// location that does not exist at all gives. Anything that separated the two
	// would confirm the room exists to a caller with no grant to learn it, which
	// is the disclosure the non-disclosing not-found exists to prevent.
	_, outOfScope := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-out", LocationName: &vault.Name}, all, narrow)
	if !errors.Is(outOfScope, storage.ErrLocationNotFound) {
		t.Fatalf("create naming a location outside the read scope: got %v, want ErrLocationNotFound", outOfScope)
	}
	_, absent := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-ghost", LocationName: strptr("ghost-room")}, all, narrow)
	if !errors.Is(absent, storage.ErrLocationNotFound) {
		t.Fatalf("create naming a location that does not exist: got %v, want ErrLocationNotFound", absent)
	}
	if outOfScope.Error() != absent.Error() {
		t.Errorf("out of scope reads as %q and absent as %q: a refusal that tells them apart discloses that the row exists",
			outOfScope, absent)
	}

	// The owner runs the identical body and is served, so the refusal above is a
	// scope boundary and not a broken write path.
	owned, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-out", LocationName: &vault.Name}, all, all)
	if err != nil {
		t.Fatalf("owner create naming the same location: %v", err)
	}
	if owned.LocationName == nil || *owned.LocationName != vault.Name {
		t.Fatalf("owner create bound %v, want %q: the refusal above is not a scope refusal", owned.LocationName, vault.Name)
	}

	// The update is the same sentence on the other verb: a relocate names a
	// destination the same way a create names a placement.
	if _, err := gw.UpdateNode(ctx, "", "edge-in", storage.NodePatch{LocationName: &vault.Name}, all, all, narrow); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("update naming a location outside the read scope: got %v, want ErrLocationNotFound", err)
	}
	still, err := gw.GetNode(ctx, "edge-in", all)
	if err != nil {
		t.Fatalf("read the node back: %v", err)
	}
	if still.LocationName == nil || *still.LocationName != closet.Name {
		t.Errorf("placement after the refused update = %v, want unchanged %q", still.LocationName, closet.Name)
	}
	moved, err := gw.UpdateNode(ctx, "", "edge-in", storage.NodePatch{LocationName: &vault.Name}, all, all, all)
	if err != nil {
		t.Fatalf("owner update naming the same location: %v", err)
	}
	if moved.LocationName == nil || *moved.LocationName != vault.Name {
		t.Fatalf("owner update bound %v, want %q: the refusal above is not a scope refusal", moved.LocationName, vault.Name)
	}

	// A caller with no location read at all binds nothing, and still writes the
	// node itself: the placement is the only thing the second set decides.
	blind, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-unplaced"}, all, scope.Set{})
	if err != nil {
		t.Fatalf("create with no placement under an empty location read: %v", err)
	}
	if blind.LocationName != nil {
		t.Fatalf("unplaced create bound %v, want nothing", blind.LocationName)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-blind", LocationName: &closet.Name}, all, scope.Set{}); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("create naming a location under an empty location read: got %v, want ErrLocationNotFound", err)
	}
}
