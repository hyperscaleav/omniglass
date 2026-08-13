package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
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
	kioskA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(roomA.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("kiosk at room-a: %v", err)
	}
	kioskB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(roomB.Name)}, all, all, all, all)
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(room.Name)}, all, all, all, all); err != nil {
		t.Fatalf("first kiosk: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(room.Name)}, all, all, all, all); !errors.Is(err, storage.ErrComponentExistsInLocation) {
		t.Fatalf("second kiosk in same room = %v, want ErrComponentExistsInLocation", err)
	}
}

// TestSameNameUnplacedRefused covers the last of the four component/system
// buckets no other test's assertion is tight enough to pin:
// component_orphan_name_key and system_orphan_name_key, WHERE parent_id IS
// NULL AND location_id IS NULL. Two components (or systems) with neither a
// parent nor a location still collide.
func TestSameNameUnplacedRefused(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "spare"}, all, all, all, all); err != nil {
		t.Fatalf("first spare component: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "spare"}, all, all, all, all); !errors.Is(err, storage.ErrComponentExistsUnplaced) {
		t.Fatalf("second unplaced spare component = %v, want ErrComponentExistsUnplaced", err)
	}

	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "spare"}, all, all); err != nil {
		t.Fatalf("first spare system: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "spare"}, all, all); !errors.Is(err, storage.ErrSystemExistsUnplaced) {
		t.Fatalf("second unplaced spare system = %v, want ErrSystemExistsUnplaced", err)
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
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "campus-one", LocationType: "campus"}, all); !errors.Is(err, storage.ErrLocationExistsAtRoot) {
		t.Fatalf("second root with the same name = %v, want ErrLocationExistsAtRoot", err)
	}
}

// TestSubLocationScopesToParent covers the location_parent_name_key bucket
// (parent_id IS NOT NULL), the twin of TestRootLocationNamesGloballyUnique's
// root bucket: a nested location's name is unique among the children of ONE
// parent, not across the whole location tree. Two rooms named "room-1" under
// different buildings are both legal; two named "room-1" under the SAME
// building are not. Nothing else in the suite pins ErrLocationExistsUnderParent
// specifically (every existing collision assertion hits the root bucket
// instead), so this is the only test that would catch a fallthrough to the
// generic ErrLocationExists sentinel or a mismatched constraint name in
// mapLocationWriteErr's 23505 switch.
func TestSubLocationScopesToParent(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	bldgA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "bldg-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("bldg-a: %v", err)
	}
	bldgB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "bldg-b", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("bldg-b: %v", err)
	}
	roomA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-1", LocationType: "room", ParentName: strptr(bldgA.Name)}, all)
	if err != nil {
		t.Fatalf("room-1 under bldg-a: %v", err)
	}
	roomB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-1", LocationType: "room", ParentName: strptr(bldgB.Name)}, all)
	if err != nil {
		t.Fatalf("room-1 under bldg-b: %v", err)
	}
	if roomA.ID == roomB.ID {
		t.Fatal("the two room-1s did not actually land on different rows")
	}

	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-1", LocationType: "room", ParentName: strptr(bldgA.Name)}, all); !errors.Is(err, storage.ErrLocationExistsUnderParent) {
		t.Fatalf("second room-1 under bldg-a = %v, want ErrLocationExistsUnderParent", err)
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

	rackA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "rack-a"}, all, all, all, all)
	if err != nil {
		t.Fatalf("rack-a: %v", err)
	}
	rackB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "rack-b"}, all, all, all, all)
	if err != nil {
		t.Fatalf("rack-b: %v", err)
	}
	portA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "port-1", ParentName: strptr(rackA.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("port-1 under rack-a: %v", err)
	}
	portB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "port-1", ParentName: strptr(rackB.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("port-1 under rack-b (different parent, should be legal): %v", err)
	}
	if portA.ID == portB.ID {
		t.Fatal("the two port-1 components did not actually land on different rows")
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "port-1", ParentName: strptr(rackA.Name)}, all, all, all, all); !errors.Is(err, storage.ErrComponentExistsUnderParent) {
		t.Fatalf("second port-1 under rack-a (same parent) = %v, want ErrComponentExistsUnderParent", err)
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

	av, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av"}, all, all)
	if err != nil {
		t.Fatalf("av: %v", err)
	}
	lab, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "lab"}, all, all)
	if err != nil {
		t.Fatalf("lab: %v", err)
	}
	edgeUnderAV, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edge", ParentName: strptr(av.Name)}, all, all)
	if err != nil {
		t.Fatalf("edge under av: %v", err)
	}
	edgeUnderLab, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edge", ParentName: strptr(lab.Name)}, all, all)
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

	av, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av"}, all, all)
	if err != nil {
		t.Fatalf("av: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edge", ParentName: strptr(av.Name)}, all, all); err != nil {
		t.Fatalf("first edge under av: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "edge", ParentName: strptr(av.Name)}, all, all); !errors.Is(err, storage.ErrSystemExistsUnderParent) {
		t.Fatalf("second edge under the same parent = %v, want ErrSystemExistsUnderParent", err)
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
	zoneA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "zone-a"}, all, all, all, all)
	if err != nil {
		t.Fatalf("zone-a: %v", err)
	}
	inZoneA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", ParentName: strptr(zoneA.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("display-1 under zone-a: %v", err)
	}
	// An unrelated "display-1", entirely outside zone-a's subtree (root,
	// unplaced): the row that makes the bare name ambiguous estate-wide.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1"}, all, all, all, all); err != nil {
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

