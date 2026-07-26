package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestCommandPillarMigration asserts the #396 schema: the command_type registry (with
// its settle window and target-property pointer) and the command invocation table
// (over the owner arc) exist with the expected columns and constraints.
func TestCommandPillarMigration(t *testing.T) {
	ctx := context.Background()
	conn := connectMigrated(t)

	// The command_type registry exists and is keyed uniquely by name.
	mustExec(t, conn, `insert into command_type (name, display_name, settle_window_seconds) values ('set_input', 'Set input', 30)`)
	if _, err := conn.Exec(ctx, `insert into command_type (name) values ('set_input')`); err == nil {
		t.Error("expected command_type.name unique to reject a duplicate")
	}

	// command_type carries the driver facts: a settle window and an optional target
	// property.
	col := func(table, name string) (present bool) {
		var n int
		if err := conn.QueryRow(ctx,
			`select count(*) from information_schema.columns where table_name = $1 and column_name = $2`,
			table, name).Scan(&n); err != nil {
			t.Fatalf("read %s.%s: %v", table, name, err)
		}
		return n == 1
	}
	for _, c := range []string{"settle_window_seconds", "target_property_type_id", "params_schema"} {
		if !col("command_type", c) {
			t.Errorf("command_type.%s: missing", c)
		}
	}
	for _, c := range []string{"command_type_id", "caused_event_id", "owner_kind", "instance", "params", "actor", "ts"} {
		if !col("command", c) {
			t.Errorf("command.%s: missing", c)
		}
	}

	var ctID string
	if err := conn.QueryRow(ctx, `select id from command_type where name = 'set_input'`).Scan(&ctID); err != nil {
		t.Fatalf("command_type id: %v", err)
	}

	// The owner arc CHECK rejects a component owner with no component_id, and the
	// owner_kind CHECK rejects an unknown kind (the same exclusive-arc primitive as
	// metric/state/event).
	if _, err := conn.Exec(ctx, `insert into command (owner_kind, command_type_id) values ('component', $1)`, ctID); err == nil {
		t.Error("expected command_owner_arc_check to reject a component owner with no component_id")
	}
	if _, err := conn.Exec(ctx, `insert into command (owner_kind, command_type_id) values ('bogus', $1)`, ctID); err == nil {
		t.Error("expected command_owner_kind_check to reject an unknown owner_kind")
	}

	// A command with an unknown command_type violates the FK.
	if _, err := conn.Exec(ctx,
		`insert into command (owner_kind, component_id, command_type_id) values ('component', gen_random_uuid(), gen_random_uuid())`); err == nil {
		t.Error("expected the command_type FK (and the component FK) to reject unknown references")
	} else if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unexpected: %v", err)
	}
}
