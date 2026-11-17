package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// What a system needs filled becomes a system slot (#2061, ADR-2032): one
// migration renames the tables, columns, constraints and indexes that spell
// the system sense of role, and leaves the IAM role alone. This test stands a
// database just below the rename, plants a slot and what hangs off it, and
// migrates forward over them.

const slotRenameVersion = "20261117050000"

// schemaSystemRoleNames is every table, column, constraint and index in the
// public schema whose name spells the system sense of role. The IAM role
// table, user_grant.role_id (principal_grant below #2075) and its constraints are the kept sense.
const schemaSystemRoleNames = `
	select relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
	 where n.nspname = 'public' and relname ~ '(system_role|role_choice)'
	union all
	select table_name || '.' || column_name from information_schema.columns
	 where table_schema = 'public' and column_name ~ 'role'
	   and table_name not in ('role', 'principal_grant', 'user_grant', 'credential', 'oauth_code')
	union all
	select conname from pg_constraint con join pg_namespace n on n.oid = con.connamespace
	 where n.nspname = 'public' and conname ~ '(system_role|role_choice)'
	order by 1`

// TestSlotRenameCarriesEveryRow: across the rename no schema name spells the
// system sense of role, the planted slot keeps its id, its typed-slot guard,
// its assignment and its owner's choice, the IAM role and its grant are
// untouched, and the renamed unique keys still refuse a duplicate. Rolled
// back, every row sits under the old names again.
func TestSlotRenameCarriesEveryRow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, slotRenameVersion); err != nil {
		t.Fatalf("roll back below the rename: %v", err)
	}
	conn := connectDSN(t, dsn)

	mustExec(t, conn, `insert into system (name) values ('slot-sys')`)
	mustExec(t, conn, `insert into component (name, product_id)
		values ('slot-c1', (select id from product where name = 'generic-device'))`)
	mustExec(t, conn, `insert into system_role (owner_kind, system_id, name, label)
		values ('system', (select id from system where name = 'slot-sys'), 'main-display', 'Main display')`)
	roleID := `(select id from system_role where name = 'main-display')`
	mustExec(t, conn, `insert into system_role_type (role_id, component_type_id)
		select `+roleID+`, component_type_id from product where name = 'generic-device'`)
	mustExec(t, conn, `insert into system_role_product (role_id, product_id)
		select `+roleID+`, id from product where name = 'generic-device'`)
	mustExec(t, conn, `insert into system_role_assignment (role_id, component_id, system_id, position)
		values (`+roleID+`, (select id from component where name = 'slot-c1'), (select id from system where name = 'slot-sys'), 1)`)
	mustExec(t, conn, `insert into role_choice (owner_kind, system_id, name, label)
		values ('system', (select id from system where name = 'slot-sys'), 'display-size', 'Display size')`)
	mustExec(t, conn, `insert into role (name, permissions) values ('slot-r1', '{system:read}')`)

	var slotID string
	if err := conn.QueryRow(ctx, `select id::text from system_role where name = 'main-display'`).Scan(&slotID); err != nil {
		t.Fatal(err)
	}

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	if left := snapshotRows(t, conn, schemaSystemRoleNames, true); len(left) > 0 {
		t.Errorf("after the rename the schema still spells the system role: %v", left)
	}
	for _, q := range []string{
		`select count(*) from system_slot where name = 'main-display' and id::text = '` + slotID + `'`,
		`select count(*) from system_slot_type where slot_id::text = '` + slotID + `'`,
		`select count(*) from system_slot_product where slot_id::text = '` + slotID + `'`,
		`select count(*) from system_slot_assignment where slot_id::text = '` + slotID + `' and position = 1`,
		`select count(*) from slot_choice where name = 'display-size'`,
		`select count(*) from role where name = 'slot-r1'`,
		`select count(*) from information_schema.columns where table_name = 'user_grant' and column_name = 'role_id'`,
	} {
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d (%v), want 1", q, n, err)
		}
	}
	if _, err := conn.Exec(ctx, `insert into system_slot_assignment (slot_id, component_id, system_id, position)
		values ('`+slotID+`', (select id from component where name = 'slot-c1'), (select id from system where name = 'slot-sys'), 2)`); err == nil {
		t.Error("a second assignment of one component in one system was accepted")
	}
	var keyed int
	if err := conn.QueryRow(ctx, `select count(distinct name) from (
		select conname as name from pg_constraint union all select relname from pg_class where relkind = 'i') k
		where name in ('system_slot_assignment_component_key', 'system_slot_assignment_position_key',
		               'system_slot_capacity_check', 'system_slot_owner_arc_check', 'slot_choice_owner_arc_check')`).Scan(&keyed); err != nil || keyed != 5 {
		t.Errorf("renamed constraints the gateway maps: %d (%v), want 5", keyed, err)
	}

	if err := migrate.RollbackBelow(dsn, slotRenameVersion); err != nil {
		t.Fatalf("roll back the rename: %v", err)
	}
	for _, q := range []string{
		`select count(*) from system_role where name = 'main-display' and id::text = '` + slotID + `'`,
		`select count(*) from system_role_type where role_id::text = '` + slotID + `'`,
		`select count(*) from system_role_product where role_id::text = '` + slotID + `'`,
		`select count(*) from system_role_assignment where role_id::text = '` + slotID + `'`,
		`select count(*) from role_choice where name = 'display-size'`,
		`select count(*) from pg_class where relname = 'system_role_assignment_position_key'`,
	} {
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("rolled back, %s: %d (%v), want 1", q, n, err)
		}
	}
}
