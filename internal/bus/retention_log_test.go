package bus

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The server's housekeeping narrates on its own platform component (#1708):
// each thing a pass did is one log_line there, beside the figures #1629
// lands, and a pass that did nothing writes nothing. No pass raises an event.

// lineAttrs decodes a log line's attributes, failing on any that are not a
// JSON object.
func lineAttrs(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("attributes %s: %v", raw, err)
	}
	return m
}

// TestRetentionNotesTellEachHousekeepingKind: a pass that failed, pruned,
// closed hours, folded days and rolled partitions tells each, in that order,
// the failure a warning carrying the error and the rest info carrying their
// counts; mapped to log lines, every one lands on the component under the
// server's source and the retention facility at the pass's time.
func TestRetentionNotesTellEachHousekeepingKind(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	through := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	week := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	rep := retentionReport{
		Deleted: map[string]int64{"metric": 120, "log_line/debug": 30, "property": 0},
		Behind:  []string{"metric"},
		Created: []string{"metric_p20261114"},
		Dropped: []string{"metric_p20261020"},
		Trends:  storage.TrendCompaction{Hours: 2, Through: through, Recomputed: 5},
		Blocks:  storage.BlockCompaction{Days: 1, Through: day, Weeks: 1, WeekThrough: week, Blocks: 40},
	}
	lines := housekeepingLines("comp-1", retentionNotes(rep, errors.New("prune metric: lock timeout")), now)

	want := []struct {
		severity, message string
		attrs             map[string]any
	}{
		{"warning", "sample retention pass failed", map[string]any{"error": "prune metric: lock timeout"}},
		{"info", "rows pruned past their retention window", map[string]any{
			"deleted": map[string]any{"metric": 120.0, "log_line/debug": 30.0, "property": 0.0},
			"total":   150.0, "behind": []any{"metric"}}},
		{"info", "metric hours closed into trends", map[string]any{
			"hours": 2.0, "through": through.Format(time.RFC3339), "recomputed": 5.0}},
		{"info", "metric days folded into blocks", map[string]any{
			"days": 1.0, "through": day.Format(time.RFC3339), "weeks": 1.0,
			"week_through": week.Format(time.RFC3339), "blocks": 40.0}},
		{"info", "metric partitions rolled", map[string]any{
			"created": []any{"metric_p20261114"}, "dropped": []any{"metric_p20261020"}}},
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines %+v, want %d", len(lines), lines, len(want))
	}
	for i, w := range want {
		l := lines[i]
		if l.OwnerKind != "component" || l.OwnerID != "comp-1" || l.Source != storage.ServerProductName ||
			l.Facility != "retention" || !l.TS.Equal(now) || l.Instance != "" {
			t.Errorf("line %d %+v: want component comp-1, source %s, facility retention, at %s", i, l, storage.ServerProductName, now)
		}
		if l.Severity != w.severity || l.Message != w.message {
			t.Errorf("line %d: %s %q, want %s %q", i, l.Severity, l.Message, w.severity, w.message)
		}
		got, _ := json.Marshal(lineAttrs(t, l.Attributes))
		exp, _ := json.Marshal(w.attrs)
		if string(got) != string(exp) {
			t.Errorf("line %d %q attributes %s, want %s", i, w.message, got, exp)
		}
	}
}

// TestAnIdlePassTellsNothing: a pass whose every lane removed nothing, with
// nothing behind, no hour closed, no day folded and no partition rolled,
// tells nothing, and nothing maps to no line; a pass that only fell behind
// still tells the prune note.
func TestAnIdlePassTellsNothing(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	idle := retentionReport{Deleted: map[string]int64{"metric": 0, "log_line/info": 0},
		Trends: storage.TrendCompaction{Through: now}, Blocks: storage.BlockCompaction{Through: now}}
	if notes := retentionNotes(idle, nil); len(notes) != 0 {
		t.Errorf("an idle pass told %+v, want nothing", notes)
	}
	if lines := housekeepingLines("comp-1", nil, now); lines != nil {
		t.Errorf("no notes mapped to %+v, want no lines", lines)
	}
	behind := retentionReport{Deleted: map[string]int64{"metric": 0}, Behind: []string{"metric_trend compaction"}}
	if notes := retentionNotes(behind, nil); len(notes) != 1 || notes[0].Message != "rows pruned past their retention window" {
		t.Errorf("a pass left behind told %+v, want the prune note naming it", notes)
	}
}

