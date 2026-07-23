package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestPlatformTierRename asserts the migration moved the tier value: 'platform'
// is legal on every owner_kind that carries the tier, 'global' is not, and the
// partial unique index moved with it.
func TestPlatformTierRename(t *testing.T) {
	ctx := context.Background()
	conn := connectMigrated(t)

	mustExec(t, conn, `insert into location_type (name, display_name) values ('site', 'Site') on conflict do nothing`)
	mustExec(t, conn, `insert into secret_type (name, display_name) values ('password', 'Password') on conflict do nothing`)
	mustExec(t, conn, `insert into tag (name) values ('environment')`)

	// The renamed value is accepted on every table that carries the tier.
	mustExec(t, conn, `
		insert into variable (name, value_type, owner_kind, value)
		values ('snmp_community', 'string', 'platform', '"public"'::jsonb)`)
	mustExec(t, conn, `
		insert into secret (name, secret_type, owner_kind)
		values ('snmp_auth', (select id from secret_type where name = 'password'), 'platform')`)
	mustExec(t, conn, `
		insert into tag_binding (tag_id, owner_kind, value)
		select id, 'platform', 'prod' from tag where name = 'environment'`)

	// The old value is rejected by the check constraint on every one of them.
	for _, stmt := range []string{
		`insert into variable (name, value_type, owner_kind, value) values ('legacy', 'string', 'global', '"x"'::jsonb)`,
		`insert into secret (name, secret_type, owner_kind) values ('legacy', 'password', 'global')`,
		`insert into tag_binding (tag_id, owner_kind, value) select id, 'global', 'x' from tag where name = 'environment'`,
	} {
		if _, err := conn.Exec(ctx, stmt); err == nil {
			t.Errorf("expected the check constraint to reject owner_kind 'global': %s", stmt)
		}
	}

	// The partial unique indexes moved with it: one platform row per name/key.
	for _, stmt := range []string{
		`insert into variable (name, value_type, owner_kind, value) values ('snmp_community', 'string', 'platform', '"other"'::jsonb)`,
		`insert into secret (name, secret_type, owner_kind) values ('snmp_auth', 'password', 'platform')`,
		`insert into tag_binding (tag_id, owner_kind, value) select id, 'platform', 'dev' from tag where name = 'environment'`,
	} {
		if _, err := conn.Exec(ctx, stmt); err == nil {
			t.Errorf("expected the platform partial unique index to reject a duplicate: %s", stmt)
		}
	}

	// setting_override carries the tier as scope, under its own check constraint.
	mustExec(t, conn, `insert into setting_override (scope, namespace) values ('platform', 'ui')`)
	if _, err := conn.Exec(ctx,
		`insert into setting_override (scope, namespace) values ('global', 'keybindings')`); err == nil {
		t.Error("expected the check constraint to reject setting_override.scope 'global'")
	}
}

// TestPlatformTierIndexNames pins the index names the rename produced. A
// `drop index if exists` on a name that never existed succeeds silently, so the
// names are asserted rather than assumed.
func TestPlatformTierIndexNames(t *testing.T) {
	conn := connectMigrated(t)

	for _, name := range []string{"variable_platform_name", "secret_platform_name", "tag_binding_platform_key"} {
		if !indexExists(t, conn, name) {
			t.Errorf("index %s missing after migrate", name)
		}
	}
	for _, name := range []string{"variable_global_name", "secret_global_name", "tag_binding_global_key"} {
		if indexExists(t, conn, name) {
			t.Errorf("index %s still present after migrate", name)
		}
	}
	// The renamed CHECKs kept their names, so the migration's drops hit real
	// constraints rather than silently doing nothing.
	for _, name := range []string{
		"variable_owner_kind_check", "variable_owner_arc",
		"secret_owner_kind_check", "secret_owner_arc",
		"tag_binding_owner_kind_check", "tag_binding_owner_arc",
	} {
		if !constraintExists(t, conn, name) {
			t.Errorf("check constraint %s missing after migrate", name)
		}
	}
}

// --- helpers -----------------------------------------------------------------

func connectMigrated(t *testing.T) *pgx.Conn {
	t.Helper()
	return connectDSN(t, storagetest.NewDSN(t))
}

func connectDSN(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func mustExec(t *testing.T, conn *pgx.Conn, sql string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

func scalar(t *testing.T, conn *pgx.Conn, sql string) string {
	t.Helper()
	var out string
	if err := conn.QueryRow(context.Background(), sql).Scan(&out); err != nil {
		t.Fatalf("query %s: %v", sql, err)
	}
	return out
}

func count(t *testing.T, conn *pgx.Conn, sql string) int {
	t.Helper()
	var out int
	if err := conn.QueryRow(context.Background(), sql).Scan(&out); err != nil {
		t.Fatalf("query %s: %v", sql, err)
	}
	return out
}

func indexExists(t *testing.T, conn *pgx.Conn, name string) bool {
	t.Helper()
	var out bool
	err := conn.QueryRow(context.Background(),
		`select exists (select 1 from pg_indexes where indexname = $1)`, name).Scan(&out)
	if err != nil {
		t.Fatalf("probe index %s: %v", name, err)
	}
	return out
}

func constraintExists(t *testing.T, conn *pgx.Conn, name string) bool {
	t.Helper()
	var out bool
	err := conn.QueryRow(context.Background(),
		`select exists (select 1 from pg_constraint where conname = $1 and contype = 'c')`, name).Scan(&out)
	if err != nil {
		t.Fatalf("probe constraint %s: %v", name, err)
	}
	return out
}
