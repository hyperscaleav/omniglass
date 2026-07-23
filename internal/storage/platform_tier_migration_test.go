package storage_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// platformTierVersion is the migration under test: the rename of the cascade's
// least-specific binding tier from 'global' to 'platform'.
const platformTierVersion = "20260722190000"

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

// TestPlatformTierRenamePreservesResolution asserts an upgrade over a database
// holding 'global' rows leaves every effective value identical: the rename moves a
// name, not a rung, so nothing may resolve differently.
//
// The harness has no way to migrate up to an arbitrary version, so the database
// is stood at the pre-rename schema by migrating fully and rolling back to just
// before the rename. That runs the real down blocks, seeds real 'global' rows,
// and then runs the real up blocks over them, which is the upgrade an existing
// operator database actually takes.
func TestPlatformTierRenamePreservesResolution(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	conn := connectDSN(t, dsn)

	if got := latestMigration(t, conn); got < platformTierVersion {
		t.Fatalf("latest applied migration = %s, want the rename %s applied", got, platformTierVersion)
	}
	rollbackBelow(t, conn, dsn, platformTierVersion)

	// After the rollback the leaf registries are back to slug primary keys.
	mustExec(t, conn, `insert into location_type (id, display_name) values ('site', 'Site') on conflict do nothing`)
	mustExec(t, conn, `insert into location (name, location_type) values ('ceres', 'site')`)
	mustExec(t, conn, `
		insert into component (name, location_id)
		select 'ceres_codec', id from location where name = 'ceres'`)

	// A tier value a location row must still beat, and one nothing overrides.
	mustExec(t, conn, `
		insert into variable (name, value_type, owner_kind, value)
		values ('poll_interval', 'int', 'global', '60'::jsonb),
		       ('snmp_community', 'string', 'global', '"public"'::jsonb)`)
	mustExec(t, conn, `
		insert into variable (name, value_type, owner_kind, location_id, value)
		select 'poll_interval', 'int', 'location', id, '30'::jsonb from location where name = 'ceres'`)
	mustExec(t, conn, `insert into setting_override (scope, namespace, doc) values ('global', 'ui', '{"theme":"dark"}'::jsonb)`)

	componentID := scalar(t, conn, `select id::text from component where name = 'ceres_codec'`)
	before := resolveAllVariables(t, conn, componentID, "global")

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over the seeded rows: %v", err)
	}

	after := resolveAllVariables(t, conn, componentID, "platform")

	if len(before) != len(after) {
		t.Fatalf("resolved key count changed: %d before, %d after", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s resolved to %v before and %v after", k, v, after[k])
		}
	}
	if after["poll_interval"] != "30" {
		t.Errorf("poll_interval = %v, want the location value 30 to still beat the tier", after["poll_interval"])
	}
	if after["snmp_community"] != `"public"` {
		t.Errorf("snmp_community = %v, want the tier value to still resolve", after["snmp_community"])
	}

	// No row was left behind at the old name, on any table that carries the tier.
	if n := count(t, conn, `select count(*) from variable where owner_kind = 'global'`); n != 0 {
		t.Errorf("%d variable rows still at owner_kind 'global'", n)
	}
	if n := count(t, conn, `select count(*) from setting_override where scope = 'global'`); n != 0 {
		t.Errorf("%d setting_override rows still at scope 'global'", n)
	}
	if got := scalar(t, conn, `select scope from setting_override where namespace = 'ui'`); got != "platform" {
		t.Errorf("setting_override scope = %s, want platform", got)
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

// rollbackBelow rolls migrations back one at a time until the newest applied one
// is older than version, leaving the database at the schema immediately before it.
// Looping (rather than rolling back a fixed count) keeps the test honest as
// migrations land on top of the one under test.
func rollbackBelow(t *testing.T, conn *pgx.Conn, dsn, version string) {
	t.Helper()
	for latestMigration(t, conn) >= version {
		if err := migrate.RollbackOne(dsn); err != nil {
			t.Fatalf("roll back below %s: %v", version, err)
		}
	}
}

func latestMigration(t *testing.T, conn *pgx.Conn) string {
	t.Helper()
	return scalar(t, conn, `select max(version) from schema_migrations`)
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

// resolveAllVariables runs the structural cascade for a component and returns the
// winning value per name. tier names the least-specific rung, the only thing the
// rename moves, so resolving with 'global' before and 'platform' after must produce
// the same map. The query is a COPY of storage.resolveVariablesSQL (unexported, so
// it cannot be called from this package) with the rung's name substituted.
//
// Being a copy, it does not pin the production bands: the 0 below is this test's
// own, and changing the band in variables.go would leave this green. The rung's
// place in the ordering is guarded where it belongs, by TestVariableCascadeResolve
// and TestSecretCascadeResolve, which drive the real Gateway and assert a location,
// system, and component binding each beat the platform rung. What this helper pins
// is narrower and is the migration's actual contract: renaming the rung changes no
// resolved value.
func resolveAllVariables(t *testing.T, conn *pgx.Conn, componentID, tier string) map[string]string {
	t.Helper()
	sql := fmt.Sprintf(`
with recursive
target as (
    -- The system band comes from the component's PRIMARY membership, not from a
    -- column: component.system_id was dropped when the cascade moved onto
    -- system_member (ADR-0051).
    select c.id,
           (select sm.system_id from system_member sm
             where sm.component_id = c.id and sm.is_primary) as system_id,
           c.location_id
      from component c where c.id = $1
),
comp_chain(id, depth) as (
    select id, 0 from component where id = $1
    union all
    select c.parent_id, cc.depth + 1
    from component c join comp_chain cc on c.id = cc.id
    where c.parent_id is not null
) cycle id set comp_cyc using comp_path,
sys_chain(id, depth) as (
    select system_id, 0 from target where system_id is not null
    union all
    select s.parent_id, sc.depth + 1
    from system s join sys_chain sc on s.id = sc.id
    where s.parent_id is not null
) cycle id set sys_cyc using sys_path,
loc_chain(id, depth) as (
    select location_id, 0 from target where location_id is not null
    union all
    select l.parent_id, lc.depth + 1
    from location l join loc_chain lc on l.id = lc.id
    where l.parent_id is not null
) cycle id set loc_cyc using loc_path,
owners(owner_kind, owner_id, band, depth) as (
                select '%s',       null::uuid, 0, 0
    union all   select 'location',  id,        1, depth from loc_chain
    union all   select 'system',    id,        2, depth from sys_chain
    union all   select 'component', id,        3, depth from comp_chain
),
ranked as (
    select v.name, v.value, o.band, o.depth,
           row_number() over (partition by v.name order by o.band desc, o.depth asc) as rnk
    from variable v
    join owners o
      on o.owner_kind = v.owner_kind
     and o.owner_id is not distinct from coalesce(v.component_id, v.system_id, v.location_id)
)
select name, value::text from ranked where rnk = 1 order by name`, tier)

	rows, err := conn.Query(context.Background(), sql, componentID)
	if err != nil {
		t.Fatalf("resolve variables at tier %s: %v", tier, err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatalf("scan resolved variable: %v", err)
		}
		out[name] = value
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("resolve variables: %v", err)
	}
	return out
}
