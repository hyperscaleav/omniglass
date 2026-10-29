package storage_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/accesspath"
)

// eventRow inserts one event of an origin at an exact age on prune-c1 and
// returns its id; source names the event it was derived from (0 for none),
// alarm the alarm it carries ("" for none).
func (f *pruneFixture) eventRow(origin string, age time.Duration, msg string, source int64, alarm string) int64 {
	f.t.Helper()
	mustExec(f.t, f.conn, `insert into event_type (name) values ('prune-ev') on conflict do nothing`)
	var src, al any
	if source != 0 {
		src = source
	}
	if alarm != "" {
		al = alarm
	}
	var id int64
	if err := f.conn.QueryRow(f.ctx, `insert into event (ts, owner_kind, component_id, event_type_id, instance, origin, message, provenance, source, source_event_id, alarm_id)
		values ($1, 'component', (select id from component where name = 'prune-c1'), (select id from event_type where name = 'prune-ev'), '', $2, $3, 'observed', 'push', $4, $5::uuid)
		returning id`, time.Now().UTC().Add(-age), origin, msg, src, al).Scan(&id); err != nil {
		f.t.Fatalf("insert event %q: %v", msg, err)
	}
	return id
}

func (f *pruneFixture) eventCount(msg string) int {
	f.t.Helper()
	var n int
	if err := f.conn.QueryRow(f.ctx, `select count(*) from event where message = $1`, msg).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestPruneEventsKeepsEachOriginToItsWindow (#1159): an event of the pruned
// origin past the cutoff with nothing referencing it is removed, chunk by
// chunk; a newer one and one of another origin are kept, and a re-run removes
// nothing.
func TestPruneEventsKeepsEachOriginToItsWindow(t *testing.T) {
	f := newPruneFixture(t)
	for i := 0; i < 5; i++ {
		f.eventRow("caught", 100*day, "old-caught", 0, "")
	}
	f.eventRow("caught", 10*day, "new-caught", 0, "")
	f.eventRow("scheduled", 100*day, "old-scheduled", 0, "")
	cutoff := time.Now().Add(-90 * day)

	var total int64
	for i := 0; i < 3; i++ {
		n, err := f.gw.PruneEvents(f.ctx, "caught", cutoff, 2)
		if err != nil {
			t.Fatalf("prune: %v", err)
		}
		total += n
		if i < 2 && n != 2 {
			t.Errorf("chunk %d removed %d, want a full chunk of 2", i, n)
		}
	}
	if total != 5 || f.eventCount("old-caught") != 0 {
		t.Errorf("removed %d, %d old caught events left; want all 5 gone", total, f.eventCount("old-caught"))
	}
	if f.eventCount("new-caught") != 1 || f.eventCount("old-scheduled") != 1 {
		t.Errorf("a newer event or another origin's was removed")
	}
	if n, err := f.gw.PruneEvents(f.ctx, "caught", cutoff, 2); err != nil || n != 0 {
		t.Errorf("a re-run removed %d (err %v), want nothing", n, err)
	}
	if _, err := f.gw.PruneEvents(f.ctx, "bogus", cutoff, 2); err == nil {
		t.Error("an unknown origin was pruned, want it refused")
	}
}

// TestPruneEventsKeepsAReferencedEvent (#1159): the floor. An event past its
// window survives while a command names it as its cause or its settlement,
// while a derived event still names it as its source, and while the alarm it
// carries still exists. Once the derived event is gone, its source goes on
// the next prune, and an event whose alarm is gone (its link set null) goes
// like any other.
func TestPruneEventsKeepsAReferencedEvent(t *testing.T) {
	f := newPruneFixture(t)
	caused := f.eventRow("caused", 100*day, "cmd-cause", 0, "")
	settled := f.eventRow("caught", 100*day, "cmd-settle", 0, "")
	mustExec(t, f.conn, `insert into command_type (name, label, settle_window_seconds) values ('prune-cmd', 'Prune', 30)`)
	mustExec(t, f.conn, `insert into command (owner_kind, component_id, command_type_id, caused_event_id, settled_event_id, status, settled_at)
		values ('component', (select id from component where name = 'prune-c1'), (select id from command_type where name = 'prune-cmd'), $1, $2, 'settled', now())`,
		caused, settled)

	source := f.eventRow("caught", 100*day, "derived-source", 0, "")
	f.eventRow("derived", 100*day, "derived-child", source, "")

	a, err := f.gw.RaiseAlarm(f.ctx, "", "prune-c1", storage.AlarmSpec{Severity: "high", Message: "m", DedupKey: "k"})
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	f.eventRow("caught", 100*day, "alarm-event", 0, a.ID)
	f.eventRow("caught", 100*day, "orphan-event", 0, "")

	cutoff := time.Now().Add(-90 * day)
	for _, origin := range []string{"caught", "caused", "derived"} {
		if _, err := f.gw.PruneEvents(f.ctx, origin, cutoff, 100); err != nil {
			t.Fatalf("prune %s: %v", origin, err)
		}
	}
	for msg, want := range map[string]int{"cmd-cause": 1, "cmd-settle": 1, "derived-child": 0, "alarm-event": 1, "orphan-event": 0} {
		if got := f.eventCount(msg); got != want {
			t.Errorf("%s: %d left, want %d", msg, got, want)
		}
	}
	// The derived event went this pass; its source, unreferenced now, goes on the next.
	if _, err := f.gw.PruneEvents(f.ctx, "caught", cutoff, 100); err != nil {
		t.Fatal(err)
	}
	if got := f.eventCount("derived-source"); got != 0 {
		t.Errorf("derived-source: %d left after its derived event was pruned, want 0", got)
	}
	// An alarm gone (its events' link set null) frees its events.
	mustExec(t, f.conn, `delete from alarm where id = $1`, a.ID)
	if _, err := f.gw.PruneEvents(f.ctx, "caught", cutoff, 100); err != nil {
		t.Fatal(err)
	}
	if got := f.eventCount("alarm-event"); got != 0 {
		t.Errorf("alarm-event: %d left after its alarm went, want 0", got)
	}
}

// TestPruneEventsReachesItsIndexes (#1159): the candidates are searched by
// origin and time through event_origin_ts_idx, and each floor probe, and each
// foreign key's action a deleted event runs, reaches an index of its own, so a
// chunk never scans the event, command or sample tables.
func TestPruneEventsReachesItsIndexes(t *testing.T) {
	f := newPruneFixture(t)
	plan, err := accesspath.Explain(f.ctx, f.conn, storage.ExportPruneEventsSQL(), time.Now(), 10, "caught")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if ok, why := plan.MustReach("event", "event_origin_ts_idx"); !ok {
		t.Errorf("the candidates are not searched by origin and time: %s", why)
	}
	for _, probe := range []struct{ sql, relation, index string }{
		{`select 1 from command where caused_event_id = $1`, "command", "command_caused_event_idx"},
		{`select 1 from command where settled_event_id = $1`, "command", "command_settled_event_idx"},
		{`select 1 from event where source_event_id = $1`, "event", "event_source_event_idx"},
		{`select 1 from property where event_id = $1`, "property", "property_event_idx"},
		{`select 1 from metric_declared where event_id = $1`, "metric_declared", "metric_declared_event_idx"},
	} {
		p, err := accesspath.Explain(f.ctx, f.conn, probe.sql, int64(1))
		if err != nil {
			t.Fatalf("explain %s: %v", probe.sql, err)
		}
		if ok, why := p.MustReach(probe.relation, probe.index); !ok {
			t.Errorf("%s cannot reach %s: %s", probe.sql, probe.index, why)
		}
	}
}
