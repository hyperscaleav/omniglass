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

// openGateway is the shared open-and-seed boilerplate every test in this file
// needs: a fresh testcontainer Postgres, a gateway on it, and the boot seed
// (the generic-device product a component create falls back to, and the
// location/system/component_type registries).
func openGateway(t *testing.T) storage.Gateway {
	t.Helper()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw
}

// TestSameNameTwoRooms is the core positive case #627 exists to legalize: two
// components placed directly at two different rooms (no parent) may share a
// name, because component_location_name_key is keyed on (location_id, name),
// not name alone.
func TestSameNameTwoRooms(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	roomA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-a: %v", err)
	}
	roomB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-b", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-b: %v", err)
	}
	kioskA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(roomA.Name)}, all)
	if err != nil {
		t.Fatalf("kiosk at room-a: %v", err)
	}
	kioskB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(roomB.Name)}, all)
	if err != nil {
		t.Fatalf("kiosk at room-b: %v", err)
	}
	if kioskA.ID == kioskB.ID {
		t.Fatal("the two kiosks did not actually land on different rows")
	}
	// Each resolves by its own uuid to its own row.
	gotA, err := gw.GetComponent(ctx, kioskA.ID, all)
	if err != nil || gotA.LocationID == nil || *gotA.LocationID != roomA.ID {
		t.Fatalf("get kioskA by id = %+v, err %v, want located at room-a", gotA, err)
	}
	gotB, err := gw.GetComponent(ctx, kioskB.ID, all)
	if err != nil || gotB.LocationID == nil || *gotB.LocationID != roomB.ID {
		t.Fatalf("get kioskB by id = %+v, err %v, want located at room-b", gotB, err)
	}
}

// TestSameNameSameRoomRefused is TestSameNameTwoRooms' negative twin: two
// components placed at the SAME room may not share a name, because they land
// in the identical (location_id, name) bucket.
func TestSameNameSameRoomRefused(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	room, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(room.Name)}, all); err != nil {
		t.Fatalf("first kiosk: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(room.Name)}, all); !errors.Is(err, storage.ErrComponentExists) {
		t.Fatalf("second kiosk in same room = %v, want ErrComponentExists", err)
	}
}

// TestRootLocationNamesGloballyUnique proves a root location's name stays
// effectively global: location has only one root bucket
// (location_root_name_key, WHERE parent_id IS NULL), so two roots can never
// share a name no matter how unrelated they are, unlike a nested location
// under two different parents.
func TestRootLocationNamesGloballyUnique(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "campus-one", LocationType: "campus"}, all); err != nil {
		t.Fatalf("first root: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "campus-one", LocationType: "campus"}, all); !errors.Is(err, storage.ErrLocationExists) {
		t.Fatalf("second root with the same name = %v, want ErrLocationExists", err)
	}
}

// TestSubComponentScopesToParent covers the component_parent_name_key bucket
// directly (parent_id IS NOT NULL): a name is unique among the children of ONE
// parent, not across the whole component tree. Two components named "port-1"
// under different parents are both legal; two named "port-1" under the SAME
// parent are not.
func TestSubComponentScopesToParent(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	rackA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "rack-a"}, all)
	if err != nil {
		t.Fatalf("rack-a: %v", err)
	}
	rackB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "rack-b"}, all)
	if err != nil {
		t.Fatalf("rack-b: %v", err)
	}
	portA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "port-1", ParentName: strptr(rackA.Name)}, all)
	if err != nil {
		t.Fatalf("port-1 under rack-a: %v", err)
	}
	portB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "port-1", ParentName: strptr(rackB.Name)}, all)
	if err != nil {
		t.Fatalf("port-1 under rack-b (different parent, should be legal): %v", err)
	}
	if portA.ID == portB.ID {
		t.Fatal("the two port-1 components did not actually land on different rows")
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "port-1", ParentName: strptr(rackA.Name)}, all); !errors.Is(err, storage.ErrComponentExists) {
		t.Fatalf("second port-1 under rack-a (same parent) = %v, want ErrComponentExists", err)
	}
}

// TestSiblingSubsystemsUnderDifferentParentsBothLegal and
// TestSiblingSubsystemsSameParentRefused cover the system tree's identical
// three-bucket shape, called out separately from the component case because
// system needed a third bucket for a structural reason component shares but a
// two-bucket design (as the epic's plan first had it) would miss: CreateSystem
// never inherits a parent's location_id, so two subsystems named the same
// thing under two DIFFERENT root systems would both fall into the orphan
// bucket and collide if the parent bucket did not exist.
func TestSiblingSubsystemsUnderDifferentParentsBothLegal(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	av, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av"}, all)
	if err != nil {
		t.Fatalf("av: %v", err)
	}
	lab, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "lab"}, all)
	if err != nil {
		t.Fatalf("lab: %v", err)
	}
	edgeUnderAV, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edge", ParentName: strptr(av.Name)}, all)
	if err != nil {
		t.Fatalf("edge under av: %v", err)
	}
	edgeUnderLab, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edge", ParentName: strptr(lab.Name)}, all)
	if err != nil {
		t.Fatalf("edge under lab (different parent, should be legal): %v", err)
	}
	if edgeUnderAV.ID == edgeUnderLab.ID {
		t.Fatal("the two edge systems did not actually land on different rows")
	}
}

