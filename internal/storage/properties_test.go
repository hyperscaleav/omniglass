package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestPropertyCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a custom property. Numbers are the other lane (#587), so the
	// property catalog's fixture is a string.
	prop, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{
		Name: "rack-unit", DataType: "string", Label: "Rack unit",
		Validation: json.RawMessage(`{"pattern":"^u[0-9]+$"}`), Description: "U position.",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if prop.Official {
		t.Fatalf("new property official=true, want false")
	}

	// Get it back.
	got, err := gw.GetPropertyType(ctx, "rack-unit")
	if err != nil || got.DataType != "string" || got.Label != "Rack unit" {
		t.Fatalf("get: %v (%+v)", err, got)
	}

	// List includes the custom property and the seeded official ones.
	props, err := gw.ListPropertyTypes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := map[string]bool{}
	for _, pp := range props {
		names[pp.Name] = true
	}
	if !names["rack-unit"] || !names["serial-number"] || !names["endpoint-reachable"] {
		t.Fatalf("list missing properties: %v", names)
	}
	if names["icmp-reachable"] {
		t.Fatalf("icmp-reachable listed on the property lane; numbers are metric types")
	}

	// Update a mutable field.
	label := "Rack Unit (U)"
	if _, err := gw.UpdatePropertyType(ctx, "", "rack-unit", storage.PropertyTypePatch{Label: &label}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := gw.GetPropertyType(ctx, "rack-unit"); got.Label != label {
		t.Fatalf("update not applied: %q", got.Label)
	}

	// A duplicate name is ErrPropertyExists.
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "rack-unit", DataType: "string"}); !errors.Is(err, storage.ErrPropertyTypeExists) {
		t.Fatalf("dup err = %v, want ErrPropertyExists", err)
	}

	// A malformed name is ErrPropertyInvalid.
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "Bad-Name", DataType: "string"}); !errors.Is(err, storage.ErrPropertyTypeInvalid) {
		t.Fatalf("bad name err = %v, want ErrPropertyInvalid", err)
	}

	// Official (seeded) properties are read-only.
	if _, err := gw.UpdatePropertyType(ctx, "", "serial-number", storage.PropertyTypePatch{Label: &label}); !errors.Is(err, storage.ErrPropertyTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrPropertyOfficial", err)
	}
	if err := gw.DeletePropertyType(ctx, "", "serial-number"); !errors.Is(err, storage.ErrPropertyTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrPropertyOfficial", err)
	}

	// An unknown property is ErrPropertyNotFound.
	if err := gw.DeletePropertyType(ctx, "", "nope"); !errors.Is(err, storage.ErrPropertyTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrPropertyNotFound", err)
	}
	if _, err := gw.GetPropertyType(ctx, "nope"); !errors.Is(err, storage.ErrPropertyTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrPropertyNotFound", err)
	}

	// Delete the custom property; a re-delete is ErrPropertyNotFound.
	if err := gw.DeletePropertyType(ctx, "", "rack-unit"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := gw.DeletePropertyType(ctx, "", "rack-unit"); !errors.Is(err, storage.ErrPropertyTypeNotFound) {
		t.Fatalf("re-delete err = %v, want ErrPropertyNotFound", err)
	}
}

// TestRegistryNamesAreUniqueAcrossBothRegistries pins the fence that stops the
// registry-shadowing bug at its source. property_type and event_type are separate
// tables with separate uniqueness, so a name could land in both, and a name in
// both resolves to NOTHING at ingest: the snapshot refuses it rather than picking
// a winner, so every sample carrying it disappears silently. Refusing at create is
// the only moment an operator can be told, while they still have the name in hand.
func TestRegistryNamesAreUniqueAcrossBothRegistries(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// video-input is seeded as a state property, so the event registry must refuse it.
	if _, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{
		Name: "video-input", Label: "Video Input",
	}); !errors.Is(err, storage.ErrEventTypeExists) {
		t.Fatalf("CreateEventType on an existing property name = %v, want ErrEventTypeExists", err)
	}

	// The mirror: call-started is a seeded event type, so the property registry
	// must refuse it.
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{
		Name: "call-started", Label: "Call Started", DataType: "string",
	}); !errors.Is(err, storage.ErrPropertyTypeExists) {
		t.Fatalf("CreatePropertyType on an existing event type name = %v, want ErrPropertyTypeExists", err)
	}

	// The split's new seams (#587): a seeded metric name is refused on the
	// property lane, and a seeded property name is refused on the metric lane, so
	// no name can route to two sinks.
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{
		Name: "icmp-rtt-avg", Label: "RTT", DataType: "string",
	}); !errors.Is(err, storage.ErrPropertyTypeExists) {
		t.Fatalf("CreatePropertyType on an existing metric type name = %v, want ErrPropertyTypeExists", err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{
		Name: "video-input", Label: "Video Input", DataType: "int",
	}); !errors.Is(err, storage.ErrMetricTypeExists) {
		t.Fatalf("CreateMetricType on an existing property name = %v, want ErrMetricTypeExists", err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{
		Name: "call-started", Label: "Call Started", DataType: "int",
	}); !errors.Is(err, storage.ErrMetricTypeExists) {
		t.Fatalf("CreateMetricType on an existing event type name = %v, want ErrMetricTypeExists", err)
	}

	// A free name in either registry is unaffected.
	if _, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{
		Name: "cable-unplugged", Label: "Cable Unplugged",
	}); err != nil {
		t.Fatalf("CreateEventType on a free name: %v", err)
	}
}
