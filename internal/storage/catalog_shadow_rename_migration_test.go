package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The fork shadow table is the catalog's, not a catalog's (#2070, ADR-2035):
// one migration renames the table, its key column and its primary key. This
// test stands a database just below the rename, plants a shadow row, and
// migrates forward over it.

const catalogShadowRenameVersion = "20261117041706"

// schemaCatalogNames is every table, column and constraint in the public
// schema whose name spells catalog.
const schemaCatalogNames = `
	select relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
	 where n.nspname = 'public' and relname ~ 'catalog'
	union all
	select table_name || '.' || column_name from information_schema.columns
	 where table_schema = 'public' and column_name ~ 'catalog'
	union all
	select conname from pg_constraint con join pg_namespace n on n.oid = con.connamespace
	 where n.nspname = 'public' and conname ~ 'catalog'
	order by 1`

// TestCatalogShadowRenameCarriesEveryRow: across the rename no schema name
// spells catalog, the planted shadow keeps its image under the catalog key,
// and the primary key still refuses a second shadow of one row. Rolled back,
// the row is the catalog_shadow's again.
func TestCatalogShadowRenameCarriesEveryRow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, catalogShadowRenameVersion); err != nil {
		t.Fatalf("roll back below the rename: %v", err)
	}
	conn := connectDSN(t, dsn)

	const rowID = "00000000-0000-0000-0000-000000002070"
	mustExec(t, conn, `insert into catalog_shadow (catalog, row_id, image)
		values ('component_type', $1, '{"description":"forked"}')`, rowID)

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	if left := snapshotRows(t, conn, schemaCatalogNames, true); len(left) > 0 {
		t.Errorf("after the rename the schema still spells catalog: %v", left)
	}
	var image string
	if err := conn.QueryRow(ctx, `select image->>'description' from catalog_shadow
		where catalog = 'component_type' and row_id = $1`, rowID).Scan(&image); err != nil || image != "forked" {
		t.Errorf("catalog_shadow row: %q (%v), want forked", image, err)
	}
	if _, err := conn.Exec(ctx, `insert into catalog_shadow (catalog, row_id, image)
		values ('component_type', $1, '{}')`, rowID); err == nil {
		t.Error("a second shadow of one catalog row was accepted")
	}

	if err := migrate.RollbackBelow(dsn, catalogShadowRenameVersion); err != nil {
		t.Fatalf("roll back the rename: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from catalog_shadow
		where catalog = 'component_type' and row_id = $1`, rowID).Scan(&n); err != nil || n != 1 {
		t.Errorf("rolled back, catalog_shadow rows: %d (%v), want 1", n, err)
	}
}