// TestOwnerScopedReadsResolveAmbiguousNameWithinScope extends the same ruling
// past GetComponent/GetSystem/GetLocation to the owner-arc reads, each of
// which resolves its reference TWICE: once via ownerInScope (the scope
// check) and, on success, a SECOND time via what used to be the scope-blind
// ownerArcValue (to bind the arc column for the actual query). Ordering the
// first resolve within scope is not enough on its own: unless the second
// resolve ALSO goes through the scoped variant (ownerArcValueInScope), a
// caller can pass the first check and then have the second one raise
// ErrAmbiguousName anyway, discovered by tracing exactly this path, not
// named in the review that asked for it: EffectiveProperties, EffectiveMetrics,
// LatestValue, SystemHealth, LocationHealth, EffectiveRoles, SetProperty,
// IssueCommand, and CommandSettlement all had this shape; this test drives
// every one of them.
func TestOwnerScopedReadsResolveAmbiguousNameWithinScope(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	// zoneA is the scope root (a system, since SystemHealth/EffectiveRoles
	// scope-check against the system tree); sysInZone is its child, named
	// "shared" (system_parent_name_key). An unrelated root system, also
	// named "shared" (system_orphan_name_key), is what makes the bare name
	// ambiguous estate-wide.
	zoneA, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "zone-a"}, all, all)
	if err != nil {
		t.Fatalf("zone-a: %v", err)
	}
	sysInZone, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "shared", ParentName: strptr(zoneA.Name)}, all, all)
	if err != nil {
		t.Fatalf("shared under zone-a: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "shared"}, all, all); err != nil {
		t.Fatalf("shared outside zone-a: %v", err)
	}
	readZoneA := scope.Set{IDs: []string{zoneA.ID}}

	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "note", DataType: "string"}); err != nil {
		t.Fatalf("create property type: %v", err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "load", DataType: "float"}); err != nil {
		t.Fatalf("create metric type: %v", err)
	}
	if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
		Name: "set-load", TargetMetricType: "load", SettleWindowSeconds: 0,
	}); err != nil {
		t.Fatalf("create command type: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	// IssueCommand's caused event needs a real actor uuid (the column is a
	// foreign key), so the bootstrapped owner is resolved by username rather
	// than passed as an empty string.
	actor, err := gw.ResolvePrincipalRef(ctx, "root")
	if err != nil {
		t.Fatalf("resolve actor: %v", err)
	}

	if _, err := gw.EffectiveProperties(ctx, "system", "shared", readZoneA); err != nil {
		t.Errorf("EffectiveProperties(shared) scoped to zone-a = %v, want ok", err)
	}
	if _, err := gw.EffectiveMetrics(ctx, "system", "shared", readZoneA); err != nil {
		t.Errorf("EffectiveMetrics(shared) scoped to zone-a = %v, want ok", err)
	}
	if _, err := gw.SystemHealth(ctx, "shared", 0, readZoneA); err != nil {
		t.Errorf("SystemHealth(shared) scoped to zone-a = %v, want ok", err)
	}
	if _, err := gw.EffectiveRoles(ctx, "shared", readZoneA); err != nil {
		t.Errorf("EffectiveRoles(shared) scoped to zone-a = %v, want ok", err)
	}
	if _, err := gw.SetProperty(ctx, "", "system", "shared", "note", "", []byte(`"hi"`), readZoneA); err != nil {
		t.Errorf("SetProperty(shared) scoped to zone-a = %v, want ok", err)
	}
	if _, err := gw.LatestValue(ctx, "system", "shared", "note", "", "declared", readZoneA); err != nil {
		t.Errorf("LatestValue(shared) scoped to zone-a = %v, want ok", err)
	}
	if _, err := gw.IssueCommand(ctx, actor, "system", "shared", "set-load", "", []byte(`5`), nil, readZoneA); err != nil {
		t.Errorf("IssueCommand(shared) scoped to zone-a = %v, want ok", err)
	}
	if _, err := gw.CommandSettlement(ctx, "system", "shared", "set-load", "", readZoneA); err != nil {
		t.Errorf("CommandSettlement(shared) scoped to zone-a = %v, want ok", err)
	}

	// And each one actually landed on sysInZone, not the other "shared": a
	// direct-by-id read confirms the write side, so a false pass (the read
	// resolving to the WRONG row while still reporting "ok") cannot hide here.
	got, err := gw.LatestValue(ctx, "system", sysInZone.ID, "note", "", "declared", all)
	if err != nil || got == nil || string(got.Value) != `"hi"` {
		t.Fatalf("LatestValue on sysInZone by id = %+v, %v, want the declared value written above", got, err)
	}

	// locationScope mirrors the system case for LocationHealth, which walks
	// the location tree instead.
	locZoneA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "loc-zone-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("loc-zone-a: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "loc-shared", LocationType: "room", ParentName: strptr(locZoneA.Name)}, all); err != nil {
		t.Fatalf("loc-shared under loc-zone-a: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "loc-shared", LocationType: "campus"}, all); err != nil {
		t.Fatalf("loc-shared outside loc-zone-a: %v", err)
	}
	readLocZoneA := scope.Set{IDs: []string{locZoneA.ID}}
	if _, err := gw.LocationHealth(ctx, "loc-shared", 0, readLocZoneA); err != nil {
		t.Errorf("LocationHealth(loc-shared) scoped to loc-zone-a = %v, want ok", err)
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

	campus, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "campus"}, all, all, all, all)
	if err != nil {
		t.Fatalf("campus: %v", err)
	}
	wingA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "wing-a", ParentName: strptr(campus.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("wing-a: %v", err)
	}
	wingB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "wing-b", ParentName: strptr(campus.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("wing-b: %v", err)
	}
	edgeA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "edge", ParentName: strptr(wingA.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("edge under wing-a: %v", err)
	}
	edgeB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "edge", ParentName: strptr(wingB.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("edge under wing-b: %v", err)
	}
	// A third "edge", unrelated to campus entirely: root, no parent.
	edgeOutside, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "edge"}, all, all, all, all)
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "kiosk", LocationName: strptr(roomA.Name)}, all, all, all, all); err != nil {
		t.Fatalf("kiosk at room-a: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av", LocationName: strptr(roomA.Name)}, all, all); err != nil {
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

// TestScopedCreateBindsCrossTierSystemAndLocation closes a critical a review
// caught in this task's own scope-ordering fix round: CreateComponent's
// system and location binds, UpdateComponent's location patch, and
// CreateSystem's/UpdateSystem's location binds all threaded the caller's OWN
// scope (resolved for "component" or "system") into a resolve against a
// DIFFERENT tier's table (system or location). inScopeTree walks the TARGET
// table's own ancestor chain, so a component-tier id can never appear in a
// location's ancestor chain: the bind denied every non-all caller, 100% of
// the time, not just a genuinely out-of-scope one. These binds are
// existence-only by design now (scope-blind, like the *NameTaken advisories),
// because no scope narrower than "all" ever meaningfully applies to a
// cross-tier reference from these call sites.
func TestScopedCreateBindsCrossTierSystemAndLocation(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	container, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "container"}, all, all, all, all)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "wing-sys"}, all, all)
	if err != nil {
		t.Fatalf("wing-sys: %v", err)
	}
	loc, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "wing-loc", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("wing-loc: %v", err)
	}
	loc2, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "wing-loc-2", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("wing-loc-2: %v", err)
	}

	// A deploy principal scoped to component:<container> only (the review's
	// own failure scenario): posting a child with a system AND a location
	// bind used to 422 unconditionally.
	compScope := scope.Set{IDs: []string{container.ID}}
	child, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "x", ParentName: strptr(container.Name),
		SystemName: strptr(sys.Name), LocationName: strptr(loc.Name),
	}, compScope, all, all, all)
	if err != nil {
		t.Fatalf("scoped create with cross-tier system+location bind = %v, want ok", err)
	}
	if child.LocationID == nil || *child.LocationID != loc.ID {
		t.Fatalf("child location = %v, want %s", child.LocationID, loc.ID)
	}

	// MoveComponent's location bind, same scope.
	updated, err := gw.MoveComponent(ctx, "", child.Name, storage.ComponentMove{LocationName: strptr(loc2.Name)}, compScope, compScope, all)
	if err != nil {
		t.Fatalf("scoped move with cross-tier location bind = %v, want ok", err)
	}
	if updated.LocationID == nil || *updated.LocationID != loc2.ID {
		t.Fatalf("updated component location = %v, want %s", updated.LocationID, loc2.ID)
	}

	// CreateSystem's location bind and MoveSystem's location bind, scoped
	// to a system-tier grant this time, to prove the fix is not accidentally
	// component-specific.
	rootSys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "root-sys"}, all, all)
	if err != nil {
		t.Fatalf("root-sys: %v", err)
	}
	sysScope := scope.Set{IDs: []string{rootSys.ID}}
	childSys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "child-sys", ParentName: strptr(rootSys.Name), LocationName: strptr(loc.Name),
	}, sysScope, all)
	if err != nil {
		t.Fatalf("scoped system create with cross-tier location bind = %v, want ok", err)
	}
	if childSys.LocationID == nil || *childSys.LocationID != loc.ID {
		t.Fatalf("child system location = %v, want %s", childSys.LocationID, loc.ID)
	}
	updatedSys, err := gw.MoveSystem(ctx, "", childSys.Name, storage.SystemMove{LocationName: strptr(loc2.Name)}, sysScope, sysScope, all)
	if err != nil {
		t.Fatalf("scoped system move with cross-tier location bind = %v, want ok", err)
	}
	if updatedSys.LocationID == nil || *updatedSys.LocationID != loc2.ID {
		t.Fatalf("updated system location = %v, want %s", updatedSys.LocationID, loc2.ID)
	}
}

