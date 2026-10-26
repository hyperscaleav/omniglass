package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestRegistryRenameThroughTheGateway is the capability ADR-0062 promised and
// #555 found missing: a registry handle can be renamed through the platform,
// not only by raw SQL (TestRegistryHandleRenameKeepsReferences proves the
// foreign keys follow; this proves there is a door). Each of the five
// registries renames by handle or uuid, keeps its uuid, answers to the new
// name and not the old, and every referencing row reads the new handle. An
// official row keeps its name (the boot seed owns it by name), a taken name is
// a conflict, an illegal or uuid-shaped one is refused, and each rename writes
// one audit row keyed on the uuid, in the same transaction.
func TestRegistryRenameThroughTheGateway(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn,
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	audits := func(resource, id string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from audit_log where verb = 'rename' and resource = $1 and resource_id = $2`,
			resource, id).Scan(&n); err != nil {
			t.Fatalf("count audit rows: %v", err)
		}
		return n
	}

	// One custom row per registry, each referenced the way the fleet references it.
	vendor, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: "acme", Label: "Acme", Kind: "manufacturer"})
	if err != nil {
		t.Fatalf("vendor: %v", err)
	}
	driver, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "acme-ctl", Label: "Acme Controller"})
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	product, err := gw.CreateProduct(ctx, "", storage.Product{Name: "acme-bar", Label: "Acme Bar", Kind: "device",
		ComponentType: "video-bar", VendorID: strptr("acme"), DriverID: strptr("acme-ctl")})
	if err != nil {
		t.Fatalf("product: %v", err)
	}
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "acme-bar-mini", Label: "Acme Bar Mini", Kind: "device",
		ComponentType: "video-bar", ParentProductID: strptr("acme-bar")}); err != nil {
		t.Fatalf("sub-product: %v", err)
	}
	standard, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "huddle", Label: "Huddle"})
	if err != nil {
		t.Fatalf("standard: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "huddle-1", StandardID: strptr("huddle")}, all, all); err != nil {
		t.Fatalf("system: %v", err)
	}

	// vendor: by handle, the product's vendor arc follows.
	id, err := gw.RenameRegistry(ctx, "", "vendor", "acme", "acme-av")
	if err != nil || id != vendor.ID {
		t.Fatalf("rename vendor = %q, %v; want its uuid %q", id, err, vendor.ID)
	}
	if _, err := gw.GetVendor(ctx, "acme"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Errorf("old vendor name still answers: %v", err)
	}
	if got, err := gw.GetProduct(ctx, "acme-bar"); err != nil || got.VendorName == nil || *got.VendorName != "acme-av" {
		t.Errorf("product's vendor after rename = %v (%v), want acme-av", got.VendorName, err)
	}

	// driver: by uuid, the product's driver arc follows.
	if _, err := gw.RenameRegistry(ctx, "", "driver", driver.ID, "acme-controller"); err != nil {
		t.Fatalf("rename driver by uuid: %v", err)
	}
	if got, err := gw.GetProduct(ctx, "acme-bar"); err != nil || got.DriverName == nil || *got.DriverName != "acme-controller" {
		t.Errorf("product's driver after rename = %v (%v), want acme-controller", got.DriverName, err)
	}

	// product: the sub-product's parent arc follows.
	if _, err := gw.RenameRegistry(ctx, "", "product", "acme-bar", "acme-soundbar"); err != nil {
		t.Fatalf("rename product: %v", err)
	}
	if got, err := gw.GetProduct(ctx, "acme-soundbar"); err != nil || got.ID != product.ID {
		t.Errorf("renamed product = %+v (%v), want the same uuid under the new name", got, err)
	}
	if got, err := gw.GetProduct(ctx, "acme-bar-mini"); err != nil || got.ParentProductName == nil || *got.ParentProductName != "acme-soundbar" {
		t.Errorf("sub-product's parent after rename = %v (%v), want acme-soundbar", got.ParentProductName, err)
	}

	// standard: the system's standard arc follows.
	if _, err := gw.RenameRegistry(ctx, "", "standard", "huddle", "huddle-space"); err != nil {
		t.Fatalf("rename standard: %v", err)
	}
	if got, err := gw.GetSystem(ctx, "huddle-1", all); err != nil || got.StandardName == nil || *got.StandardName != "huddle-space" {
		t.Errorf("system's standard after rename = %v (%v), want huddle-space", got.StandardName, err)
	}

	for resource, id := range map[string]string{"vendor": vendor.ID, "driver": driver.ID, "product": product.ID, "standard": standard.ID} {
		if n := audits(resource, id); n != 1 {
			t.Errorf("%s rename audit rows keyed on its uuid = %d, want 1", resource, n)
		}
	}

	// The refusals: an official row keeps its name (the shipped catalogs; the
	// seeded standards are example content the operator owns, official=false, so
	// they rename like any custom row), a taken name is a conflict, an illegal or
	// uuid-shaped name is refused, an unknown ref is not found on every table.
	officials := map[string]string{"vendor": "kestrel", "driver": "snmp-generic", "product": "generic-device", "location_type": "room"}
	for table, official := range officials {
		if _, err := gw.RenameRegistry(ctx, "", table, official, "renamed-official"); !errors.Is(err, storage.ErrTypeOfficial) {
			t.Errorf("rename official %s %q: want ErrTypeOfficial, got %v", table, official, err)
		}
	}
	for _, table := range []string{"vendor", "driver", "product", "standard", "location_type"} {
		if _, err := gw.RenameRegistry(ctx, "", table, "no-such-row", "anything"); !errors.Is(err, storage.ErrTypeNotFound) {
			t.Errorf("rename unknown %s: want ErrTypeNotFound, got %v", table, err)
		}
	}
	if _, err := gw.RenameRegistry(ctx, "", "vendor", "acme-av", "kestrel"); !errors.Is(err, storage.ErrTypeExists) {
		t.Errorf("rename vendor onto a taken name: want ErrTypeExists, got %v", err)
	}
	if _, err := gw.RenameRegistry(ctx, "", "product", "acme-soundbar", "Acme_Bar"); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Errorf("rename product to an illegal name: want ErrInvalidEntityName, got %v", err)
	}
	if _, err := gw.RenameRegistry(ctx, "", "product", "acme-soundbar", "0198f2c1-4a5b-7c3d-9e0f-1a2b3c4d5e6f"); !errors.Is(err, storage.ErrEntityNameIsUUID) {
		t.Errorf("rename product to a uuid-shaped name: want ErrEntityNameIsUUID, got %v", err)
	}
	if _, err := gw.RenameRegistry(ctx, "", "location", "anything", "anything-else"); err == nil {
		t.Errorf("rename on a table that is not a renameable registry succeeded")
	}
}

// TestLocationTypeRenameRewritesPlacementRules covers the one registry whose
// name is also stored by value: allowed_parent_types holds location_type NAMES
// (a within-registry reference ADR-0062 left as-is), on the type rows and in an
// operator's fork of a shipped type. A rename rewrites every occurrence in the
// same transaction, so placement keeps working under the new name; a rename to
// the reserved "root" sentinel is refused.
func TestLocationTypeRenameRewritesPlacementRules(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	wing, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: "wing", Label: "Wing", AllowedParentTypes: []string{storage.RootPlacement}})
	if err != nil {
		t.Fatalf("wing: %v", err)
	}
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{Name: "suite", Label: "Suite", AllowedParentTypes: []string{"wing", storage.RootPlacement}}); err != nil {
		t.Fatalf("suite: %v", err)
	}
	// An operator's fork of a shipped type that names the custom one.
	if _, err := gw.UpdateLocationType(ctx, "", "room", storage.LocationTypePatch{AllowedParentTypes: &[]string{"floor", "wing"}}); err != nil {
		t.Fatalf("fork room: %v", err)
	}

	if _, err := gw.RenameRegistry(ctx, "", "location_type", "wing", "root"); !errors.Is(err, storage.ErrReservedTypeID) {
		t.Fatalf("rename to the root sentinel: want ErrReservedTypeID, got %v", err)
	}
	id, err := gw.RenameRegistry(ctx, "", "location_type", "wing", "east-wing")
	if err != nil || id != wing.ID {
		t.Fatalf("rename location_type = %q, %v; want its uuid", id, err)
	}

	suite, err := gw.GetLocationType(ctx, "suite")
	if err != nil {
		t.Fatalf("get suite: %v", err)
	}
	if !slices.Equal(suite.AllowedParentTypes, []string{"east-wing", storage.RootPlacement}) {
		t.Errorf("suite allowed_parent_types = %v, want [east-wing root]", suite.AllowedParentTypes)
	}
	room, err := gw.GetLocationType(ctx, "room")
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	if !slices.Equal(room.AllowedParentTypes, []string{"floor", "east-wing"}) || !room.Forked {
		t.Errorf("forked room allowed_parent_types = %v (forked %v), want [floor east-wing] on the fork", room.AllowedParentTypes, room.Forked)
	}

	// Placement keeps working under the new name.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "west", LocationType: "east-wing"}, all); err != nil {
		t.Fatalf("a location of the renamed type: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "suite-100", LocationType: "suite", ParentName: strptr("west")}, all); err != nil {
		t.Errorf("a suite under the renamed type was refused: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-100", LocationType: "room", ParentName: strptr("west")}, all); err != nil {
		t.Errorf("a forked room under the renamed type was refused: %v", err)
	}
}

// TestASeededStandardKeepsItsName: the boot seed plants its example standards
// by NAME, only when absent (SeedStandard), so a renamed one would be planted
// again on the next boot, beside the operator's renamed row, with the shipped
// roles and choices hung on it. The rename refuses a seeded standard instead,
// naming why, and a reboot then finds one row under the shipped name. A row an
// older binary seeded (before the marker existed) is marked by the next boot,
// so it is refused too. Forking a template into a standard of the operator's
// own is #827's.
func TestASeededStandardKeepsItsName(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
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
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	named := func(name string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from standard where name = $1`, name).Scan(&n); err != nil {
			t.Fatalf("count standards named %q: %v", name, err)
		}
		return n
	}

	if _, err := gw.RenameRegistry(ctx, "", "standard", "meeting-room", "conference-room"); !errors.Is(err, storage.ErrRegistrySeededExample) {
		t.Fatalf("rename a seeded standard: want ErrRegistrySeededExample, got %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("reboot seed: %v", err)
	}
	if n, m := named("meeting-room"), named("conference-room"); n != 1 || m != 0 {
		t.Fatalf("after a refused rename and a reboot: %d meeting-room, %d conference-room; want 1 and 0", n, m)
	}

	// A row seeded before the marker existed reads unmarked until the next boot,
	// which marks it without touching anything the operator edited.
	if _, err := conn.Exec(ctx, `update standard set seeded = false, label = 'Our Meeting Room' where name = 'meeting-room'`); err != nil {
		t.Fatalf("unmark: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("reboot seed: %v", err)
	}
	if _, err := gw.RenameRegistry(ctx, "", "standard", "meeting-room", "conference-room"); !errors.Is(err, storage.ErrRegistrySeededExample) {
		t.Fatalf("rename a standard an older boot seeded: want ErrRegistrySeededExample, got %v", err)
	}
	if got, err := gw.GetStandard(ctx, "meeting-room"); err != nil || got.Label != "Our Meeting Room" {
		t.Fatalf("the boot that marked it reverted the operator's edit: %+v, %v", got, err)
	}
}

// TestADriverRenameRederivesItsTasks: every task a driver derives bakes the
// driver's name into its spec (and its id hashes the spec), so a rename that
// only moved the column would leave the node running tasks under a name the
// registry no longer has, and hand the churn to the next unrelated edit. The
// rename re-derives the attached endpoints' tasks in its own transaction, as a
// version change does (#823): every driver task carries the new name, the
// endpoint is bumped so the node picks the set up, and a later label edit moves
// nothing.
func TestADriverRenameRederivesItsTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
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
	all := scope.Set{All: true}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "panel-proto", Label: "Panel", Version: "1.0.0", Spec: []byte(lineProtoSpec)}); err != nil {
		t.Fatalf("driver: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-n"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-n"}, all, all); err != nil {
		t.Fatalf("node: %v", err)
	}
	comp, name, node := "panel-n", "panel-proto", "edge-n"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &name, Component: &comp, Node: &node,
		Inputs: map[string]string{"host": "10.20.4.62"}, SecretRead: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	driverTasks := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, r := range endpointTasks(t, ctx, conn, ep.ID) {
			var spec struct {
				Driver string `json:"driver"`
			}
			if err := json.Unmarshal([]byte(r.Spec), &spec); err != nil {
				t.Fatalf("task spec: %v", err)
			}
			if spec.Driver != "" {
				out[r.ID] = spec.Driver
			}
		}
		return out
	}
	epUpdated := func() time.Time {
		t.Helper()
		var ts time.Time
		if err := conn.QueryRow(ctx, `select updated_at from endpoint where id = $1`, ep.ID).Scan(&ts); err != nil {
			t.Fatalf("endpoint updated_at: %v", err)
		}
		return ts
	}
	before, stampedBefore := driverTasks(), epUpdated()
	if len(before) == 0 {
		t.Fatal("the fixture derived no driver tasks")
	}

	if _, err := gw.RenameRegistry(ctx, "", "driver", "panel-proto", "panel-protocol"); err != nil {
		t.Fatalf("rename driver: %v", err)
	}
	after := driverTasks()
	if len(after) != len(before) {
		t.Fatalf("driver tasks went from %d to %d across the rename", len(before), len(after))
	}
	for id, drv := range after {
		if drv != "panel-protocol" {
			t.Errorf("task %s still bakes driver %q after the rename", id, drv)
		}
	}
	if !epUpdated().After(stampedBefore) {
		t.Error("the endpoint was not bumped, so the node would keep the old task set")
	}

	// The churn happened at the rename, so an unrelated edit moves nothing.
	label := "Panel protocol"
	if _, err := gw.UpdateDriver(ctx, "", "panel-protocol", storage.DriverPatch{Label: &label}); err != nil {
		t.Fatalf("label edit: %v", err)
	}
	if again := driverTasks(); !maps.Equal(again, after) {
		t.Errorf("a label edit after the rename re-derived the tasks: %v, want %v", again, after)
	}
}