func TestSiblingSubsystemsSameParentRefused(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	av, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av"}, all)
	if err != nil {
		t.Fatalf("av: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edge", ParentName: strptr(av.Name)}, all); err != nil {
		t.Fatalf("first edge under av: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edge", ParentName: strptr(av.Name)}, all); !errors.Is(err, storage.ErrSystemExists) {
		t.Fatalf("second edge under the same parent = %v, want ErrSystemExists", err)
	}
}

// TestNodeNameStaysGlobal pins the one tree-shaped-looking name that #627
// deliberately leaves alone: a node's name is also its NATS subject, a wire
// identity outside any placement, so node_name_key stays a plain global
// UNIQUE(name) rather than gaining placement buckets.
func TestNodeNameStaysGlobal(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-node"}, all); err != nil {
		t.Fatalf("first node: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-node"}, all); !errors.Is(err, storage.ErrNodeExists) {
		t.Fatalf("second node with the same name = %v, want ErrNodeExists", err)
	}
}

// TestNameAmbiguousGloballyButUniqueToCallerResolvesCleanly is the first half
// of the architect ruling "scope decides before ambiguity does": scope narrows
// the candidate set BEFORE ambiguity is decided, so a name that collides
// estate-wide but not within the caller's own scope is not refused. Without
// that ordering, a caller scoped to room-b would be refused for a name unique
// in their own room, solely because room-a (which they cannot even read) also
// holds one, and the old behavior (resolve globally, then scope-check the
// single resolved row) could not do better: scopedByName picked whichever row
// sorted first and would have raised ErrAmbiguousName before scope ever ran.
func TestNameAmbiguousGloballyButUniqueToCallerResolvesCleanly(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	// GetComponent's read scope walks the COMPONENT tree (inScopeTree on
	// componentTable), so the subtree root has to be a component id, not a
	// location id: zone-a is that root, and "display-1" is its child
	// (component_parent_name_key), inside zone-a's own subtree.
	zoneA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "zone-a"}, all)
	if err != nil {
		t.Fatalf("zone-a: %v", err)
	}
	inZoneA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", ParentName: strptr(zoneA.Name)}, all)
	if err != nil {
		t.Fatalf("display-1 under zone-a: %v", err)
	}
	// An unrelated "display-1", entirely outside zone-a's subtree (root,
	// unplaced): the row that makes the bare name ambiguous estate-wide.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1"}, all); err != nil {
		t.Fatalf("display-1 outside zone-a: %v", err)
	}

	// A caller scoped ONLY to zone-a's subtree: "display-1" is ambiguous
	// estate-wide (two rows), but unique within this caller's own read
	// scope, so it must resolve cleanly to zone-a's row, not refuse.
	readZoneA := scope.Set{IDs: []string{zoneA.ID}}
	got, err := gw.GetComponent(ctx, "display-1", readZoneA)
	if err != nil {
		t.Fatalf("get display-1 scoped to zone-a = %v, want ok (unique within this caller's scope)", err)
	}
	if got.ID != inZoneA.ID {
		t.Fatalf("get display-1 scoped to zone-a resolved to %s, want %s", got.ID, inZoneA.ID)
	}
}

