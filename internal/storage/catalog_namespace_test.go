package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// An operator's create lands in `local`, and the database itself refuses an
// `og` row that is not official, so no write path can plant one (#1431).
func TestAnOperatorsCatalogRowIsLocalAndOgIsOfficialOnly(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "fan-speed", DataType: "int"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: "acme", Kind: "manufacturer"}); err != nil {
		t.Fatal(err)
	}
	for table, name := range map[string]string{"metric_type": "fan-speed", "vendor": "acme"} {
		var ns string
		if err := conn.QueryRow(ctx, `select namespace from `+table+` where name = $1`, name).Scan(&ns); err != nil {
			t.Fatalf("%s %s: %v", table, name, err)
		}
		if ns != "local" {
			t.Errorf("an operator's %s %s landed in %q, want local", table, name, ns)
		}
	}

	var pgErr *pgconn.PgError
	_, err = conn.Exec(ctx, `insert into vendor (namespace, name, official, kind) values ('og', 'not-ours', false, 'manufacturer')`)
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "vendor_og_official_check" {
		t.Errorf("an og row that is not official: err %v, want vendor_og_official_check", err)
	}
	_, err = conn.Exec(ctx, `insert into vendor (namespace, name, kind) values ('Not Kebab', 'acme-two', 'manufacturer')`)
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "vendor_namespace_check" {
		t.Errorf("a namespace that is not one kebab segment: err %v, want vendor_namespace_check", err)
	}
}

// The search path (#1432): a release that ships a name an operator already
// holds writes its row beside theirs, a bare name reaches the operator's, and
// deleting theirs lets the shipped one answer. An operator's own create never
// takes a shipped name: a shadow arises only from a release.
func TestABareNameReachesTheOperatorsRowAndTheShippedOneWaits(t *testing.T) {
	ctx := context.Background()
	gw := storagetest.NewDB(t)

	// A signal catalog.
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "fan-speed", DataType: "int"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.UpsertMetricType(ctx, storage.MetricType{Name: "fan-speed", DataType: "float", Official: true}); err != nil {
		t.Fatalf("a release shipping a name the operator holds: %v", err)
	}
	got, err := gw.GetMetricType(ctx, "fan-speed")
	if err != nil || got.Namespace != "local" || got.DataType != "int" {
		t.Fatalf("a bare name reads %+v (%v), want the operator's int", got, err)
	}
	if err := gw.DeleteMetricType(ctx, "", "fan-speed"); err != nil {
		t.Fatalf("delete the operator's: %v", err)
	}
	got, err = gw.GetMetricType(ctx, "fan-speed")
	if err != nil || got.Namespace != "og" || got.DataType != "float" {
		t.Fatalf("after the operator's is deleted the bare name reads %+v (%v), want the shipped float", got, err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "fan-speed", DataType: "int"}); !errors.Is(err, storage.ErrMetricTypeExists) {
		t.Errorf("an operator's create of a shipped name: err %v, want ErrMetricTypeExists", err)
	}

	// An ordinary catalog.
	if _, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: "acme", Kind: "manufacturer", Label: "Mine"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.UpsertVendor(ctx, storage.Vendor{Name: "acme", Kind: "manufacturer", Label: "Shipped", Official: true}); err != nil {
		t.Fatalf("a release shipping a vendor the operator holds: %v", err)
	}
	v, err := gw.GetVendor(ctx, "acme")
	if err != nil || v.Label != "Mine" || v.Official {
		t.Fatalf("a bare vendor name reads %+v (%v), want the operator's", v, err)
	}
	if err := gw.DeleteVendor(ctx, "", "acme"); err != nil {
		t.Fatalf("delete the operator's vendor: %v", err)
	}
	if v, err = gw.GetVendor(ctx, "acme"); err != nil || v.Label != "Shipped" || !v.Official {
		t.Fatalf("after the operator's vendor is deleted the bare name reads %+v (%v), want the shipped one", v, err)
	}
}
