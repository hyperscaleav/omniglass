package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// commandFixture stands up the minimal rows a command needs (an owner, both
// catalog lanes, a command_type, and a caused event) and returns their ids.
type commandFixture struct {
	compID, mtID, ptID, ctID, etID string
	evID                           int64
}

func newCommandFixture(t *testing.T, conn *pgx.Conn) commandFixture {
	t.Helper()
	ctx := context.Background()
	var f commandFixture
	if err := conn.QueryRow(ctx, `insert into component (name, product_id) values ('c1', (select id from product where name = 'generic-device')) returning id`).Scan(&f.compID); err != nil {
		t.Fatalf("component: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into metric_type (name, official, data_type) values ('t-metric', false, 'float') returning id`).Scan(&f.mtID); err != nil {
		t.Fatalf("metric_type: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into property_type (name, official, data_type) values ('t-prop', false, 'string') returning id`).Scan(&f.ptID); err != nil {
		t.Fatalf("property_type: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into command_type (name, target_property_type_id) values ('t-cmd', $1) returning id`, f.ptID).Scan(&f.ctID); err != nil {
		t.Fatalf("command_type: %v", err)
	}
	if err := conn.QueryRow(ctx, `insert into event_type (name) values ('t-ev') returning id`).Scan(&f.etID); err != nil {
		t.Fatalf("event_type: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into event (owner_kind, component_id, event_type_id) values ('component', $1, $2) returning id`,
		f.compID, f.etID).Scan(&f.evID); err != nil {
		t.Fatalf("event: %v", err)
	}
	return f
}

// TestCommandStatusMigration asserts the #590 command-status schema: a command
// records its status (default issued, a fixed terminal domain) and the moment
// it settled, with the two coupled (settled_at is null exactly while issued).
func TestCommandStatusMigration(t *testing.T) {
	ctx := context.Background()
	conn := connectMigrated(t)
	f := newCommandFixture(t, conn)

	var (
		cmdID     int64
		status    string
		settledAt *string
	)
	if err := conn.QueryRow(ctx,
		`insert into command (owner_kind, component_id, command_type_id, caused_event_id)
		 values ('component', $1, $2, $3) returning id, status, settled_at::text`,
		f.compID, f.ctID, f.evID).Scan(&cmdID, &status, &settledAt); err != nil {
		t.Fatalf("insert command: %v", err)
	}
	if status != "issued" || settledAt != nil {
		t.Errorf("fresh command: status=%q settled_at=%v, want issued with no settled_at", status, settledAt)
	}

	// The status domain is fixed (settled_at set so only the domain can refuse).
	if _, err := conn.Exec(ctx, `update command set status = 'bogus', settled_at = now() where id = $1`, cmdID); err == nil ||
		!strings.Contains(err.Error(), "command_status_check") {
		t.Errorf("bogus status error = %v, want command_status_check violation", err)
	}
	// A terminal status records when it settled; a terminal write without the
	// moment is refused.
	if _, err := conn.Exec(ctx, `update command set status = 'settled' where id = $1`, cmdID); err == nil {
		t.Error("terminal status without settled_at accepted, want refused")
	}
	for _, terminal := range []string{"settled", "failed", "timed-out"} {
		if _, err := conn.Exec(ctx,
			`update command set status = $1, settled_at = now() where id = $2`, terminal, cmdID); err != nil {
			t.Errorf("terminal status %q refused: %v", terminal, err)
		}
	}
}

// TestCommandTypeTargetArcMigration asserts the two-armed target: command_type
// may target a property or a metric, never both.
func TestCommandTypeTargetArcMigration(t *testing.T) {
	ctx := context.Background()
	conn := connectMigrated(t)
	f := newCommandFixture(t, conn)

	// A metric-only target is a valid arm.
	mustExec(t, conn, `insert into command_type (name, target_metric_type_id) values ('t-cmd-metric', $1)`, f.mtID)
	// Both arms at once are refused.
	if _, err := conn.Exec(ctx,
		`update command_type set target_metric_type_id = $1 where id = $2`, f.mtID, f.ctID); err == nil ||
		!strings.Contains(err.Error(), "command_type_target_arc_check") {
		t.Errorf("both-target error = %v, want command_type_target_arc_check violation", err)
	}
	// The metric arm is a real FK.
	if _, err := conn.Exec(ctx,
		`insert into command_type (name, target_metric_type_id) values ('t-cmd-dangling', gen_random_uuid())`); err == nil {
		t.Error("dangling target_metric_type_id accepted, want FK violation")
	}
}

// TestIntendedLineageNamesCommand asserts the repointed lineage CHECK on both
// series tables: an intended row must name the command that opened it
// (command_id), while the caused event stays a stamped-but-optional column.
// Every other provenance still refuses command lineage.
func TestIntendedLineageNamesCommand(t *testing.T) {
	ctx := context.Background()
	conn := connectMigrated(t)
	f := newCommandFixture(t, conn)

	var cmdID int64
	if err := conn.QueryRow(ctx,
		`insert into command (owner_kind, component_id, command_type_id, caused_event_id)
		 values ('component', $1, $2, $3) returning id`, f.compID, f.ctID, f.evID).Scan(&cmdID); err != nil {
		t.Fatalf("insert command: %v", err)
	}

	// property: an intended row carries its command; the event is optional.
	mustExec(t, conn, `insert into property (owner_kind, component_id, property_type_id, provenance, value, command_id)
		values ('component', $1, $2, 'intended', to_jsonb('hdmi2'::text), $3)`, f.compID, f.ptID, cmdID)
	// An intended row with only event lineage is the old shape; refused now.
	if _, err := conn.Exec(ctx,
		`insert into property (owner_kind, component_id, property_type_id, provenance, value, event_id)
		 values ('component', $1, $2, 'intended', to_jsonb('hdmi2'::text), $3)`, f.compID, f.ptID, f.evID); err == nil ||
		!strings.Contains(err.Error(), "property_lineage_check") {
		t.Errorf("event-only intended property error = %v, want property_lineage_check violation", err)
	}
	// Command lineage belongs to the intended arm only.
	if _, err := conn.Exec(ctx,
		`insert into property (owner_kind, component_id, property_type_id, provenance, value, command_id)
		 values ('component', $1, $2, 'observed', to_jsonb('hdmi2'::text), $3)`, f.compID, f.ptID, cmdID); err == nil ||
		!strings.Contains(err.Error(), "property_lineage_check") {
		t.Errorf("observed property with command lineage error = %v, want property_lineage_check violation", err)
	}
	// The FK is real: a command that does not exist cannot be named.
	if _, err := conn.Exec(ctx,
		`insert into property (owner_kind, component_id, property_type_id, provenance, value, command_id)
		 values ('component', $1, $2, 'intended', to_jsonb('hdmi2'::text), $3)`, f.compID, f.ptID, cmdID+999999); err == nil {
		t.Error("dangling property.command_id accepted, want FK violation")
	}

	// metric: the same repoint on the numeric lane.
	mustExec(t, conn, `insert into metric (owner_kind, component_id, metric_type_id, provenance, value, command_id)
		values ('component', $1, $2, 'intended', 50, $3)`, f.compID, f.mtID, cmdID)
	if _, err := conn.Exec(ctx,
		`insert into metric (owner_kind, component_id, metric_type_id, provenance, value, event_id)
		 values ('component', $1, $2, 'intended', 50, $3)`, f.compID, f.mtID, f.evID); err == nil ||
		!strings.Contains(err.Error(), "metric_lineage_check") {
		t.Errorf("event-only intended metric error = %v, want metric_lineage_check violation", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into metric (owner_kind, component_id, metric_type_id, provenance, value, command_id)
		 values ('component', $1, $2, 'observed', 50, $3)`, f.compID, f.mtID, cmdID); err == nil ||
		!strings.Contains(err.Error(), "metric_lineage_check") {
		t.Errorf("observed metric with command lineage error = %v, want metric_lineage_check violation", err)
	}
}

// TestIntendedLineageBackfill proves the migration repoints existing intended
// rows from their event to that event's command: stand the database at the
// previous schema, write an intended row the old way (event lineage only),
// and migrate forward over it.
func TestIntendedLineageBackfill(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	// Below the command-status migration itself, wherever it now sits in the
	// stack: this test writes rows in the shape that migration retired.
	if err := migrate.RollbackBelow(dsn, "20260805210000"); err != nil {
		t.Fatalf("rollback below the lineage migration: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()
	f := newCommandFixture(t, conn)

	var cmdID int64
	if err := conn.QueryRow(ctx,
		`insert into command (owner_kind, component_id, command_type_id, caused_event_id)
		 values ('component', $1, $2, $3) returning id`, f.compID, f.ctID, f.evID).Scan(&cmdID); err != nil {
		t.Fatalf("insert command: %v", err)
	}
	var rowID int64
	if err := conn.QueryRow(ctx,
		`insert into property (owner_kind, component_id, property_type_id, provenance, value, event_id)
		 values ('component', $1, $2, 'intended', to_jsonb('hdmi2'::text), $3) returning id`,
		f.compID, f.ptID, f.evID).Scan(&rowID); err != nil {
		t.Fatalf("insert old-shape intended row: %v", err)
	}

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over old-shape rows: %v", err)
	}
	var gotCmd *int64
	if err := conn.QueryRow(ctx, `select command_id from property where id = $1`, rowID).Scan(&gotCmd); err != nil {
		t.Fatalf("read backfilled row: %v", err)
	}
	if gotCmd == nil || *gotCmd != cmdID {
		t.Errorf("backfilled command_id = %v, want %d (the event's command)", gotCmd, cmdID)
	}
}

// TestIntendedLineageBackfillRefusesOrphan proves the migration refuses loudly
// on an intended row whose command cannot be recovered from its event, instead
// of guessing or leaving a row the new CHECK would refuse.
func TestIntendedLineageBackfillRefusesOrphan(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	// Below the command-status migration itself, wherever it now sits in the
	// stack: this test writes rows in the shape that migration retired.
	if err := migrate.RollbackBelow(dsn, "20260805210000"); err != nil {
		t.Fatalf("rollback below the lineage migration: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()
	f := newCommandFixture(t, conn)

	// An intended row whose event no command claims: unrecoverable lineage.
	if _, err := conn.Exec(ctx,
		`insert into property (owner_kind, component_id, property_type_id, provenance, value, event_id)
		 values ('component', $1, $2, 'intended', to_jsonb('hdmi2'::text), $3)`, f.compID, f.ptID, f.evID); err != nil {
		t.Fatalf("insert orphan intended row: %v", err)
	}
	if err := migrate.Run(dsn); err == nil {
		t.Fatal("migrate over an unrecoverable intended row succeeded, want a loud refusal")
	} else if !strings.Contains(err.Error(), "intended") {
		t.Errorf("refusal error = %v, want it to name the intended row", err)
	}
}
