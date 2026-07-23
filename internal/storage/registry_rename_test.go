package storage_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// A registry handle can be renamed with every reference intact. This is the whole
// point of giving the registries uuid primary keys, and it is the test each slice
// of the epic writes first.
//
// Before this, a product id was the primary key, so a typo or a rebrand could not
// be corrected: `cisco-room-bar` was permanent, and every referencing row would
// have had to be rewritten to change it.
func TestRegistryHandleRenameKeepsReferences(t *testing.T) {
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

	if _, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: "acme", DisplayName: "Acme", Kind: "manufacturer"}); err != nil {
		t.Fatalf("vendor: %v", err)
	}
	// A product referencing that vendor, a sub-product referencing the product, a
	// property contract on it, and a component instance of it: one of every
	// inbound reference this slice converted.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "acme-bar", DisplayName: "Acme Bar", Kind: "device",
		VendorID: strptr("acme"), Capabilities: []string{"microphone"}}); err != nil {
		t.Fatalf("product: %v", err)
	}
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "acme-bar-mini", DisplayName: "Acme Bar Mini", Kind: "device",
		ParentProductID: strptr("acme-bar")}); err != nil {
		t.Fatalf("sub-product: %v", err)
	}
	if _, err := gw.SetProductProperty(ctx, "", "acme-bar", storage.ProductPropertySpec{
		PropertyName: "serial_number", Required: true}); err != nil {
		t.Fatalf("property: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "bar-1", ProductName: strptr("acme-bar")}, all); err != nil {
		t.Fatalf("component: %v", err)
	}

	// The rename. Nothing else moves.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `update product set name = 'acme-soundbar' where name = 'acme-bar'`); err != nil {
		t.Fatalf("rename product: %v", err)
	}
	if _, err := conn.Exec(ctx, `update vendor set name = 'acme-av' where name = 'acme'`); err != nil {
		t.Fatalf("rename vendor: %v", err)
	}

	// Every reference still resolves, and now reads the NEW handle.
	got, err := gw.GetProduct(ctx, "acme-soundbar")
	if err != nil {
		t.Fatalf("get renamed product: %v", err)
	}
	if got.VendorName == nil || *got.VendorName != "acme-av" {
		t.Errorf("vendor reads %v, want acme-av: the arc should follow the rename", got.VendorName)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0] != "microphone" {
		t.Errorf("capabilities = %v, want the set intact through the rename", got.Capabilities)
	}
	sub, err := gw.GetProduct(ctx, "acme-bar-mini")
	if err != nil {
		t.Fatalf("get sub-product: %v", err)
	}
	if sub.ParentProductName == nil || *sub.ParentProductName != "acme-soundbar" {
		t.Errorf("parent reads %v, want acme-soundbar", sub.ParentProductName)
	}
	props, err := gw.ListProductProperties(ctx, "acme-soundbar")
	if err != nil {
		t.Fatalf("list properties: %v", err)
	}
	if len(props) != 1 || props[0].PropertyName != "serial_number" {
		t.Errorf("contract = %v, want serial_number still declared", props)
	}
	comp, err := gw.GetComponent(ctx, "bar-1", all)
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	if comp.ProductHandle == nil || *comp.ProductHandle != "acme-soundbar" {
		t.Errorf("component product reads %v, want acme-soundbar", comp.ProductHandle)
	}

	// Slice 2: a capability and a standard rename with every reference intact.
	// The product requires microphone; the standard has a role requiring it and a
	// property contract; renaming both must leave all of that resolving.
	if _, err := gw.CreateStandard(ctx, "", storage.Standard{Name: "huddle", DisplayName: "Huddle"}); err != nil {
		t.Fatalf("standard: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "standard", "huddle", storage.SystemRoleSpec{
		Name: "mic", DisplayName: "Mic", Quorum: 1, Capabilities: []string{"microphone"}, Impact: "degraded"}); err != nil {
		t.Fatalf("role: %v", err)
	}
	if _, err := conn.Exec(ctx, `update capability set name = 'mic-cap' where name = 'microphone'`); err != nil {
		t.Fatalf("rename capability: %v", err)
	}
	if _, err := conn.Exec(ctx, `update standard set name = 'huddle-space' where name = 'huddle'`); err != nil {
		t.Fatalf("rename standard: %v", err)
	}
	// The product's required set still resolves, now reading the capability's new
	// handle; the role still requires it; the standard is addressable by its new one.
	after, err := gw.GetProduct(ctx, "acme-soundbar")
	if err != nil {
		t.Fatalf("get product after capability rename: %v", err)
	}
	if len(after.Capabilities) != 1 || after.Capabilities[0] != "mic-cap" {
		t.Errorf("product capabilities = %v, want [mic-cap] through the rename", after.Capabilities)
	}
	roles, err := gw.ListSystemRoles(ctx, "standard", "huddle-space")
	if err != nil {
		t.Fatalf("list roles by the renamed standard: %v", err)
	}
	if len(roles) != 1 || len(roles[0].Capabilities) != 1 || roles[0].Capabilities[0] != "mic-cap" {
		t.Errorf("role requirement = %v, want [mic-cap]", roles)
	}

	// Slice 3: a property rename follows its contract line, a declared value, AND
	// a telemetry series, since the telemetry key is now a real foreign key.
	if _, err := gw.SetProductProperty(ctx, "", "acme-soundbar", storage.ProductPropertySpec{
		PropertyName: "serial_number", Required: true}); err != nil {
		t.Fatalf("contract: %v", err)
	}
	if _, err := gw.SetPropertyValue(ctx, "", "component", "bar-1", "serial_number", "", []byte(`"SN-1"`), all); err != nil {
		t.Fatalf("value: %v", err)
	}
	if err := gw.InsertMetricDatapoints(ctx, []storage.MetricDatapointEvent{{
		OwnerKind: "component", OwnerID: "bar-1", Key: "tcp.open", Value: 1, Source: "test"}}); err != nil {
		t.Fatalf("datapoint: %v", err)
	}
	if _, err := conn.Exec(ctx, `update property set name = 'serial_no' where name = 'serial_number'`); err != nil {
		t.Fatalf("rename property: %v", err)
	}
	if _, err := conn.Exec(ctx, `update property set name = 'tcp.reachable' where name = 'tcp.open'`); err != nil {
		t.Fatalf("rename telemetry property: %v", err)
	}
	// The contract reads the property's new handle.
	props2, err := gw.ListProductProperties(ctx, "acme-soundbar")
	if err != nil {
		t.Fatalf("list contract after rename: %v", err)
	}
	if len(props2) != 1 || props2[0].PropertyName != "serial_no" {
		t.Errorf("contract property = %v, want serial_no", props2)
	}
	// The datapoint's key follows the property rename, which is the point of the
	// telemetry foreign key.
	dp, err := gw.LatestMetric(ctx, "bar-1", "tcp.reachable")
	if err != nil {
		t.Fatalf("latest metric by the new key: %v", err)
	}
	if dp == nil || dp.Key != "tcp.reachable" {
		t.Errorf("datapoint key = %v, want tcp.reachable through the rename", dp)
	}

	// Slice 4: the leaf registries (driver, location_type, secret_type,
	// interface_type) rename with every inbound reference intact. These are the
	// last string-keyed registries the epic converts, closing the invariant that
	// no foreign key anywhere points at a mutable string.

	// driver: a product's driver arc follows the rename.
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "acme-ctl", DisplayName: "Acme Controller"}); err != nil {
		t.Fatalf("driver: %v", err)
	}
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "acme-panel", DisplayName: "Acme Panel", Kind: "device", DriverID: strptr("acme-ctl")}); err != nil {
		t.Fatalf("product with driver: %v", err)
	}
	if _, err := conn.Exec(ctx, `update driver set name = 'acme-controller' where name = 'acme-ctl'`); err != nil {
		t.Fatalf("rename driver: %v", err)
	}
	panel, err := gw.GetProduct(ctx, "acme-panel")
	if err != nil {
		t.Fatalf("get product after driver rename: %v", err)
	}
	if panel.DriverName == nil || *panel.DriverName != "acme-controller" {
		t.Errorf("driver reads %v, want acme-controller through the rename", panel.DriverName)
	}

	// location_type: a location's type arc AND a location_type_property contract
	// both follow the rename (property serial_no exists from the slice-3 rename above).
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{
		Name: "wing", DisplayName: "Wing", AllowedParentTypes: []string{storage.RootPlacement}}); err != nil {
		t.Fatalf("location type: %v", err)
	}
	if _, err := gw.SetLocationTypeProperty(ctx, "", "wing", storage.LocationTypePropertySpec{
		PropertyName: "serial_no", Required: true}); err != nil {
		t.Fatalf("location type property: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "west-wing", LocationType: "wing"}, all); err != nil {
		t.Fatalf("location: %v", err)
	}
	if _, err := conn.Exec(ctx, `update location_type set name = 'annex' where name = 'wing'`); err != nil {
		t.Fatalf("rename location type: %v", err)
	}
	loc, err := gw.GetLocation(ctx, "west-wing", all)
	if err != nil {
		t.Fatalf("get location after type rename: %v", err)
	}
	if loc.LocationType != "annex" {
		t.Errorf("location type reads %q, want annex through the rename", loc.LocationType)
	}
	ltProps, err := gw.ListLocationTypeProperties(ctx, "annex")
	if err != nil {
		t.Fatalf("list location type properties by the renamed type: %v", err)
	}
	if len(ltProps) != 1 || ltProps[0].PropertyName != "serial_no" {
		t.Errorf("location type contract = %v, want serial_no still declared", ltProps)
	}

	// secret_type: a secret's type arc follows the rename.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "snmp", SecretType: "snmp-community", OwnerKind: "component", OwnerName: strptr("bar-1"),
		Fields: map[string]string{"community": "public"}}, all, true); err != nil {
		t.Fatalf("secret: %v", err)
	}
	if _, err := conn.Exec(ctx, `update secret_type set name = 'snmp-comm' where name = 'snmp-community'`); err != nil {
		t.Fatalf("rename secret type: %v", err)
	}
	secrets, err := gw.ListSecrets(ctx, all, true)
	if err != nil {
		t.Fatalf("list secrets after type rename: %v", err)
	}
	var snmp *storage.Secret
	for i := range secrets {
		if secrets[i].Name == "snmp" {
			snmp = &secrets[i]
		}
	}
	if snmp == nil || snmp.SecretType != "snmp-comm" {
		t.Errorf("secret type reads %v, want snmp-comm through the rename", snmp)
	}

	// interface_type: an interface's transport arc follows the rename.
	iface, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr("bar-1")}, all)
	if err != nil {
		t.Fatalf("interface: %v", err)
	}
	if _, err := conn.Exec(ctx, `update interface_type set name = 'tcp-connect' where name = 'tcp'`); err != nil {
		t.Fatalf("rename interface type: %v", err)
	}
	gotIf, err := gw.GetInterface(ctx, iface.ID, all)
	if err != nil {
		t.Fatalf("get interface after type rename: %v", err)
	}
	if gotIf.Type != "tcp-connect" {
		t.Errorf("interface type reads %q, want tcp-connect through the rename", gotIf.Type)
	}
}
