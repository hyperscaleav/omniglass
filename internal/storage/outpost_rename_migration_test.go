package storage_test

import (
	"context"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The node becomes an outpost (#2059, ADR-2031): one migration renames every
// schema object that spells the old noun and moves the rows that carry it as
// a value. These tests stand a database just below the rename, plant a node
// and what it owns, and migrate forward over them.

const outpostRenameVersion = "20261117011457"

// schemaNodeNames is every table, column, constraint, index and sequence in
// the public schema whose name spells node as a word.
const schemaNodeNames = `
	select relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
	 where n.nspname = 'public' and relname ~ '(^|_)node(_|$)'
	union all
	select table_name || '.' || column_name from information_schema.columns
	 where table_schema = 'public' and column_name ~ '(^|_)node(_|$)'
	union all
	select conname from pg_constraint con join pg_namespace n on n.oid = con.connamespace
	 where n.nspname = 'public' and conname ~ '(^|_)node(_|$)'
	order by 1`

// TestOutpostRenameCarriesEveryRow: across the rename no schema name spells
// node, the planted outpost keeps its principal, its log, its property and
// its tag binding under the outpost kind, its endpoint stays assigned to it, an operator role's node permission
// becomes outpost's, the seeded platform rows keep their ids under the new
// names, and the old kind is refused. Rolled back, every row is a node's
// again.
func TestOutpostRenameCarriesEveryRow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, outpostRenameVersion); err != nil {
		t.Fatalf("roll back below the rename: %v", err)
	}
	conn := connectDSN(t, dsn)

	// The test database carries no boot seed, so the platform rows the seed
	// keys by name are planted here as they ship.
	mustExec(t, conn, `insert into component_type (name) values ('omniglass-node')`)
	mustExec(t, conn, `insert into product (name, kind, component_type_id)
		select 'omniglass-node', 'service', id from component_type where name = 'omniglass-node'`)
	mustExec(t, conn, `insert into event_type (name) values ('rename-silent')`)
	mustExec(t, conn, `insert into event_rule (name, sample_key, sample_lane, fire_criteria, fire_event_type_id)
		select 'node-silent', 'silence', 'metric', '{}', id from event_type where name = 'rename-silent'`)
	mustExec(t, conn, `insert into property_type (name, data_type) values ('rename-p1', 'string')`)

	mustExec(t, conn, `insert into principal (kind) values ('node')`)
	mustExec(t, conn, `insert into node (principal_id, name)
		select id, 'rename-n1' from principal where kind = 'node' order by seq desc limit 1`)
	nodeID := `(select principal_id from node where name = 'rename-n1')`
	mustExec(t, conn, `insert into node_log (node_id, message) values (`+nodeID+`, 'hello')`)
	mustExec(t, conn, `insert into property (owner_kind, node_id, property_type_id, value, provenance)
		values ('node', `+nodeID+`, (select id from property_type where name = 'rename-p1'), '"v"', 'observed')`)
	mustExec(t, conn, `insert into endpoint (name, transport, node_name) values ('rename-e1', 'tcp', `+nodeID+`)`)
	mustExec(t, conn, `insert into tag (name) values ('rename-t1')`)
	mustExec(t, conn, `insert into tag_binding (tag_id, owner_kind, node_id, value)
		values ((select id from tag where name = 'rename-t1'), 'node', `+nodeID+`, 'x')`)
	mustExec(t, conn, `insert into role (name, permissions) values ('rename-r1', '{component:read,node:read,node:enroll}')`)

	ids := func(table, name string) string {
		t.Helper()
		var id string
		if err := conn.QueryRow(ctx, `select id::text from `+table+` where name = $1`, name).Scan(&id); err != nil {
			t.Fatalf("%s %s: %v", table, name, err)
		}
		return id
	}
	typeID, productID, ruleID := ids("component_type", "omniglass-node"), ids("product", "omniglass-node"), ids("event_rule", "node-silent")

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	if left := snapshotRows(t, conn, schemaNodeNames, true); len(left) > 0 {
		t.Errorf("after the rename the schema still spells node: %v", left)
	}
	outpostID := `(select user_id from outpost where name = 'rename-n1')`
	for _, q := range []string{
		`select count(*) from users where kind = 'machine' and id = ` + outpostID,
		`select count(*) from outpost_log where message = 'hello' and outpost_id = ` + outpostID,
		`select count(*) from property where owner_kind = 'outpost' and outpost_id = ` + outpostID,
		`select count(*) from tag_binding where owner_kind = 'outpost' and outpost_id = ` + outpostID,
		`select count(*) from endpoint where name = 'rename-e1' and outpost_id = ` + outpostID,
	} {
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d (%v), want 1", q, n, err)
		}
	}
	var perms []string
	if err := conn.QueryRow(ctx, `select permissions from role where name = 'rename-r1'`).Scan(&perms); err != nil {
		t.Fatal(err)
	}
	if want := []string{"component:read", "outpost:read", "outpost:enroll"}; !slices.Equal(perms, want) {
		t.Errorf("role permissions %v, want %v", perms, want)
	}
	if got := ids("component_type", "omniglass-outpost"); got != typeID {
		t.Errorf("component type id %s, want it kept as %s", got, typeID)
	}
	if got := ids("product", "omniglass-outpost"); got != productID {
		t.Errorf("product id %s, want it kept as %s", got, productID)
	}
	if got := ids("event_rule", "outpost-silent"); got != ruleID {
		t.Errorf("event rule id %s, want it kept as %s", got, ruleID)
	}
	if _, err := conn.Exec(ctx, `insert into users (kind) values ('node')`); err == nil {
		t.Error("a user of kind node was accepted after the rename")
	}
	if _, err := conn.Exec(ctx, `insert into tag_binding (tag_id, owner_kind, outpost_id, value)
		values ((select id from tag where name = 'rename-t1'), 'outpost', `+outpostID+`, 'y')`); err == nil {
		t.Error("a second binding of one tag to one outpost was accepted")
	}

	if err := migrate.RollbackBelow(dsn, outpostRenameVersion); err != nil {
		t.Fatalf("roll back the rename: %v", err)
	}
	for _, q := range []string{
		`select count(*) from principal where kind = 'node' and id = ` + nodeID,
		`select count(*) from property where owner_kind = 'node' and node_id = ` + nodeID,
		`select count(*) from tag_binding where owner_kind = 'node' and node_id = ` + nodeID,
		`select count(*) from endpoint where name = 'rename-e1' and node_name = ` + nodeID,
		`select count(*) from role where name = 'rename-r1' and permissions = '{component:read,node:read,node:enroll}'`,
		`select count(*) from event_rule where name = 'node-silent'`,
	} {
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("rolled back, %s: %d (%v), want 1", q, n, err)
		}
	}
}
