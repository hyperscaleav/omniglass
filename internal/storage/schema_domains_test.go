package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSampleProvenanceDomains pins each lane's provenance domain after #591
// re-admitted declared rows on the property series (a declared value IS a
// series row now, reversing the #460 narrowing for that lane): property accepts
// a declared row with no lineage, while metric still refuses declared until a
// slice folds declared numeric values into it.
func TestSampleProvenanceDomains(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// A minimal owner and a key per sink to satisfy the FKs: the metric sink keys
	// on metric_type, the property sink on property_type (the #587 lanes).
	var compID, mtID, ptID string
	if err := conn.QueryRow(ctx, `insert into component (name, product_id) values ('c1', (select id from product where name = 'generic-device')) returning id`).Scan(&compID); err != nil {
		t.Fatalf("component: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		insert into metric_type (name, official, data_type)
		values ('t-metric', false, 'float') returning id`).Scan(&mtID); err != nil {
		t.Fatalf("metric_type: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		insert into property_type (name, official, data_type)
		values ('t-prop', false, 'string') returning id`).Scan(&ptID); err != nil {
		t.Fatalf("property_type: %v", err)
	}

	// The metric lane has no declared writer, so its schema still refuses the
	// row (either narrowed constraint may report first).
	_, err = conn.Exec(ctx, `
		insert into metric (ts, owner_kind, component_id, metric_type_id, value, provenance, source)
		values (now(), 'component', $1, $2, 1, 'declared', 'test')`, compID, mtID)
	if err == nil || (!strings.Contains(err.Error(), "metric_provenance_check") && !strings.Contains(err.Error(), "metric_lineage_check")) {
		t.Errorf("metric declared insert error = %v, want a provenance or lineage CHECK violation", err)
	}

	// The property lane holds declared values as series rows (#591), with the
	// no-lineage arm: no rule, no causing event.
	if _, err := conn.Exec(ctx, `
		insert into property (ts, owner_kind, component_id, property_type_id, value, provenance, source)
		values (now(), 'component', $1, $2, to_jsonb('on'::text), 'declared', 'test')`, compID, ptID); err != nil {
		t.Errorf("property declared insert error = %v, want accepted (a declared value is a series row)", err)
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