// TestPartitionNotesTellTheBootsCreations: the boot's partition check tells
// the partitions it made, and nothing when it made none; a server with no
// platform component maps any note to no line.
func TestPartitionNotesTellTheBootsCreations(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	notes := partitionNotes([]string{"metric_p20261113", "metric_p20261114"})
	lines := housekeepingLines("comp-1", notes, now)
	if len(lines) != 1 || lines[0].Severity != "info" || lines[0].Message != "metric partitions created" ||
		lines[0].Facility != "retention" || lines[0].Source != storage.ServerProductName {
		t.Fatalf("boot creations mapped to %+v, want one info line", lines)
	}
	if got, _ := json.Marshal(lineAttrs(t, lines[0].Attributes)); string(got) != `{"created":["metric_p20261113","metric_p20261114"]}` {
		t.Errorf("attributes %s, want the two partitions created", got)
	}
	if notes := partitionNotes(nil); len(notes) != 0 {
		t.Errorf("no creations told %+v, want nothing", notes)
	}
	if lines := housekeepingLines("", notes, now); lines != nil {
		t.Errorf("no component mapped to %+v, want no lines", lines)
	}
}

// failingLineStore refuses every log line write, and counts the calls.
type failingLineStore struct{ calls *int }

func (s failingLineStore) InsertLogLines(context.Context, []storage.LogLineWrite) error {
	*s.calls++
	return errors.New("connection refused")
}

// TestAFailedLineWriteIsLoggedNotRetried: a refused log line write is one
// slog warning naming the error, written once, never retried, and returns
// nothing for the pass to fail on; a server with no platform component, or
// one shutting down, writes nothing.
func TestAFailedLineWriteIsLoggedNotRetried(t *testing.T) {
	warnings := captureWarnings(t)
	calls := 0
	tellHousekeeping(context.Background(), failingLineStore{&calls}, "comp-1",
		partitionNotes([]string{"metric_p20261114"}), time.Now(), time.Second)
	if calls != 1 {
		t.Errorf("wrote %d times, want once and no retry", calls)
	}
	got := warnings("housekeeping log lines not written")
	if len(got) != 1 || !strings.Contains(got[0]["error"].(string), "connection refused") {
		t.Errorf("warnings %+v, want one naming the refused write", got)
	}
	calls = 0
	tellHousekeeping(context.Background(), failingLineStore{&calls}, "",
		partitionNotes([]string{"metric_p20261114"}), time.Now(), time.Second)
	if calls != 0 {
		t.Errorf("no component wrote %d times, want none", calls)
	}
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	tellHousekeeping(stopped, failingLineStore{&calls}, "comp-1",
		partitionNotes([]string{"metric_p20261114"}), time.Now(), time.Second)
	if calls != 0 || len(warnings("housekeeping log lines not written")) != 1 {
		t.Errorf("a server shutting down wrote %d times and warned %d times, want neither", calls,
			len(warnings("housekeeping log lines not written"))-1)
	}
}

// housekeepingFixture is a migrated, seeded database with the server's
// platform component and another component whose old debug lines a pass
// prunes.
type housekeepingFixture struct {
	ctx  context.Context
	gw   *storage.PG
	conn *pgx.Conn
	comp string
}