// TestResolveTagsSystemBandSurvivesScopedCaller closes Critical B: forSystem
// used to be checked against read, which ResolveTags resolves for
// "component" (the inScopeTree check on componentID just above it), not
// "system". A component-tier id can never appear in a system's own ancestor
// chain, so every non-all caller's ?system= filter silently fell into the
// "named a system with no binding here" empty-band branch regardless of
// whether the system genuinely had one, a wrong answer with no error, not a
// refusal. A scoped principal that can read the component must still see the
// system band its own tags cascade includes.
func TestResolveTagsSystemBandSurvivesScopedCaller(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "note", Propagates: true}, all); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "cascade-sys"}, all, all)
	if err != nil {
		t.Fatalf("cascade-sys: %v", err)
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "cascade-comp"}, all, all, all, all)
	if err != nil {
		t.Fatalf("cascade-comp: %v", err)
	}
	if err := gw.AddMember(ctx, "", sys.Name, comp.Name, all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "note", "system", strptr(sys.ID), "from-system", all, all); err != nil {
		t.Fatalf("bind system tag: %v", err)
	}

	// A viewer scoped only to component:<comp> (read-only, no system grant at
	// all) still resolves the component's effective tags including the
	// system band its own membership cascades from.
	compScope := scope.Set{IDs: []string{comp.ID}}
	resolved, err := gw.ResolveTags(ctx, comp.ID, sys.Name, compScope)
	if err != nil {
		t.Fatalf("resolve tags scoped to component = %v, want ok", err)
	}
	found := false
	for _, rt := range resolved {
		if rt.Key == "note" && rt.OwnerKind == "system" && rt.Value == "from-system" {
			found = true
		}
	}
	if !found {
		t.Fatalf("resolved tags = %+v, want the system band's note=from-system present", resolved)
	}
}

