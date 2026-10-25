package storage_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/db"
)

// The one-time backfill that holds the three newly reserved command outcome
// events to their class (#1155, ADR-1155): before this release an operator
// could create command-settled, command-failed or command-timed-out and a
// push or an event rule could write one, and the boot seed takes such a type
// over. An event of those types no command points at is retired whole into
// retired_sample with its reason, never deleted; one a command's outcome
// wrote stays. Driven from the embedded migration text, so the test runs the
// statements the migration applies.

const outcomeBackfill = "migrations/20261025120100_command_outcome_backfill.sql"

func outcomeBackfillSQL(t *testing.T) (up, down string) {
	t.Helper()
	raw, err := db.FS.ReadFile(outcomeBackfill)
	if err != nil {
		t.Fatalf("read the backfill: %v", err)
	}
	up, down, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatal("the backfill has no -- migrate:down")
	}
	return strings.TrimPrefix(up, "-- migrate:up"), down
}

func TestOutcomeBackfillRetiresEventsNoCommandWrote(t *testing.T) {
	r := newStatusRig(t, valueReply)
	up, down := outcomeBackfillSQL(t)
	// A command's own outcome event, which stays.
	id := r.issue(t, "hdmi3")
	r.dispatch(t, id)
	if err := r.gw.RecordCommandExecution(r.ctx, "edge", id, "", "INPUT hdmi1"); err != nil {
		t.Fatal(err)
	}
	owned := *r.read(t, id).SettledEventID
	// What a release before the ruling could have left: a pushed
	// command-failed nobody's command wrote.
	var forged int64
	if err := r.conn.QueryRow(r.ctx, `insert into event (owner_kind, component_id, event_type_id, instance, origin, message, provenance, source)
		values ('component', $1, (select id from event_type where name = 'command-failed'), '', 'caught', 'forged', 'observed', 'push')
		returning id`, r.compID).Scan(&forged); err != nil {
		t.Fatal(err)
	}
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := r.conn.QueryRow(r.ctx, sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for run := 0; run < 2; run++ {
		if _, err := r.conn.Exec(r.ctx, up); err != nil {
			t.Fatalf("run %d of the backfill: %v", run, err)
		}
		if n := count(`select count(*) from event where id = $1`, forged); n != 0 {
			t.Fatalf("run %d: the forged event is still served", run)
		}
		if n := count(`select count(*) from retired_sample where lane = 'event' and sample_id = $1 and reason <> ''`, forged); n != 1 {
			t.Fatalf("run %d: the forged event is not on record in retired_sample", run)
		}
		if n := count(`select count(*) from event where id = $1`, owned); n != 1 {
			t.Fatalf("run %d: the command's own outcome event was retired", run)
		}
	}
	if _, err := r.conn.Exec(r.ctx, down); err != nil {
		t.Fatalf("the backfill's down: %v", err)
	}
	if n := count(`select count(*) from event where id = $1 and message = 'forged'`, forged); n != 1 {
		t.Fatal("the down did not restore the retired event")
	}
	if n := count(`select count(*) from retired_sample where lane = 'event' and sample_id = $1`, forged); n != 0 {
		t.Fatal("the down left the restored event on record as retired")
	}
}
