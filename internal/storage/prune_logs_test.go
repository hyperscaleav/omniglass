package storage_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/accesspath"
)

// logFixture adds a node to the prune fixture and inserts a line into either
// log table at an exact age, with a severity ("" for none).
func (f *pruneFixture) logRow(table, severity string, age time.Duration, msg string) {
	f.t.Helper()
	var sev any = severity
	if severity == "" {
		sev = nil
	}
	ts := time.Now().UTC().Add(-age)
	if table == "log_line" {
		mustExec(f.t, f.conn, `insert into log_line (ts, component_id, instance, source, severity, message)
			values ($1, (select id from component where name = 'prune-c1'), '', 'push', $2, $3)`, ts, sev, msg)
		return
	}
	mustExec(f.t, f.conn, `insert into node_log (ts, node_id, source, severity, message)
		values ($1, (select principal_id from node where name = 'log-node'), 'node', $2, $3)`, ts, sev, msg)
}

func (f *pruneFixture) logMessages(table string) string {
	f.t.Helper()
	rows, err := f.conn.Query(f.ctx, `select message from `+table+` order by message`)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, m)
	}
	return strings.Join(out, ",")
}

func newLogFixture(t *testing.T) *pruneFixture {
	f := newPruneFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateNode(f.ctx, "", storage.NodeSpec{Name: "log-node"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	return f
}

const day = 24 * time.Hour

// TestPruneLogsKeepsEachBandToItsWindow (#417): a log line is pruned past its
// band's window and only then, on both log tables. Debug, info, warning and
// error each go by their own cutoff, and a line with no severity goes with
// info. A re-run deletes nothing.
func TestPruneLogsKeepsEachBandToItsWindow(t *testing.T) {
	f := newLogFixture(t)
	for _, table := range []string{"log_line", "node_log"} {
		f.logRow(table, "debug", 5*day, "debug-5d")
		f.logRow(table, "info", 5*day, "info-5d")
		f.logRow(table, "notice", 20*day, "notice-20d")
		f.logRow(table, "", 20*day, "unrated-20d")
		f.logRow(table, "warning", 20*day, "warning-20d")
		f.logRow(table, "critical", 20*day, "critical-20d")
		f.logRow(table, "error", 100*day, "error-100d")

		now := time.Now().UTC()
		cutoff := map[string]time.Duration{"debug": 3 * day, "info": 14 * day, "warning": 30 * day, "error": 90 * day}
		for _, band := range []string{"debug", "info", "warning", "error"} {
			if _, err := f.gw.PruneLogs(f.ctx, table, band, now.Add(-cutoff[band]), 100); err != nil {
				t.Fatalf("prune %s %s: %v", table, band, err)
			}
		}
		if got := f.logMessages(table); got != "critical-20d,info-5d,warning-20d" {
			t.Errorf("%s after a pass = %s, want critical-20d,info-5d,warning-20d", table, got)
		}
		for _, band := range []string{"debug", "info", "warning", "error"} {
			if n, err := f.gw.PruneLogs(f.ctx, table, band, now.Add(-cutoff[band]), 100); err != nil || n != 0 {
				t.Errorf("%s %s re-run deleted %d (err %v), want 0", table, band, n, err)
			}
		}
	}
}

// TestPruneLogsCompletesABacklogInChunks: a backlog larger than one call's limit
// completes across calls, each deleting at most the limit.
func TestPruneLogsCompletesABacklogInChunks(t *testing.T) {
	f := newLogFixture(t)
	for i := 0; i < 15; i++ {
		f.logRow("log_line", "info", 20*day, "old-info")
		f.logRow("log_line", "", 20*day, "old-unrated")
	}
	f.logRow("log_line", "info", time.Hour, "fresh")
	cutoff := time.Now().UTC().Add(-14 * day)
	var got []int64
	for _, want := range []int64{10, 10, 10, 0} {
		n, err := f.gw.PruneLogs(f.ctx, "log_line", "info", cutoff, 10)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, n)
		if n != want {
			t.Fatalf("chunks deleted %v, want 10, 10, 10, then 0", got)
		}
	}
	if m := f.logMessages("log_line"); m != "fresh" {
		t.Fatalf("after the backlog, lines = %s, want only the fresh one", m)
	}
}

// TestPruneLogsReachesItsIndex holds the log prune's access path with the
// accesspath instrument: the candidates of a band are searched by severity and
// time through the table's (severity, ts) index, and a band's lines with no
// severity through its (ts) index for them, so a chunk reads only rows it may
// delete, never the table.
func TestPruneLogsReachesItsIndex(t *testing.T) {
	f := newLogFixture(t)
	for _, table := range []string{"log_line", "node_log"} {
		stmts := storage.ExportPruneLogsSQL(table)
		for kind, index := range map[string]string{"rated": table + "_severity_ts_idx", "unrated": table + "_unrated_ts_idx"} {
			args := []any{time.Now(), 10}
			if kind == "rated" {
				args = append(args, []string{"info", "notice"})
			}
			plan, err := accesspath.Explain(f.ctx, f.conn, stmts[kind], args...)
			if err != nil {
				t.Fatalf("explain the %s %s prune: %v", table, kind, err)
			}
			var found bool
			for _, s := range plan.Scans(table) {
				if s.Alias != "c" {
					continue
				}
				found = true
				if s.Index != index || s.Disabled || !strings.Contains(s.Cond, "ts <") {
					t.Errorf("the %s %s prune's candidates are not searched by %s on time:\n\t%s", table, kind, index, plan)
				}
			}
			if !found {
				t.Errorf("the %s %s prune never scans its candidates:\n\t%s", table, kind, plan)
			}
		}
	}
}

// TestPruningALogLineClearsItsEventsLinkThroughAnIndex: an event that named a
// log line as its source keeps itself when the line is pruned, its link set to
// null by the foreign key's on delete set null. That action runs once per
// deleted line as an update of event by source_log_line_id, so it must reach an
// index: without one each deleted line scanned the whole event table, which is
// never pruned (#1159), measured at 27.7s for a 2,000-line chunk over 200k events
// against 8.8ms with the index (the 2026-10-28 review).
func TestPruningALogLineClearsItsEventsLinkThroughAnIndex(t *testing.T) {
	f := newLogFixture(t)
	f.logRow("log_line", "debug", 5*day, "the-source")
	var lineID int64
	if err := f.conn.QueryRow(f.ctx, `select id from log_line where message = 'the-source'`).Scan(&lineID); err != nil {
		t.Fatal(err)
	}
	mustExec(t, f.conn, `insert into event_type (name) values ('log-derived') on conflict do nothing`)
	var eventID int64
	if err := f.conn.QueryRow(f.ctx, `insert into event (owner_kind, component_id, event_type_id, instance, origin, message, provenance, source, source_log_line_id)
		values ('component', (select id from component where name = 'prune-c1'), (select id from event_type where name = 'log-derived'), '', 'caught', 'from a line', 'observed', 'push', $1)
		returning id`, lineID).Scan(&eventID); err != nil {
		t.Fatalf("event: %v", err)
	}
	if n, err := f.gw.PruneLogs(f.ctx, "log_line", "debug", time.Now().Add(-3*day), 100); err != nil || n != 1 {
		t.Fatalf("prune: %d lines, err %v; want the one debug line", n, err)
	}
	var link *int64
	if err := f.conn.QueryRow(f.ctx, `select source_log_line_id from event where id = $1`, eventID).Scan(&link); err != nil {
		t.Fatalf("the event did not survive its source line's prune: %v", err)
	}
	if link != nil {
		t.Fatalf("the event still names line %d, want its link cleared", *link)
	}

	// The action finds its rows as this select does; an UPDATE's own plan adds a
	// ModifyTable node the instrument would read as a second access to event.
	plan, err := accesspath.Explain(f.ctx, f.conn, `select id from event where source_log_line_id = $1`, lineID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, why := plan.MustReach("event", "event_source_log_line_idx"); !ok {
		t.Errorf("the foreign key's set-null action cannot reach an index: %s", why)
	}
}
