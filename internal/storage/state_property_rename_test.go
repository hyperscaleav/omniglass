package storage_test

import (
	"context"
	"testing"
)

// TestStateBecomesPropertyMigration asserts the #588 rename: the string-sample
// series is `property` (the record takes the noun, the catalog keeps the _type
// suffix, matching event_type/event), `state` is gone, every dependent
// identifier follows the table, and the two value columns (text value plus the
// value_json sidecar) converged on one `value jsonb NOT NULL`.
func TestStateBecomesPropertyMigration(t *testing.T) {
	ctx := context.Background()
	conn := connectMigrated(t)

	regclass := func(name string) *string {
		var reg *string
		if err := conn.QueryRow(ctx, `select to_regclass($1)::text`, name).Scan(&reg); err != nil {
			t.Fatalf("to_regclass(%s): %v", name, err)
		}
		return reg
	}

	// The table: property present, state gone.
	if regclass("property") == nil {
		t.Fatal("the table `property` does not exist; the state rename did not run")
	}
	if regclass("state") != nil {
		t.Fatal("the table `state` still exists; the rename left the old name behind")
	}

	// One value column, jsonb, NOT NULL; the text projection and the value_json
	// sidecar are gone.
	var dataType, nullable string
	if err := conn.QueryRow(ctx, `
		select data_type, is_nullable from information_schema.columns
		where table_name = 'property' and column_name = 'value'`).Scan(&dataType, &nullable); err != nil {
		t.Fatalf("property.value column: %v", err)
	}
	if dataType != "jsonb" {
		t.Errorf("property.value is %q, want jsonb (the one converged value column)", dataType)
	}
	if nullable != "NO" {
		t.Errorf("property.value is nullable; want NOT NULL (the declared tombstone is JSON null, not SQL NULL)")
	}
	var hasValueJSON bool
	if err := conn.QueryRow(ctx, `
		select exists (select 1 from information_schema.columns
		where table_name = 'property' and column_name = 'value_json')`).Scan(&hasValueJSON); err != nil {
		t.Fatalf("check value_json: %v", err)
	}
	if hasValueJSON {
		t.Error("property.value_json still exists; it folded into value and drops")
	}

	// Every dependent identifier follows the table: nothing on `property` may
	// still carry the state_ prefix.
	rows, err := conn.Query(ctx, `
		select conname from pg_constraint
		where conrelid = 'property'::regclass and conname like 'state\_%'`)
	if err != nil {
		t.Fatalf("list stale constraints: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan stale constraint: %v", err)
		}
		t.Errorf("constraint %s still carries the state_ prefix", name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stale constraints: %v", err)
	}

	// Spot-check the renamed inventory by name: one of each dependent-object
	// kind (PK, plain index, BRIN index, FK, CHECK, named NOT NULL, sequence).
	for _, c := range []string{
		"property_pkey",
		"property_property_type_id_fkey",
		"property_lineage_check",
		"property_owner_arc_check",
		"property_owner_kind_check",
		"property_provenance_check",
		"property_value_not_null",
	} {
		var present bool
		if err := conn.QueryRow(ctx, `
			select exists (select 1 from pg_constraint
			where conrelid = 'property'::regclass and conname = $1)`, c).Scan(&present); err != nil {
			t.Fatalf("check constraint %s: %v", c, err)
		}
		if !present {
			t.Errorf("constraint %s missing after the rename", c)
		}
	}
	for _, idx := range []string{"property_owner_idx", "property_ts_brin"} {
		var present bool
		if err := conn.QueryRow(ctx, `
			select exists (select 1 from pg_class where relkind = 'i' and relname = $1)`, idx).Scan(&present); err != nil {
			t.Fatalf("check index %s: %v", idx, err)
		}
		if !present {
			t.Errorf("index %s missing after the rename", idx)
		}
	}
	if regclass("property_id_seq") == nil {
		t.Error("sequence property_id_seq missing after the rename")
	}
	if regclass("state_id_seq") != nil {
		t.Error("sequence state_id_seq still exists after the rename")
	}

	// The series behaves on the one column: a structured value is stored as
	// itself (a bool is a bool, not its text projection), and the tombstone's
	// JSON null is a value, not SQL NULL.
	mustExec(t, conn, `insert into property_type (name, data_type) values ('rename-prop', 'bool')`)
	mustExec(t, conn, `insert into component (name, product_id) values ('rename-c1', (select id from product where name = 'generic-device'))`)
	mustExec(t, conn, `
		insert into property (owner_kind, component_id, property_type_id, instance, provenance, value)
		values ('component', (select id from component where name = 'rename-c1'),
		        (select id from property_type where name = 'rename-prop'), '', 'declared', to_jsonb(true))`)
	var typ string
	if err := conn.QueryRow(ctx, `
		select jsonb_typeof(value) from property
		where component_id = (select id from component where name = 'rename-c1')
		order by id desc limit 1`).Scan(&typ); err != nil {
		t.Fatalf("read structured value: %v", err)
	}
	if typ != "boolean" {
		t.Errorf("stored declared bool reads back as jsonb %q, want boolean (value is the exact jsonb)", typ)
	}
	if _, err := conn.Exec(ctx, `
		insert into property (owner_kind, component_id, property_type_id, instance, provenance, value)
		values ('component', (select id from component where name = 'rename-c1'),
		        (select id from property_type where name = 'rename-prop'), '', 'declared', 'null'::jsonb)`); err != nil {
		t.Errorf("tombstone insert (value = JSON null) rejected: %v", err)
	}

	// The lineage check kept its shape across the rename: a declared row must
	// not carry an event.
	if _, err := conn.Exec(ctx, `
		insert into property (owner_kind, component_id, property_type_id, instance, provenance, value, event_id)
		values ('component', (select id from component where name = 'rename-c1'),
		        (select id from property_type where name = 'rename-prop'), '', 'declared', to_jsonb(false), 1)`); err == nil {
		t.Error("property accepted a declared row with event lineage; property_lineage_check is missing or wrong")
	}
}
