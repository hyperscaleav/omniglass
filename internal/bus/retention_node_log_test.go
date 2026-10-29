package bus

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestANodeLogOverridePrunesNodeLogsAndOnlyThem (#1350), against real
// Postgres: with no node override set, a node's debug lines two days old are
// kept by the shared 3-day debug window, exactly as a component's are. Once an
// admin sets node_log_debug_days to 1, the pass removes the node's debug lines
// past a day, across more than one chunk, and leaves the component's lines and
// the node's newer and info lines alone. A re-run removes nothing, and
// restoring the override puts node_log back on the shared window.
func TestANodeLogOverridePrunesNodeLogsAndOnlyThem(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	now := time.Now().UTC()
	line := func(table, severity string, age time.Duration, msg string) {
		t.Helper()
		ts := now.Add(-age)
		var q string
		if table == "node_log" {
			q = `insert into node_log (ts, node_id, source, severity, message)
				values ($1, (select principal_id from node where name = 'node-a'), 'node', $2, $3)`
		} else {
			q = `insert into log_line (ts, component_id, instance, source, severity, message)
				values ($1, (select id from component where name = 'display-1'), '', 'push', $2, $3)`
		}
		if _, err := conn.Exec(ctx, q, ts, severity, msg); err != nil {
			t.Fatalf("insert %s: %v", table, err)
		}
	}
	const day = 24 * time.Hour
	// Five old node debug lines, so a chunk of 2 needs three statements: the
	// last one short.
	for i := 0; i < 5; i++ {
		line("node_log", "debug", 2*day, "node-old-debug")
	}
	line("node_log", "debug", 12*time.Hour, "node-new-debug")
	line("node_log", "info", 2*day, "node-old-info")
	line("log_line", "debug", 2*day, "comp-old-debug")
	count := func(table, msg string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from `+table+` where message = $1`, msg).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	pass := func() retentionReport {
		t.Helper()
		rep, err := retentionPass(ctx, gw, time.Now(), 2, 10, 0)
		if err != nil {
			t.Fatalf("pass: %v", err)
		}
		return rep
	}

	if rep := pass(); rep.Deleted["node_log/debug"] != 0 || count("node_log", "node-old-debug") != 5 {
		t.Fatalf("with no node override, node_log/debug removed %d and kept %d, want the shared 3-day window to keep all 5",
			rep.Deleted["node_log/debug"], count("node_log", "node-old-debug"))
	}

	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "retention", map[string]any{"node_log_debug_days": 1}); err != nil {
		t.Fatalf("set the node override: %v", err)
	}
	rep := pass()
	if rep.Deleted["node_log/debug"] != 5 || len(rep.Behind) != 0 {
		t.Errorf("report %+v, want 5 node debug lines removed and nothing behind", rep)
	}
	for table, want := range map[[2]string]int{
		{"node_log", "node-old-debug"}: 0,
		{"node_log", "node-new-debug"}: 1,
		{"node_log", "node-old-info"}:  1,
		{"log_line", "comp-old-debug"}: 1,
	} {
		if got := count(table[0], table[1]); got != want {
			t.Errorf("%s %q: %d left, want %d", table[0], table[1], got, want)
		}
	}
	if rep := pass(); rep.Deleted["node_log/debug"] != 0 {
		t.Errorf("a re-run removed %d node debug lines, want none", rep.Deleted["node_log/debug"])
	}

	// Restoring the override (a null in the merge patch) returns node_log to
	// the shared window: a node line two days old survives again.
	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "retention", map[string]any{"node_log_debug_days": nil}); err != nil {
		t.Fatalf("restore the node override: %v", err)
	}
	line("node_log", "debug", 2*day, "node-old-debug-2")
	pass()
	if got := count("node_log", "node-old-debug-2"); got != 1 {
		t.Errorf("after restoring the override a 2-day node debug line left %d, want it kept by the shared window", got)
	}
}
