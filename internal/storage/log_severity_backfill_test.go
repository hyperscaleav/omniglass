package storage_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/logline"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

const severityBackfill = "migrations/20261028170000_log_severity_backfill.sql"

func severityBackfillUp(t *testing.T) string {
	t.Helper()
	raw, err := db.FS.ReadFile(severityBackfill)
	if err != nil {
		t.Fatalf("read the backfill: %v", err)
	}
	up, _, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatal("the backfill has no -- migrate:down")
	}
	return strings.TrimPrefix(up, "-- migrate:up")
}

// TestLogSeverityBackfillMatchesTheGoTable holds the backfill's alias list to
// internal/logline's, so the words the backfill maps are exactly the words every
// writer maps today (#417): a word one accepts and the other does not would leave
// a stray severity a retention window by severity never meets.
func TestLogSeverityBackfillMatchesTheGoTable(t *testing.T) {
	pairs := regexp.MustCompile(`\('([^']*)',\s*'([^']*)'\)`).FindAllStringSubmatch(severityBackfillUp(t), -1)
	got := map[string]string{}
	for _, p := range pairs {
		got[p[1]] = p[2]
	}
	want := logline.Aliases()
	if len(got) != len(want) {
		t.Errorf("the backfill maps %d words, the Go table %d", len(got), len(want))
	}
	for word, level := range want {
		if got[word] != level {
			t.Errorf("the Go table maps %q to %q, the backfill to %q", word, level, got[word])
		}
	}
}

// TestLogSeverityBackfillCanonicalizesBothTables is the one-time backfill (#417,
// the 2026-10-25 comment on it): every existing log_line and node_log severity
// is mapped through the canonical alias table, a word outside it moves to the
// severity_raw attribute with no severity (as logline.Normalize does on a new
// line), and a blank severity becomes none. A second run changes nothing.
func TestLogSeverityBackfillCanonicalizesBothTables(t *testing.T) {
	f := newPruneFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateNode(f.ctx, "", storage.NodeSpec{Name: "sev-node"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	type row struct{ severity, attrs string }
	lineRows := []row{
		{"warn", ""}, {"ERROR", ""}, {" Informational ", ""}, {"5", ""}, {"weird", `{"a": 1}`},
		{"Odd", `[1, 2]`}, {"   ", ""}, {"", "null"}, {"critical", ""}, {"ERROR\r", ""}, {"warn\t", ""}, {"error\u00a0", ""},
	}
	for i, r := range lineRows {
		var sev any = r.severity
		if r.attrs == "null" {
			sev = nil
		}
		var attrs any
		if r.attrs != "" && r.attrs != "null" {
			attrs = r.attrs
		}
		mustExec(t, f.conn, `insert into log_line (ts, component_id, instance, source, severity, message, attributes)
			values (now(), (select id from component where name = 'prune-c1'), '', 'push', $1, $2, $3::jsonb)`, sev, "line "+string(rune('a'+i)), attrs)
	}
	for _, sev := range []string{"WARN", "fatal", "bogus"} {
		mustExec(t, f.conn, `insert into node_log (ts, node_id, source, severity, message)
			values (now(), (select principal_id from node where name = 'sev-node'), 'node', $1, $1)`, sev)
	}

	up := severityBackfillUp(t)
	read := func(sql string) []string {
		t.Helper()
		rows, err := f.conn.Query(f.ctx, sql)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	for run := 1; run <= 2; run++ {
		if _, err := f.conn.Exec(f.ctx, up); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		lines := strings.Join(read(`select coalesce(severity, '-') || '|' || coalesce(attributes::text, '-') from log_line order by id`), "; ")
		want := `warning|-; error|-; info|-; notice|-; -|{"a": 1, "severity_raw": "weird"}; ` +
			`-|{"attributes": [1, 2], "severity_raw": "Odd"}; -|-; -|-; critical|-; error|-; warning|-; error|-`
		if lines != want {
			t.Errorf("run %d: log_line severities\n got %s\nwant %s", run, lines, want)
		}
		nodes := strings.Join(read(`select coalesce(severity, '-') || '|' || coalesce(attributes->>'severity_raw', '-') from node_log order by id`), "; ")
		if nodes != "warning|-; critical|-; -|bogus" {
			t.Errorf("run %d: node_log severities = %s, want warning, critical, and bogus kept raw", run, nodes)
		}
	}
}

// TestANodeLogLandsWithACanonicalSeverity: a node's self-log is held to the
// canonical severity at the gateway, as a component's log line is (#1145), so
// the backfill is the last time node_log needs one: a known word becomes its
// level, an unknown one is kept as severity_raw with no severity.
func TestANodeLogLandsWithACanonicalSeverity(t *testing.T) {
	f := newPruneFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateNode(f.ctx, "", storage.NodeSpec{Name: "sev-node"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	landed, err := f.gw.LandBatch(f.ctx, storage.IngestBatch{BatchID: "node-sev", NodeLogs: []storage.NodeLogWrite{
		{Node: "sev-node", Source: "node", Severity: "WARN", Message: "a"},
		{Node: "sev-node", Source: "node", Severity: "bogus", Message: "b"},
	}})
	if err != nil || !landed {
		t.Fatalf("land: landed=%v err=%v", landed, err)
	}
	var got []string
	rows, err := f.conn.Query(f.ctx, `select coalesce(severity, '-') || '|' || coalesce(attributes->>'severity_raw', '-') from node_log order by id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	rows.Close()
	if strings.Join(got, "; ") != "warning|-; -|bogus" {
		t.Fatalf("node_log rows = %v, want WARN as warning and bogus kept raw", got)
	}
}
