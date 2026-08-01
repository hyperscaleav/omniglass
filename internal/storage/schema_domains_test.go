package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSampleProvenanceDomainsNarrowed pins #460: `declared` is impossible on
// the sample sinks (a declared value lives only in the property cache), so the
// metric and state provenance CHECKs refuse it at the schema, not just by
// writer convention.
func TestSampleProvenanceDomainsNarrowed(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// A minimal owner and key to satisfy the FKs.
	var compID, ptID string
	if err := conn.QueryRow(ctx, `insert into component (name) values ('c1') returning id`).Scan(&compID); err != nil {
		t.Fatalf("component: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		insert into property_type (name, official, kind, data_type)
		values ('t.metric', false, 'metric', 'float') returning id`).Scan(&ptID); err != nil {
		t.Fatalf("property_type: %v", err)
	}

	for _, tbl := range []string{"metric", "state"} {
		val := "1"
		if tbl == "state" {
			val = "'on'"
		}
		_, err := conn.Exec(ctx, `
			insert into `+tbl+` (ts, owner_kind, component_id, property_type_id, value, provenance, source)
			values (now(), 'component', $1, $2, `+val+`, 'declared', 'test')`, compID, ptID)
		// Either narrowed constraint may report first; the claim is that the
		// schema refuses the row.
		if err == nil || (!strings.Contains(err.Error(), tbl+"_provenance_check") && !strings.Contains(err.Error(), tbl+"_lineage_check")) {
			t.Errorf("%s declared insert error = %v, want a provenance or lineage CHECK violation", tbl, err)
		}
	}
}

// TestRoleConstraintNamesCorrected pins the uuid-refactor cleanup: the role
// NOT NULL constraints carry names that match their columns, so the next
// rename migration cannot be misled by a constraint whose name says id on the
// name column.
func TestRoleConstraintNamesCorrected(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	names := map[string]bool{}
	rows, err := conn.Query(ctx, `
		select con.conname
		from pg_constraint con
		join pg_class rel ON rel.oid = con.conrelid
		where rel.relname = 'role' and con.contype = 'n'`)
	if err != nil {
		t.Fatalf("query constraints: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names[n] = true
	}
	if !names["role_name_not_null"] || !names["role_id_not_null"] || names["role_id_not_null1"] {
		t.Errorf("role NOT NULL constraint names = %v, want role_name_not_null and role_id_not_null, no digit-suffixed artifact", names)
	}
}

// TestGrantGroupScopeKindRefusedBySchema pins #461: scope_kind='group' (a
// group as the scope TARGET) is unbuilt and refused by every code path, so
// the schema refuses it too; a future gateway bug cannot mint a grant no
// resolver honors. The grant SUBJECT (group_id, a group holding a grant)
// stays.
func TestGrantGroupScopeKindRefusedBySchema(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var principalID, roleID string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('service') returning id`).Scan(&principalID); err != nil {
		t.Fatalf("principal: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		insert into role (name, official, permissions, inherits)
		values ('t-role', false, '{}'::text[], '{}') returning id`).Scan(&roleID); err != nil {
		t.Fatalf("role: %v", err)
	}
	_, err = conn.Exec(ctx, `
		insert into principal_grant (principal_id, role_id, scope_kind, scope_id)
		values ($1, $2, 'group', '00000000-0000-0000-0000-000000000000')`, principalID, roleID)
	if err == nil || !strings.Contains(err.Error(), "principal_grant_scope_kind_check") {
		t.Errorf("group-scoped grant insert error = %v, want principal_grant_scope_kind_check violation", err)
	}
}
