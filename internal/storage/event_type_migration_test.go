package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestEventTypeFamilyMigration asserts the #395 repoint: the event_type registry
// exists, the log kind has left property_type, and the event table is repointed
// onto event_type with the new origin/causation columns and no property_type_id.
func TestEventTypeFamilyMigration(t *testing.T) {
	ctx := context.Background()
	conn := connectMigrated(t)

	// The event_type registry exists and takes a row, keyed uniquely by name.
	mustExec(t, conn, `insert into event_type (name, display_name) values ('call-started', 'Call started')`)
	if _, err := conn.Exec(ctx, `insert into event_type (name) values ('call-started')`); err == nil {
		t.Error("expected event_type.name unique to reject a duplicate")
	}

	// The kind discriminator is gone entirely (#587 finished what #395 started):
	// the log kind left first, then the split retired the column, so a plain
	// insert works and the column must not exist at all.
	mustExec(t, conn, `insert into property_type (name, data_type) values ('device-log', 'string')`)
	var kindPresent bool
	if err := conn.QueryRow(ctx,
		`select exists (select 1 from information_schema.columns
		 where table_name = 'property_type' and column_name = 'kind')`).Scan(&kindPresent); err != nil {
		t.Fatalf("read property_type kind column: %v", err)
	}
	if kindPresent {
		t.Error("property_type still carries the kind column; the lane split retired it")
	}
	mustExec(t, conn, `insert into metric_type (name, data_type) values ('cpu-load', 'float')`)

	// event is repointed onto event_type: event_type_id present and NOT NULL, the
	// old property_type_id gone, and the origin/causation columns present.
	col := func(name string) (nullable string, present bool) {
		err := conn.QueryRow(ctx,
			`select is_nullable from information_schema.columns where table_name = 'event' and column_name = $1`,
			name).Scan(&nullable)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false
		}
		if err != nil {
			t.Fatalf("read event column %s: %v", name, err)
		}
		return nullable, true
	}
	if n, ok := col("event_type_id"); !ok || n != "NO" {
		t.Errorf("event.event_type_id: want present and NOT NULL, got nullable=%q present=%v", n, ok)
	}
	// The lineage columns (ADR-0066): caused_by_event_id is renamed to
	// source_event_id, and source_log_line_id + derived_by_rule_id are added.
	for _, c := range []string{"origin", "source_event_id", "correlation_id", "source_log_line_id", "derived_by_rule_id"} {
		if _, ok := col(c); !ok {
			t.Errorf("event.%s: missing after migration", c)
		}
	}
	if _, ok := col("caused_by_event_id"); ok {
		t.Error("event.caused_by_event_id should be renamed to source_event_id")
	}
	if _, ok := col("property_type_id"); ok {
		t.Error("event.property_type_id should be dropped after the repoint")
	}

	// The origin check constraint is present.
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from pg_constraint where conname = 'event_origin_check'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("event_origin_check: want exactly 1, got %d (err %v)", n, err)
	}
}
