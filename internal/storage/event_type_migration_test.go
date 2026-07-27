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
	mustExec(t, conn, `insert into event_type (name, display_name) values ('call.started', 'Call started')`)
	if _, err := conn.Exec(ctx, `insert into event_type (name) values ('call.started')`); err == nil {
		t.Error("expected event_type.name unique to reject a duplicate")
	}

	// The log kind left property_type: metric is accepted, log is rejected.
	mustExec(t, conn, `insert into property_type (name, kind, data_type) values ('cpu.load', 'metric', 'float')`)
	if _, err := conn.Exec(ctx, `insert into property_type (name, kind, data_type) values ('log.line', 'log', 'string')`); err == nil {
		t.Error("expected property_type.kind check to reject 'log'")
	}

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
	for _, c := range []string{"origin", "caused_by_event_id", "correlation_id"} {
		if _, ok := col(c); !ok {
			t.Errorf("event.%s: missing after migration", c)
		}
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