func newHousekeepingFixture(t *testing.T) housekeepingFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	comp, err := gw.EnsureServerComponent(ctx, "srv-a")
	if err != nil {
		t.Fatalf("ensure the server component: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	return housekeepingFixture{ctx: ctx, gw: gw, conn: conn, comp: comp}
}

// plantOldDebug writes n debug lines five days old on display-1, past the
// shared 3-day debug window.
func (f housekeepingFixture) plantOldDebug(t *testing.T, n int) {
	t.Helper()
	if _, err := f.conn.Exec(f.ctx, `insert into log_line (ts, component_id, instance, source, severity, message)
		select now() - interval '5 days', (select id from component where name = 'display-1'), '', 'push', 'debug', 'old'
		from generate_series(1, $1)`, n); err != nil {
		t.Fatalf("plant old debug lines: %v", err)
	}
}

// standMarks stands both compactions' marks at their targets, so no pass
// has an hour or a day to fold (as the node_log retention test does).
func (f housekeepingFixture) standMarks(t *testing.T) {
	t.Helper()
	trendMark := time.Now().UTC().Add(-storage.MetricTrendHorizon).Truncate(time.Hour)
	if _, err := f.conn.Exec(f.ctx, `insert into metric_trend_mark (id, announced_through, compacted_through)
		values (true, $1, $1)`, trendMark); err != nil {
		t.Fatalf("stand the trend marks: %v", err)
	}
	blockTarget := trendMark.Truncate(24 * time.Hour)
	blockWeek := blockTarget.AddDate(0, 0, -((int(blockTarget.Weekday()) + 6) % 7))
	if _, err := f.conn.Exec(f.ctx, `insert into metric_block_mark (id, blocked_through, week_through)
		values (true, $1, $2)`, blockTarget, blockWeek); err != nil {
		t.Fatalf("stand the block marks: %v", err)
	}
}

// serverLines is every retention line on the server component.
func (f housekeepingFixture) serverLines(t *testing.T) []storage.LogLine {
	t.Helper()
	lines, err := f.gw.ListComponentLogs(f.ctx, f.comp, time.Hour, 200, storage.LogFilter{})
	if err != nil {
		t.Fatalf("read the server's log: %v", err)
	}
	var out []storage.LogLine
	for _, l := range lines {
		if l.Facility == "retention" {
			out = append(out, l)
		}
	}
	return out
}

// findLine is the server's line carrying message, or nil.
func findLine(lines []storage.LogLine, message string) *storage.LogLine {
	for i := range lines {
		if lines[i].Message == message {
			return &lines[i]
		}
	}
	return nil
}

// serverEvents counts the event rows on the server component.
func (f housekeepingFixture) serverEvents(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.conn.QueryRow(f.ctx, `select count(*) from event where component_id = $1::uuid`, f.comp).Scan(&n); err != nil {
		t.Fatalf("count the server's events: %v", err)
	}
	return n
}

// TestTheServersHousekeepingNarratesOnItsComponent, against real Postgres:
// a server booting over a day ahead with no partition makes it and leaves
// that as a log line on its platform component before it starts ingest; its
// first retention pass prunes seven old debug lines from another component
// and leaves a line on its own carrying the retention facility and the
// count. Neither raises an event.
func TestTheServersHousekeepingNarratesOnItsComponent(t *testing.T) {
	f := newHousekeepingFixture(t)
	f.plantOldDebug(t, 7)
	var today time.Time
	if err := f.conn.QueryRow(f.ctx, `select date_trunc('day', now() at time zone 'UTC') at time zone 'UTC'`).Scan(&today); err != nil {
		t.Fatal(err)
	}
	ahead := "metric_p" + today.AddDate(0, 0, 3).Format("20060102")
	if _, err := f.conn.Exec(f.ctx, `drop table `+ahead); err != nil {
		t.Fatalf("drop %s: %v", ahead, err)
	}

	srv, err := New(Config{Host: "127.0.0.1", Port: 0, PlatformComponentID: f.comp}, f.gw)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer srv.Shutdown()

	boot := findLine(f.serverLines(t), "metric partitions created")
	if boot == nil {
		t.Fatalf("server lines %+v, want the boot's partition creation", f.serverLines(t))
	}
	if boot.Severity != "info" || boot.Source != storage.ServerProductName {
		t.Errorf("boot line %+v, want info from %s", boot, storage.ServerProductName)
	}
	if created, _ := lineAttrs(t, boot.Attributes)["created"].([]any); len(created) != 1 || created[0] != ahead {
		t.Errorf("boot line attributes %s, want %s created", boot.Attributes, ahead)
	}

	deadline := time.Now().Add(time.Minute)
	var pruned *storage.LogLine
	for pruned == nil && time.Now().Before(deadline) {
		pruned = findLine(f.serverLines(t), "rows pruned past their retention window")
		if pruned == nil {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if pruned == nil {
		t.Fatalf("server lines %+v, want the first pass's prune", f.serverLines(t))
	}
	attrs := lineAttrs(t, pruned.Attributes)
	if deleted, _ := attrs["deleted"].(map[string]any); deleted["log_line/debug"] != 7.0 || attrs["total"] != 7.0 {
		t.Errorf("prune line attributes %s, want 7 log_line/debug lines and a total of 7", pruned.Attributes)
	}
	if pruned.Severity != "info" || pruned.Facility != "retention" {
		t.Errorf("prune line %+v, want info under retention", pruned)
	}
	if n := f.serverEvents(t); n != 0 {
		t.Errorf("the server component holds %d events, want none from housekeeping", n)
	}
}

// TestAPassWithNothingToDoWritesNoLine, against real Postgres: with the
// compactions' marks at their targets and nothing past any window, a pass
// leaves no line on the server component; a pass that prunes leaves one,
// and neither leaves an event.
func TestAPassWithNothingToDoWritesNoLine(t *testing.T) {
	f := newHousekeepingFixture(t)
	f.standMarks(t)
	pass := func() {
		t.Helper()
		now := time.Now()
		rep, err := retentionPass(f.ctx, f.gw, now, 100, 10, 0)
		if err != nil {
			t.Fatalf("pass: %v", err)
		}
		tellHousekeeping(f.ctx, f.gw, f.comp, retentionNotes(rep, nil), now, 0)
	}
	pass()
	if lines := f.serverLines(t); len(lines) != 0 {
		t.Fatalf("an idle pass left %+v, want no line", lines)
	}
	f.plantOldDebug(t, 3)
	pass()
	lines := f.serverLines(t)
	if len(lines) != 1 || lines[0].Message != "rows pruned past their retention window" {
		t.Fatalf("a pruning pass left %+v, want one prune line", lines)
	}
	pass()
	if again := f.serverLines(t); len(again) != 1 {
		t.Errorf("an idle pass after a prune left %+v, want only the prune's line", again)
	}
	if n := f.serverEvents(t); n != 0 {
		t.Errorf("the server component holds %d events, want none from housekeeping", n)
	}
}
