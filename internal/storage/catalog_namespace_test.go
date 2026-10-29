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