// TestAssignRoleComponentAmbiguityNeverLeaksCandidates closes the remaining
// C1/C2 shape a review found in AssignRole's and resolveMembershipEnds's
// (AddMember's) component end: the only scope either holds is write (or
// read), resolved for "system", which can never narrow a component resolve
// (the same tier-mismatch AS Critical A, just on the read side of ambiguity
// rather than the deny side, since scopedByName here is deliberately
// scope-blind and existence-only, not threaded at all). An ambiguous
// component name here is a real refusal a caller can hit, and
// withoutCandidates is what keeps the resulting 409 from naming a component
// uuid a system:update-only principal, with no component:read grant of any
// kind, holds no right to see.
//
// UnassignRole no longer shares this shape (#627 review round 3, closing
// #645): it used to be code-identical to resolveMembershipEnds (the same
// scopedByName-plus-withoutCandidates call), which is why a prior round
// drove it through the same estate-wide-ambiguous fixture below and got the
// same ErrAmbiguousName. It now resolves the component within THIS role's
// current occupants instead, so an estate-wide-ambiguous name that occupies
// nothing here is ErrAssignmentMissing, a cleaner and more honest answer
// than a 409 over a component that was never actually a candidate for this
// call. TestUnassignRoleResolvesEstateWideDuplicateWithinOccupancy and
// TestUnassignRoleStillRefusesWhenBothDuplicatesOccupyTheSameRole below
// cover UnassignRole's own new shape; this test keeps proving AssignRole and
// AddMember, which did not change this round.
func TestAssignRoleComponentAmbiguityNeverLeaksCandidates(t *testing.T) {
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
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "role-sys"}, all, all)
	if err != nil {
		t.Fatalf("role-sys: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into system_role (owner_kind, system_id, name, display_name)
		values ('system', $1, 'seat', 'Seat')`, sys.ID); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	holder, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "holder"}, all, all, all, all)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-seat"}, all, all, all, all); err != nil {
		t.Fatalf("root dup-seat: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-seat", ParentName: strptr(holder.Name)}, all, all, all, all); err != nil {
		t.Fatalf("nested dup-seat: %v", err)
	}

	sysScope := scope.Set{IDs: []string{sys.ID}}
	var ambig *storage.ErrAmbiguousName

	err = gw.AssignRole(ctx, "", sys.Name, "seat", "dup-seat", sysScope)
	if !errors.As(err, &ambig) {
		t.Fatalf("assign role with ambiguous component = %v, want *ErrAmbiguousName", err)
	}
	if len(ambig.Candidates) != 0 {
		t.Fatalf("assign role ambiguous component candidates = %v, want none (system:update holds no component:read)", ambig.Candidates)
	}

	// UnassignRole resolves within the role's own occupants now (#645):
	// "dup-seat" is estate-wide ambiguous but occupies nothing here (no
	// assignment exists), so this is the honest, more specific
	// ErrAssignmentMissing, not the ambiguity 409 it used to raise over a
	// component that was never actually a candidate for this call.
	err = gw.UnassignRole(ctx, "", sys.Name, "seat", "dup-seat", sysScope)
	if !errors.Is(err, storage.ErrAssignmentMissing) {
		t.Fatalf("unassign role with an unassigned, estate-wide-ambiguous component = %v, want ErrAssignmentMissing", err)
	}

	// AddMember drives resolveMembershipEnds directly: no role or assignment
	// involved, just the (system, component) membership resolve.
	ambig = nil
	err = gw.AddMember(ctx, "", sys.Name, "dup-seat", sysScope)
	if !errors.As(err, &ambig) {
		t.Fatalf("add member with ambiguous component = %v, want *ErrAmbiguousName", err)
	}
	if len(ambig.Candidates) != 0 {
		t.Fatalf("add member ambiguous component candidates = %v, want none (system:update holds no component:read)", ambig.Candidates)
	}
}

// TestUnassignRoleResolvesEstateWideDuplicateWithinOccupancy is the epic's
// headline promise, closed for the last remaining write (#627 review round
// 3, #645): an operator who can staff a role with a duplicate-named
// component must also be able to unstaff it, not get in with no way back
// out. "dup-seat" is ambiguous estate-wide (two components, two placements),
// but only the nested one occupies "seat" here, so UnassignRole resolves it
// from the bare name alone, exactly the string the wire's own AssignedTo
// gives a caller (internal/api's EffectiveRoleBody.AssignedTo, names only).
func TestUnassignRoleResolvesEstateWideDuplicateWithinOccupancy(t *testing.T) {
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
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	all := scope.Set{All: true}

	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "unassign-sys"}, all, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into system_role (owner_kind, system_id, name, display_name)
		values ('system', $1, 'seat', 'Seat')`, sys.ID); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	holder, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "holder"}, all, all, all, all)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-seat"}, all, all, all, all); err != nil {
		t.Fatalf("root dup-seat: %v", err)
	}
	nested, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-seat", ParentName: strptr(holder.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("nested dup-seat: %v", err)
	}

	sysScope := scope.Set{IDs: []string{sys.ID}}
	// Staffed by uuid, exactly how the console addresses AssignRole's own
	// component end today (round 3's assign/add fix): AssignRole itself did
	// not change, only what a caller supplies, and a uuid never ambiguous.
	if err := gw.AssignRole(ctx, "", sys.Name, "seat", nested.ID, sysScope); err != nil {
		t.Fatalf("assign nested dup-seat: %v", err)
	}
	roles, err := gw.EffectiveRoles(ctx, sys.Name, sysScope)
	if err != nil {
		t.Fatalf("effective roles: %v", err)
	}
	if len(roles) != 1 || len(roles[0].AssignedTo) != 1 {
		t.Fatalf("roles after assign = %+v, want one role with one occupant", roles)
	}

	// Unassigned by the bare, estate-wide-ambiguous name: the wire has no
	// uuid to give here (AssignedTo is names only), so this is the case the
	// fix exists for.
	if err := gw.UnassignRole(ctx, "", sys.Name, "seat", "dup-seat", sysScope); err != nil {
		t.Fatalf("unassign estate-wide-ambiguous but uniquely-occupying component = %v, want ok", err)
	}
	roles, err = gw.EffectiveRoles(ctx, sys.Name, sysScope)
	if err != nil {
		t.Fatalf("effective roles after unassign: %v", err)
	}
	if len(roles) != 1 || len(roles[0].AssignedTo) != 0 {
		t.Fatalf("roles after unassign = %+v, want the role vacated", roles)
	}
}

