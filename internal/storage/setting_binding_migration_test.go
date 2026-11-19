package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A value set at one cascade level is a binding that a deeper one shadows
// (#2079, ADR-2037); override is the catalog's sparse edit alone. One
// migration renames the settings table and every constraint and index named
// for it. This test stands a database just below the rename, plants a
// platform binding, and migrates forward over it.

const settingBindingVersion = "20261117231532"

// schemaSettingOverrideNames is every table, constraint and index in the
// public schema still named for the setting override.
const schemaSettingOverrideNames = `
	select relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
	 where n.nspname = 'public' and relname ~ 'setting_override'
	union all
	select conname from pg_constraint con join pg_namespace n on n.oid = con.connamespace
	 where n.nspname = 'public' and conname ~ 'setting_override'
	order by 1`

// TestSettingBindingRenameCarriesEveryRow: across the rename no schema name
// spells setting_override, the planted binding keeps its id and document, and
// the renamed identity key still refuses a second binding for one level and
// namespace. Rolled back, the row sits under the old names again.
func TestSettingBindingRenameCarriesEveryRow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, settingBindingVersion); err != nil {
		t.Fatalf("roll back below the rename: %v", err)
	}
	conn := connectDSN(t, dsn)

	mustExec(t, conn, `delete from setting_override where scope = 'platform' and namespace = 'binding-ns'`)
	mustExec(t, conn, `insert into setting_override (scope, namespace, doc)
		values ('platform', 'binding-ns', '{"retention_days": 7}')`)
	var id string
	if err := conn.QueryRow(ctx, `select id::text from setting_override where namespace = 'binding-ns'`).Scan(&id); err != nil {
		t.Fatal(err)
	}

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	if left := snapshotRows(t, conn, schemaSettingOverrideNames, true); len(left) > 0 {
		t.Errorf("after the rename the schema still spells setting_override: %v", left)
	}
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from setting_binding
		where id::text = $1 and doc->>'retention_days' = '7'`, id).Scan(&n); err != nil || n != 1 {
		t.Errorf("the planted binding: %d (%v), want 1", n, err)
	}
	if _, err := conn.Exec(ctx, `insert into setting_binding (scope, namespace) values ('platform', 'binding-ns')`); err == nil {
		t.Error("a second binding for one level and namespace was accepted")
	}
	var keyed int
	if err := conn.QueryRow(ctx, `select count(*) from pg_constraint
		where conname in ('setting_binding_identity', 'setting_binding_pkey', 'setting_binding_scope_check')`).Scan(&keyed); err != nil || keyed != 3 {
		t.Errorf("renamed constraints: %d (%v), want 3", keyed, err)
	}

	if err := migrate.RollbackBelow(dsn, settingBindingVersion); err != nil {
		t.Fatalf("roll back the rename: %v", err)
	}
	for _, q := range []string{
		`select count(*) from setting_override where id::text = '` + id + `'`,
		`select count(*) from pg_constraint where conname = 'setting_override_identity'`,
	} {
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("rolled back, %s: %d (%v), want 1", q, n, err)
		}
	}
}
