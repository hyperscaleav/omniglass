package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestMetricTypeCRUD mirrors TestPropertyCRUD on the metric lane (#587): a
// custom metric type registers with its numeric facts (unit, precision), the
// seeded canon is read-only, and the sentinels map one to one.
func TestMetricTypeCRUD(t *testing.T) {
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

	// Create a custom metric type carrying the numeric facts.
	unit := "rpm"
	precision := 0
	mt, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{
		Name: "fan-speed", DataType: "int", DisplayName: "Fan speed",
		Unit: &unit, Precision: &precision, Description: "Rotations per minute.",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if mt.Official {
		t.Fatalf("new metric type official=true, want false")
	}

	// Get it back, numeric facts intact.
	got, err := gw.GetMetricType(ctx, "fan-speed")
	if err != nil || got.DataType != "int" || got.DisplayName != "Fan speed" {
		t.Fatalf("get: %v (%+v)", err, got)
	}
	if got.Unit == nil || *got.Unit != "rpm" || got.Precision == nil || *got.Precision != 0 {
		t.Fatalf("numeric facts did not round-trip: %+v", got)
	}

	// List includes the custom type and the seeded official ones.
	mts, err := gw.ListMetricTypes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := map[string]bool{}
	for _, m := range mts {
		names[m.Name] = true
	}
	if !names["fan-speed"] || !names["icmp-rtt-avg"] || !names["tcp-open"] {
		t.Fatalf("list missing metric types: %v", names)
	}

	// Update the mutable fields.
	label, ms := "Fan Speed (RPM)", "ms"
	if _, err := gw.UpdateMetricType(ctx, "", "fan-speed", storage.MetricTypePatch{DisplayName: &label, Unit: &ms}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := gw.GetMetricType(ctx, "fan-speed"); got.DisplayName != label || got.Unit == nil || *got.Unit != "ms" {
		t.Fatalf("update not applied: %+v", got)
	}

	// A duplicate name is ErrMetricTypeExists.
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "fan-speed", DataType: "int"}); !errors.Is(err, storage.ErrMetricTypeExists) {
		t.Fatalf("dup err = %v, want ErrMetricTypeExists", err)
	}

	// A malformed name is ErrMetricTypeInvalid.
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "Bad-Name", DataType: "int"}); !errors.Is(err, storage.ErrMetricTypeInvalid) {
		t.Fatalf("bad name err = %v, want ErrMetricTypeInvalid", err)
	}

	// Official (seeded) metric types are read-only.
	if _, err := gw.UpdateMetricType(ctx, "", "tcp-open", storage.MetricTypePatch{DisplayName: &label}); !errors.Is(err, storage.ErrMetricTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrMetricTypeOfficial", err)
	}
	if err := gw.DeleteMetricType(ctx, "", "tcp-open"); !errors.Is(err, storage.ErrMetricTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrMetricTypeOfficial", err)
	}

	// An unknown metric type is ErrMetricTypeNotFound.
	if err := gw.DeleteMetricType(ctx, "", "nope"); !errors.Is(err, storage.ErrMetricTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrMetricTypeNotFound", err)
	}
	if _, err := gw.GetMetricType(ctx, "nope"); !errors.Is(err, storage.ErrMetricTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrMetricTypeNotFound", err)
	}

	// Delete the custom type; a re-delete is ErrMetricTypeNotFound.
	if err := gw.DeleteMetricType(ctx, "", "fan-speed"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := gw.DeleteMetricType(ctx, "", "fan-speed"); !errors.Is(err, storage.ErrMetricTypeNotFound) {
		t.Fatalf("re-delete err = %v, want ErrMetricTypeNotFound", err)
	}
}

// TestAuditResourceIDOnTheMetricLane mirrors the property-lane audit invariant:
// every metric_type mutation writes an audit row in the same transaction, keyed
// on the uuid (a rename must not orphan the trail), never on the name.
func TestAuditResourceIDOnTheMetricLane(t *testing.T) {
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

	const name = "audit-key-metric"
	created, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{
		Name: name, DisplayName: "X", DataType: "int"})
	if err != nil {
		t.Fatalf("create metric type: %v", err)
	}
	display := "Y"
	if _, err := gw.UpdateMetricType(ctx, "", name, storage.MetricTypePatch{DisplayName: &display}); err != nil {
		t.Fatalf("update metric type: %v", err)
	}
	if err := gw.DeleteMetricType(ctx, "", name); err != nil {
		t.Fatalf("delete metric type: %v", err)
	}

	rows, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: "metric_type", Limit: 50})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var seen int
	for _, r := range rows {
		if r.ResourceID == name {
			t.Errorf("a metric_type audit row keys on the NAME %q; a rename orphans it", name)
		}
		if r.ResourceID == created.ID {
			seen++
		}
	}
	if seen != 3 {
		t.Errorf("%d metric_type audit rows key on the uuid %q, want 3 (create, update, delete)",
			seen, created.ID)
	}
}
