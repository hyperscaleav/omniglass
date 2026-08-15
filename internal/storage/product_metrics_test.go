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

// TestProductMetricCRUD mirrors TestProductPropertyCRUD on the metric lane: the
// product's declared-metric contract lists, upserts by (product, metric), refuses
// an unknown metric and an official product, and keeps the unaudited seed path
// idempotent. The same test also pins the classifier-existence guards on the
// standard and location-type seed paths (and the location-type PROPERTY one,
// fixed alongside): an unknown classifier is ErrTypeNotFound, never a raw
// constraint violation.
func TestProductMetricCRUD(t *testing.T) {
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

	// A custom (official=false) product owns a mutable contract.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "acme-widget", Label: "Acme Widget", Kind: "device"}); err != nil {
		t.Fatalf("create product: %v", err)
	}

	// A product with no contract lists empty, not an error.
	lines, err := gw.ListProductMetrics(ctx, "acme-widget")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("list empty = %d rows, want 0", len(lines))
	}

	// Declare a required metric with a default.
	pm, err := gw.SetProductMetric(ctx, "", "acme-widget", storage.ProductMetricSpec{
		MetricTypeName: "icmp-rtt-avg", DefaultValue: json.RawMessage(`10.5`), Required: true,
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if pm.ProductName != "acme-widget" || pm.MetricTypeName != "icmp-rtt-avg" || !pm.Required {
		t.Fatalf("set = %+v, want acme-widget/icmp-rtt-avg required", pm)
	}
	if string(pm.DefaultValue) != `10.5` {
		t.Fatalf("set default = %s, want 10.5", pm.DefaultValue)
	}

	// Setting the same metric again updates the contract in place rather than
	// adding a second row (the (product, metric) pair is the key).
	if _, err := gw.SetProductMetric(ctx, "", "acme-widget", storage.ProductMetricSpec{
		MetricTypeName: "icmp-rtt-avg", DefaultValue: json.RawMessage(`20`), Required: false,
	}); err != nil {
		t.Fatalf("re-set: %v", err)
	}
	lines, err = gw.ListProductMetrics(ctx, "acme-widget")
	if err != nil {
		t.Fatalf("list after re-set: %v", err)
	}
	if len(lines) != 1 || string(lines[0].DefaultValue) != `20` || lines[0].Required {
		t.Fatalf("re-set = %+v, want one line defaulting to 20 required=false", lines)
	}

	// A contract may declare a metric with no default at all; nil round-trips as
	// nil (a SQL NULL), not as the JSON literal null.
	noDefault, err := gw.SetProductMetric(ctx, "", "acme-widget", storage.ProductMetricSpec{MetricTypeName: "tcp-open"})
	if err != nil {
		t.Fatalf("set without default: %v", err)
	}
	if noDefault.DefaultValue != nil {
		t.Fatalf("set without default = %s, want nil", noDefault.DefaultValue)
	}
	lines, err = gw.ListProductMetrics(ctx, "acme-widget")
	if err != nil {
		t.Fatalf("list two: %v", err)
	}
	// Ordered by metric name: icmp-rtt-avg before tcp-open.
	if len(lines) != 2 || lines[0].MetricTypeName != "icmp-rtt-avg" || lines[1].MetricTypeName != "tcp-open" {
		t.Fatalf("list two = %+v, want icmp-rtt-avg then tcp-open", lines)
	}

	// An unknown metric name is a missing catalog reference, not a silent insert.
	if _, err := gw.SetProductMetric(ctx, "", "acme-widget", storage.ProductMetricSpec{MetricTypeName: "nope-not-a-metric"}); !errors.Is(err, storage.ErrMetricTypeNotFound) {
		t.Fatalf("unknown metric err = %v, want ErrMetricTypeNotFound", err)
	}

	// Delete clears one declaration; a re-delete is ErrTypeNotFound.
	if err := gw.DeleteProductMetric(ctx, "", "acme-widget", "tcp-open"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := gw.DeleteProductMetric(ctx, "", "acme-widget", "icmp-rtt-avg"); err != nil {
		t.Fatalf("delete second: %v", err)
	}
	lines, err = gw.ListProductMetrics(ctx, "acme-widget")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("list after delete = %d rows, want 0", len(lines))
	}
	if err := gw.DeleteProductMetric(ctx, "", "acme-widget", "icmp-rtt-avg"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("re-delete err = %v, want ErrTypeNotFound", err)
	}

	// A seeded (official) product's contract is seed-owned and read-only.
	if _, err := gw.SetProductMetric(ctx, "", "cisco-room-bar", storage.ProductMetricSpec{MetricTypeName: "tcp-open"}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("set official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteProductMetric(ctx, "", "cisco-room-bar", "tcp-open"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// The boot-seed path writes an official product's contract without the guard
	// and without an audit, and is idempotent.
	if err := gw.UpsertProductMetric(ctx, "cisco-room-bar", storage.ProductMetricSpec{
		MetricTypeName: "tcp-open", DefaultValue: json.RawMessage(`1`), Required: true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := gw.UpsertProductMetric(ctx, "cisco-room-bar", storage.ProductMetricSpec{
		MetricTypeName: "tcp-open", DefaultValue: json.RawMessage(`0`), Required: false,
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	lines, err = gw.ListProductMetrics(ctx, "cisco-room-bar")
	if err != nil {
		t.Fatalf("list official: %v", err)
	}
	if len(lines) != 1 || string(lines[0].DefaultValue) != `0` || lines[0].Required {
		t.Fatalf("upsert = %+v, want one tcp-open line defaulting to 0 required=false", lines)
	}

	// An unknown classifier is ErrTypeNotFound on both write paths of every
	// family, never a raw not-null violation from the resolved-to-NULL insert.
	if _, err := gw.SetProductMetric(ctx, "", "no-such-product", storage.ProductMetricSpec{MetricTypeName: "tcp-open"}); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("set unknown product err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.UpsertProductMetric(ctx, "no-such-product", storage.ProductMetricSpec{MetricTypeName: "tcp-open"}); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("upsert unknown product err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.UpsertStandardMetric(ctx, "no-such-standard", storage.StandardMetricSpec{MetricTypeName: "tcp-open"}); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("upsert unknown standard err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.UpsertLocationTypeMetric(ctx, "no-such-type", storage.LocationTypeMetricSpec{MetricTypeName: "tcp-open"}); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("upsert unknown location type err = %v, want ErrTypeNotFound", err)
	}
	// The location-type PROPERTY seed path had the same gap (product resolves the
	// ref, standard inline-selects, location_type had nothing); fixed with the
	// metric guards, pinned here.
	if err := gw.UpsertLocationTypeProperty(ctx, "no-such-type", storage.LocationTypePropertySpec{PropertyTypeName: "model-number"}); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("upsert unknown location type property err = %v, want ErrTypeNotFound", err)
	}
}