// TestAmbiguousNameCandidatesNeverLeakOutOfScopeRow is the second half of the
// architect ruling: when a name IS ambiguous within the caller's own scope,
// the resulting ErrAmbiguousName's Candidates list must name only the rows the
// caller can read, never a same-named row sitting outside their scope. That
// list is a 409 body an operator reads; naming a uuid the caller cannot
// otherwise reach is the same disclosure the non-disclosing 404 exists to
// prevent elsewhere.
//
// campus is a root scoped subtree containing two DIFFERENT branches (wing-a,
// wing-b), each with its own "edge" component: two rows sharing a name is
// legal here because they sit under different parents
// (component_parent_name_key), and BOTH are within the campus scope, so this
// is a genuine in-scope collision, not the clean-resolve case above. A third
// "edge", entirely outside campus's subtree, proves the out-of-scope row never
// appears in Candidates.
func TestAmbiguousNameCandidatesNeverLeakOutOfScopeRow(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	campus, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "campus"}, all)
	if err != nil {
		t.Fatalf("campus: %v", err)
	}
	wingA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "wing-a", ParentName: strptr(campus.Name)}, all)
	if err != nil {
		t.Fatalf("wing-a: %v", err)
	}
	wingB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "wing-b", ParentName: strptr(campus.Name)}, all)
	if err != nil {
		t.Fatalf("wing-b: %v", err)
	}
	edgeA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "edge", ParentName: strptr(wingA.Name)}, all)
	if err != nil {
		t.Fatalf("edge under wing-a: %v", err)
	}
	edgeB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "edge", ParentName: strptr(wingB.Name)}, all)
	if err != nil {
		t.Fatalf("edge under wing-b: %v", err)
	}
	// A third "edge", unrelated to campus entirely: root, no parent.
	edgeOutside, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "edge"}, all)
	if err != nil {
		t.Fatalf("edge outside campus: %v", err)
	}

	readCampus := scope.Set{IDs: []string{campus.ID}}
	_, err = gw.GetComponent(ctx, "edge", readCampus)
	var ambig *storage.ErrAmbiguousName
	if !errors.As(err, &ambig) {
		t.Fatalf("get edge scoped to campus = %v, want *storage.ErrAmbiguousName (two in-scope rows share the name)", err)
	}
	if len(ambig.Candidates) != 2 {
		t.Fatalf("candidates = %v, want exactly 2 (edgeA and edgeB, not edgeOutside)", ambig.Candidates)
	}
	got := map[string]bool{ambig.Candidates[0]: true, ambig.Candidates[1]: true}
	if !got[edgeA.ID] || !got[edgeB.ID] {
		t.Fatalf("candidates = %v, want %s and %s", ambig.Candidates, edgeA.ID, edgeB.ID)
	}
	if got[edgeOutside.ID] {
		t.Fatalf("candidates = %v leaked the out-of-scope row %s", ambig.Candidates, edgeOutside.ID)
	}
}

// TestCheckNameIsScopedToPlacement drives ComponentNameTaken, SystemNameTaken,
// and LocationNameTaken directly: each now reports availability against the
// SAME bucket a create would actually land in, not a global fact (#627). A
// name taken in one room is reported free at a different one, and taken
// wherever it collides with the same bucket.
func TestCheckNameIsScopedToPlacement(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	roomA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-a: %v", err)
	}
	roomB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-b", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-b: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(roomA.Name)}, all); err != nil {
		t.Fatalf("kiosk at room-a: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av", LocationName: strptr(roomA.Name)}, all); err != nil {
		t.Fatalf("av at room-a: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "sub-room", LocationType: "room", ParentName: strptr(roomA.Name)}, all); err != nil {
		t.Fatalf("sub-room under room-a: %v", err)
	}

	// Component: taken at room-a, free at room-b, free unplaced.
	if taken, err := gw.ComponentNameTaken(ctx, "kiosk", nil, strptr(roomA.Name)); err != nil || !taken {
		t.Fatalf("kiosk taken at room-a = %v, %v, want true, nil", taken, err)
	}
	if taken, err := gw.ComponentNameTaken(ctx, "kiosk", nil, strptr(roomB.Name)); err != nil || taken {
		t.Fatalf("kiosk taken at room-b = %v, %v, want false, nil", taken, err)
	}
	if taken, err := gw.ComponentNameTaken(ctx, "kiosk", nil, nil); err != nil || taken {
		t.Fatalf("kiosk taken unplaced = %v, %v, want false, nil", taken, err)
	}

	// System: same shape.
	if taken, err := gw.SystemNameTaken(ctx, "av", nil, strptr(roomA.Name)); err != nil || !taken {
		t.Fatalf("av taken at room-a = %v, %v, want true, nil", taken, err)
	}
	if taken, err := gw.SystemNameTaken(ctx, "av", nil, strptr(roomB.Name)); err != nil || taken {
		t.Fatalf("av taken at room-b = %v, %v, want false, nil", taken, err)
	}

	// Location: taken under room-a, free at root (room-a itself is only taken
	// at root, not under a parent).
	if taken, err := gw.LocationNameTaken(ctx, "sub-room", strptr(roomA.Name)); err != nil || !taken {
		t.Fatalf("sub-room taken under room-a = %v, %v, want true, nil", taken, err)
	}
	if taken, err := gw.LocationNameTaken(ctx, "sub-room", nil); err != nil || taken {
		t.Fatalf("sub-room taken at root = %v, %v, want false, nil", taken, err)
	}
	if taken, err := gw.LocationNameTaken(ctx, "room-a", nil); err != nil || !taken {
		t.Fatalf("room-a taken at root = %v, %v, want true, nil", taken, err)
	}
}