// TestUnassignRoleStillRefusesWhenBothDuplicatesOccupyTheSameRole is the
// edge case the occupancy-scoped resolve must not paper over: a role's
// capacity is unbounded by default, so nothing stops two DIFFERENT
// same-named components (different placements) from both occupying the same
// role at once. When that happens, "the name is unique within this role's
// occupants" no longer holds, and the fix must still refuse ambiguously
// rather than silently picking whichever row sorted first.
func TestUnassignRoleStillRefusesWhenBothDuplicatesOccupyTheSameRole(t *testing.T) {
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
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	all := scope.Set{All: true}

	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "both-occupy-sys"}, all, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into system_role (owner_kind, system_id, name, display_name)
		values ('system', $1, 'seat', 'Seat')`, sys.ID); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	holder, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "holder"}, all, all, all, all)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	root, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-seat"}, all, all, all, all)
	if err != nil {
		t.Fatalf("root dup-seat: %v", err)
	}
	nested, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-seat", ParentName: strptr(holder.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("nested dup-seat: %v", err)
	}

	sysScope := scope.Set{IDs: []string{sys.ID}}
	if err := gw.AssignRole(ctx, "", sys.Name, "seat", root.ID, sysScope); err != nil {
		t.Fatalf("assign root dup-seat: %v", err)
	}
	if err := gw.AssignRole(ctx, "", sys.Name, "seat", nested.ID, sysScope); err != nil {
		t.Fatalf("assign nested dup-seat: %v", err)
	}

	var ambig *storage.ErrAmbiguousName
	err = gw.UnassignRole(ctx, "", sys.Name, "seat", "dup-seat", sysScope)
	if !errors.As(err, &ambig) {
		t.Fatalf("unassign with both duplicates occupying the role = %v, want *ErrAmbiguousName", err)
	}
	if len(ambig.Candidates) != 0 {
		t.Fatalf("unassign ambiguous-within-occupancy candidates = %v, want none (system:update holds no component:read)", ambig.Candidates)
	}
}

// TestRemoveMemberResolvesEstateWideDuplicateWithinMembership is
// TestUnassignRoleResolvesEstateWideDuplicateWithinOccupancy's membership
// counterpart (#627 review round 3, #645): a duplicate-named component that
// is a member of this system removes cleanly by its bare name, the only
// form SystemMemberBody.component gives a caller (internal/api/members.go),
// even though the same name is ambiguous estate-wide.
func TestRemoveMemberResolvesEstateWideDuplicateWithinMembership(t *testing.T) {
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
	all := scope.Set{All: true}

	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "remove-sys"}, all, all)
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	holder, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "holder"}, all, all, all, all)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-member"}, all, all, all, all); err != nil {
		t.Fatalf("root dup-member: %v", err)
	}
	nested, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-member", ParentName: strptr(holder.Name)}, all, all, all, all)
	if err != nil {
		t.Fatalf("nested dup-member: %v", err)
	}

	sysScope := scope.Set{IDs: []string{sys.ID}}
	// Added by uuid, exactly how the console addresses AddMember's own
	// component end today (round 3's assign/add fix): AddMember itself did
	// not change, only what a caller supplies, and a uuid never ambiguous.
	if err := gw.AddMember(ctx, "", sys.Name, nested.ID, sysScope); err != nil {
		t.Fatalf("add nested dup-member: %v", err)
	}
	members, err := gw.ListMembers(ctx, sys.Name, sysScope)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members after add = %+v, want one", members)
	}

	// Removed by the bare, estate-wide-ambiguous name: the wire has no uuid
	// to give here (SystemMemberBody.component is a name), so this is the
	// case the fix exists for.
	if err := gw.RemoveMember(ctx, "", sys.Name, "dup-member", sysScope); err != nil {
		t.Fatalf("remove estate-wide-ambiguous but uniquely-member component = %v, want ok", err)
	}
	members, err = gw.ListMembers(ctx, sys.Name, sysScope)
	if err != nil {
		t.Fatalf("list members after remove: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("members after remove = %+v, want none", members)
	}
}

// TestAPlacementBindNamesTheCandidatesItMatched is #697: the other side of the
// policy the three tests above pin. A bind that resolves with NO caller scope
// keeps its redaction (they prove it); a bind that resolves INSIDE the caller's
// own read scope has nothing left to redact, because #700 made every candidate a
// row the caller already proved it may read, and an ambiguity error that names
// nothing tells an operator their input is ambiguous while handing them nothing
// to disambiguate with.
//
// The fixture is the shape the issue was filed about: every building's first
// floor may be named "1" (a positional name is allocated per placement bucket,
// and the bucket is the placement), so a cross-tier bind by the bare name "1"
// matches one row per building.
//
// The candidates are uuids rather than names because a uuid is the one spelling
// resolveRef is guaranteed to narrow to a single row; the same reference accepts
// a dotted address too (loadByRef parses one), which is what makes the listed
// answer actionable rather than only informative.
func TestAPlacementBindNamesTheCandidatesItMatched(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	for _, campus := range []string{"bldg-a", "bldg-b"} {
		if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: campus, LocationType: "campus"}, all); err != nil {
			t.Fatalf("create %s: %v", campus, err)
		}
	}
	first := make(map[string]string, 2)
	for _, campus := range []string{"bldg-a", "bldg-b"} {
		floor, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
			Name: "1", LocationType: "floor", ParentName: strptr(campus),
		}, all)
		if err != nil {
			t.Fatalf("create %s floor 1: %v", campus, err)
		}
		first[campus] = floor.ID
	}

	_, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel", LocationName: strptr("1"),
	}, all, all, all, all)
	var ambig *storage.ErrAmbiguousName
	if !errors.As(err, &ambig) {
		t.Fatalf("create against the ambiguous floor name = %v, want *ErrAmbiguousName", err)
	}
	want := map[string]bool{first["bldg-a"]: true, first["bldg-b"]: true}
	if len(ambig.Candidates) != len(want) {
		t.Fatalf("candidates = %v, want the two floors named 1 (%v)", ambig.Candidates, want)
	}
	for _, c := range ambig.Candidates {
		if !want[c] {
			t.Fatalf("candidates = %v, want exactly the two floors named 1 (%v)", ambig.Candidates, want)
		}
	}
	// The list has to reach the operator, not just the struct: the message is
	// what a caller with no typed access to the error ever sees.
	for _, id := range ambig.Candidates {
		if !strings.Contains(ambig.Error(), id) {
			t.Fatalf("error message %q does not name candidate %s", ambig.Error(), id)
		}
	}
}

// TestAPlacementBindStillFoldsAPathMiss guards withoutCandidates' OTHER job,
// which #697 splits away from the redaction rather than deleting: a dotted
// placement reference that fails to resolve structurally must leave the gateway
// as the bare sentinel, or mapRefErr's blanket *ErrPathNotFound case turns a
// create's missing location into a 404 before the entity's own mapper can call
// it the 422 it is. path_body_ref_test.go drives the same fold through every
// other call site; this one pins it on the seam the redaction just left.
func TestAPlacementBindStillFoldsAPathMiss(t *testing.T) {
	gw := openGateway(t)
	ctx := context.Background()

	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create hq: %v", err)
	}
	_, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "panel", LocationName: strptr("hq.nope"),
	}, all, all, all, all)
	if !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("create against a structural path miss = %v, want ErrLocationNotFound", err)
	}
	var leaked *storage.ErrPathNotFound
	if errors.As(err, &leaked) {
		t.Fatalf("error still carries *storage.ErrPathNotFound (%v): mapRefErr would answer 404 instead of the create's 422", err)
	}
}
