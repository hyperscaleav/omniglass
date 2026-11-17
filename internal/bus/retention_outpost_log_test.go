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

// TestAOutpostLogOverridePrunesOutpostLogsAndOnlyThem (#1350), against real
// Postgres: with no outpost override set, an outpost's debug lines two days old are
// kept by the shared 3-day debug window, exactly as a component's are. Once an
// admin sets outpost_log_debug_days to 1, the pass removes the outpost's debug lines
// past a day, across more than one chunk, and leaves the component's lines and
// the outpost's newer and info lines alone. A re-run removes nothing, and
// restoring the override puts outpost_log back on the shared window. It runs at
// the real clock and either side of a UTC midnight (#1567).
func TestAOutpostLogOverridePrunesOutpostLogsAndOnlyThem(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	storagetest.AtEachMidnight(t, outpostLogOverridePrunesOutpostLogsAndOnlyThem)
}

func outpostLogOverridePrunesOutpostLogsAndOnlyThem(t *testing.T, at time.Time) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	clock := time.Now
	if !at.IsZero() {
		clock = storagetest.PinClock(t, dsn, at)
	}
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
	if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "outpost-a"}, all, all); err != nil {
		t.Fatalf("create outpost: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	now := clock().UTC()
	// This test is about outpost_log, not the metric compactions: stand their
	// marks at their targets, so no pass has an hour or a day to fold and
	// none reports Behind. Left to seed itself, a fresh database's trend marks
	// start at its oldest partition (the 1st of the month), and the test's
	// passes at 48 hours each fall behind from about the 5th on; its block
	// marks start at the Monday of the last closed day's week, and its two
	// passes at two days each fall behind whenever that day is a Saturday or a
	// Sunday (#1567). The block target is the trend mark's UTC day: the last
	// day before it is the newest whose every hour is compacted.
	trendMark := now.Add(-storage.MetricTrendHorizon).Truncate(time.Hour)
	if _, err := conn.Exec(ctx, `insert into metric_trend_mark (id, announced_through, compacted_through)
		values (true, $1, $1)`, trendMark); err != nil {
		t.Fatalf("stand the trend marks at the target: %v", err)
	}
	blockTarget := trendMark.Truncate(24 * time.Hour)
	blockWeek := blockTarget.AddDate(0, 0, -((int(blockTarget.Weekday()) + 6) % 7))
	if _, err := conn.Exec(ctx, `insert into metric_block_mark (id, blocked_through, week_through)
		values (true, $1, $2)`, blockTarget, blockWeek); err != nil {
		t.Fatalf("stand the block marks at the target: %v", err)
	}
	line := func(table, severity string, age time.Duration, msg string) {
		t.Helper()
		ts := now.Add(-age)
		var q string
		if table == "outpost_log" {
			q = `insert into outpost_log (ts, outpost_id, source, severity, message)
				values ($1, (select user_id from outpost where name = 'outpost-a'), 'outpost', $2, $3)`
		} else {
			q = `insert into log_line (ts, component_id, instance, source, severity, message)
				values ($1, (select id from component where name = 'display-1'), '', 'push', $2, $3)`
		}
		if _, err := conn.Exec(ctx, q, ts, severity, msg); err != nil {
			t.Fatalf("insert %s: %v", table, err)
		}
	}
	const day = 24 * time.Hour
	// Five old outpost debug lines, so a chunk of 2 needs three statements: the
	// last one short.
	for i := 0; i < 5; i++ {
		line("outpost_log", "debug", 2*day, "outpost-old-debug")
	}
	line("outpost_log", "debug", 12*time.Hour, "outpost-new-debug")
	line("outpost_log", "info", 2*day, "outpost-old-info")
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
		rep, err := retentionPass(ctx, gw, clock(), 2, 10, 0)
		if err != nil {
			t.Fatalf("pass: %v", err)
		}
		return rep
	}

	if rep := pass(); rep.Deleted["outpost_log/debug"] != 0 || count("outpost_log", "outpost-old-debug") != 5 {
		t.Fatalf("with no outpost override, outpost_log/debug removed %d and kept %d, want the shared 3-day window to keep all 5",
			rep.Deleted["outpost_log/debug"], count("outpost_log", "outpost-old-debug"))
	}

	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "retention", map[string]any{"outpost_log_debug_days": 1}); err != nil {
		t.Fatalf("set the outpost override: %v", err)
	}
	rep := pass()
	if rep.Deleted["outpost_log/debug"] != 5 || len(rep.Behind) != 0 {
		t.Errorf("report %+v, want 5 outpost debug lines removed and nothing behind", rep)
	}
	for table, want := range map[[2]string]int{
		{"outpost_log", "outpost-old-debug"}: 0,
		{"outpost_log", "outpost-new-debug"}: 1,
		{"outpost_log", "outpost-old-info"}:  1,
		{"log_line", "comp-old-debug"}:       1,
	} {
		if got := count(table[0], table[1]); got != want {
			t.Errorf("%s %q: %d left, want %d", table[0], table[1], got, want)
		}
	}
	if rep := pass(); rep.Deleted["outpost_log/debug"] != 0 {
		t.Errorf("a re-run removed %d outpost debug lines, want none", rep.Deleted["outpost_log/debug"])
	}

	// Restoring the override (a null in the merge patch) returns outpost_log to
	// the shared window: an outpost line two days old survives again.
	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "retention", map[string]any{"outpost_log_debug_days": nil}); err != nil {
		t.Fatalf("restore the outpost override: %v", err)
	}
	line("outpost_log", "debug", 2*day, "outpost-old-debug-2")
	pass()
	if got := count("outpost_log", "outpost-old-debug-2"); got != 1 {
		t.Errorf("after restoring the override a 2-day outpost debug line left %d, want it kept by the shared window", got)
	}
}
