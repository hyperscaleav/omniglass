package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The IAM subject becomes a user (#2075, ADR-2036): one migration renames the
// principal tables, the human and service detail tables, every principal
// column and every constraint, index, sequence and trigger named for them,
// and rewrites the three value sets that spell the old words (a user's kind,
// a notification target's kind, a role's permissions). This test stands a
// database just below the rename, plants a person, a machine user, an
// outpost and what hangs off them, and migrates forward over them.

const userRenameVersion = "20261117182952"

// schemaPrincipalNames is every table, column, constraint, index, sequence
// and trigger in the public schema whose name spells principal, human or
// service. A column like service_name on another table is not the subject.
const schemaPrincipalNames = `
	select relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
	 where n.nspname = 'public' and (relname ~ 'principal' or relname ~ '^(human|service)(_|$)')
	union all
	select table_name || '.' || column_name from information_schema.columns
	 where table_schema = 'public' and column_name ~ 'principal'
	union all
	select conname from pg_constraint con join pg_namespace n on n.oid = con.connamespace
	 where n.nspname = 'public' and (conname ~ 'principal' or conname ~ '^(human|service)_')
	union all
	select tgname from pg_trigger where tgname ~ 'principal'
	order by 1`

// TestUserRenameCarriesEveryRow: across the rename no schema name spells
// principal, every planted subject keeps its id under its new kind (a human
// is a person, a machine user and an outpost are machines), the group,
// membership, grant and notification target keep their rows, a role's
// permissions name the user resources in their old order, and the owner
// guard reads the renamed grant table. Rolled back, every row sits under the old names and
// kinds again, the outpost told from the machine user by its outpost row.
func TestUserRenameCarriesEveryRow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, userRenameVersion); err != nil {
		t.Fatalf("roll back below the rename: %v", err)
	}
	conn := connectDSN(t, dsn)

	var personID, machineID, outpostID string
	for _, p := range []struct {
		kind string
		id   *string
	}{{"human", &personID}, {"service", &machineID}, {"outpost", &outpostID}} {
		if err := conn.QueryRow(ctx, `insert into principal (kind) values ($1) returning id::text`, p.kind).Scan(p.id); err != nil {
			t.Fatalf("plant a %s: %v", p.kind, err)
		}
	}
	mustExec(t, conn, `insert into human (principal_id, username, email) values ($1, 'ur-person', 'ur@example.com')`, personID)
	mustExec(t, conn, `insert into service (principal_id, name) values ($1, 'ur-machine')`, machineID)
	mustExec(t, conn, `insert into outpost (principal_id, name) values ($1, 'ur-outpost')`, outpostID)
	mustExec(t, conn, `insert into principal_group (name) values ('ur-group')`)
	groupID := `(select id from principal_group where name = 'ur-group')`
	mustExec(t, conn, `insert into principal_group_member (group_id, principal_id) values (`+groupID+`, $1)`, personID)
	mustExec(t, conn, `insert into role (name, permissions)
		values ('ur-role', '{system:read,principal:read,principal_group:*,principal_grant:create,principal:purge:admin}')`)
	mustExec(t, conn, `insert into principal_grant (principal_id, role_id, scope_kind)
		values ($1, (select id from role where name = 'ur-role'), 'all')`, personID)
	mustExec(t, conn, `insert into notification_target (name, kind, principal_id, channels)
		values ('ur-to-person', 'principal', $1, '{inbox}')`, personID)
	mustExec(t, conn, `insert into notification_target (name, kind, principal_group_id, channels)
		values ('ur-to-group', 'principal_group', `+groupID+`, '{email}')`)

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	if left := snapshotRows(t, conn, schemaPrincipalNames, true); len(left) > 0 {
		t.Errorf("after the rename the schema still spells principal: %v", left)
	}
	userGroup := `(select id from user_group where name = 'ur-group')`
	for _, q := range []string{
		`select count(*) from users where id::text = '` + personID + `' and kind = 'person'`,
		`select count(*) from users where id::text = '` + machineID + `' and kind = 'machine'`,
		`select count(*) from users where id::text = '` + outpostID + `' and kind = 'machine'`,
		`select count(*) from person where user_id::text = '` + personID + `' and username = 'ur-person'`,
		`select count(*) from machine where user_id::text = '` + machineID + `' and name = 'ur-machine'`,
		`select count(*) from outpost where user_id::text = '` + outpostID + `'`,
		`select count(*) from user_group_member where user_id::text = '` + personID + `' and group_id = ` + userGroup,
		`select count(*) from user_grant where user_id::text = '` + personID + `'`,
		`select count(*) from notification_target where name = 'ur-to-person' and kind = 'user' and user_id::text = '` + personID + `'`,
		`select count(*) from notification_target where name = 'ur-to-group' and kind = 'user_group' and user_group_id = ` + userGroup,
		`select count(*) from role where name = 'ur-role'
		   and permissions = '{system:read,user:read,user_group:*,user_grant:create,user:purge:admin}'`,
		`select count(*) from pg_trigger where tgname = 'user_grant_owner_guard'`,
		`select count(*) from pg_proc where proname = 'assert_owner_grant_exists' and prosrc ~ 'from user_grant'`,
	} {
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d (%v), want 1", q, n, err)
		}
	}
	if _, err := conn.Exec(ctx, `insert into users (kind) values ('outpost')`); err == nil {
		t.Error("a user of kind outpost was accepted; the kinds are person and machine")
	}

	if err := migrate.RollbackBelow(dsn, userRenameVersion); err != nil {
		t.Fatalf("roll back the rename: %v", err)
	}
	for _, q := range []string{
		`select count(*) from principal where id::text = '` + personID + `' and kind = 'human'`,
		`select count(*) from principal where id::text = '` + machineID + `' and kind = 'service'`,
		`select count(*) from principal where id::text = '` + outpostID + `' and kind = 'outpost'`,
		`select count(*) from human where principal_id::text = '` + personID + `'`,
		`select count(*) from service where principal_id::text = '` + machineID + `'`,
		`select count(*) from principal_group_member where principal_id::text = '` + personID + `'`,
		`select count(*) from principal_grant where principal_id::text = '` + personID + `'`,
		`select count(*) from notification_target where name = 'ur-to-person' and kind = 'principal'`,
		`select count(*) from notification_target where name = 'ur-to-group' and kind = 'principal_group'`,
		`select count(*) from role where name = 'ur-role'
		   and permissions = '{system:read,principal:read,principal_group:*,principal_grant:create,principal:purge:admin}'`,
		`select count(*) from pg_class where relname = 'principal_grant_unique'`,
	} {
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("rolled back, %s: %d (%v), want 1", q, n, err)
		}
	}
}
