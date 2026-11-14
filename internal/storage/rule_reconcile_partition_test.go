package storage_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// finishedDay fills a day four days back with enough metric samples to span
// several block ranges, through the gateway's on-demand day and then in
// bulk, and returns the day's partition and the series it filled. Autovacuum
// is off on the day, so nothing but the code under test summarizes it.
func finishedDay(t *testing.T, f *healthFixture) (part string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	u := time.Now().UTC().AddDate(0, 0, -4)
	at = time.Date(u.Year(), u.Month(), u.Day(), 1, 0, 0, 0, time.UTC)
	part = "metric_p" + at.Format("20060102")
	if err := insertMetrics(ctx, f.gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "http-response-time", Instance: "bulk", Value: 1, TS: at}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(ctx, `alter table `+pgx.Identifier{part}.Sanitize()+` set (autovacuum_enabled = false)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(ctx, `insert into metric (series_id, ts, value)
		select m.series_id, $1::timestamptz + g * interval '1 ms', 1 from (select series_id from metric where ts = $1 limit 1) m,
		generate_series(1, 60000) g`, at); err != nil {
		t.Fatal(err)
	}
	return part, at
}

// pagesRead sums the pages (heap and index) every scan of one relation read
// in an EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) plan, and the plan.
func pagesRead(t *testing.T, f *healthFixture, rel, sql string, args ...any) (pages float64, plan string) {
	t.Helper()
	var raw []byte
	if err := f.conn.QueryRow(context.Background(), "explain (analyze, buffers, format json) "+sql, args...).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plans []struct{ Plan map[string]any }
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatal(err)
	}
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		if n["Relation Name"] == rel {
			for _, k := range []string{"Shared Hit Blocks", "Shared Read Blocks"} {
				if v, ok := n[k].(float64); ok {
					pages += v
				}
			}
		}
		kids, _ := n["Plans"].([]any)
		for _, k := range kids {
			if m, ok := k.(map[string]any); ok {
				walk(m)
			}
		}
	}
	walk(plans[0].Plan)
	return pages, string(raw)
}

// #1929: the reconcile's marks read and candidates scan reach the samples
// past their mark through each day's id block range index, so a finished day
// whose ranges are summarized costs a few index pages. Before, the roll
// left a finished day's ranges unsummarized, so they read lossy, and the
// candidates scan walked each watched series' whole day in its btree: every
// pass read every finished day. Seqscan is off for the question because the
// fixture's days are small; the block range index is what the planner takes
// once a day holds a soak's samples.
func TestReconcileReadsSkipAFinishedDay(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "slow", SampleKey: "http-response-time",
		Fire: criteria("gt", 30), Severity: "warning"}, f.all); err != nil {
		t.Fatal(err)
	}
	part, _ := finishedDay(t, f)
	if _, err := f.gw.EnsureMetricPartitions(ctx); err != nil {
		t.Fatal(err)
	}
	mark := maxSampleID(t, f, "metric")
	if err := insertMetrics(ctx, f.gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "http-response-time", Instance: "http", Value: 5, TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	to := maxSampleID(t, f, "metric")
	var dayPages float64
	if err := f.conn.QueryRow(ctx, `select pg_relation_size(to_regclass($1)) / current_setting('block_size')::int`, part).Scan(&dayPages); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(ctx, "set enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		sql  string
		args []any
	}{
		{"marks read", storage.ExportRuleMarksSQL, []any{mark}},
		{"candidates scan", storage.ExportRuleCandidatesSQL("metric"), []any{"metric", mark, to}},
	} {
		// A summarized day answers from a few index pages; one unsummarized
		// range alone is 128 heap pages, past a tenth of this day.
		if pages, plan := pagesRead(t, f, part, tc.sql, tc.args...); pages >= dayPages/10 {
			t.Errorf("the %s read %v pages of the finished day %s (%v heap pages), which holds no sample past the mark; want a few index pages:\n%s",
				tc.name, pages, part, dayPages, plan)
		}
	}
}

// Every block range index on metric summarizes a range once it fills, on
// every day: those the migration found and those made since.
func TestMetricBlockRangeIndexesAutosummarize(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	finishedDay(t, f)
	rows, err := f.conn.Query(ctx, `select c.relname::text from pg_class c
		where (c.oid in ('public.metric_id_brin'::regclass, 'public.metric_ts_brin'::regclass)
			or c.oid in (select inhrelid from pg_inherits where inhparent in ('public.metric_id_brin'::regclass, 'public.metric_ts_brin'::regclass)))
		and not coalesce(c.reloptions @> array['autosummarize=on'], false)`)
	if err != nil {
		t.Fatal(err)
	}
	off, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(off) > 0 {
		t.Fatalf("block range indexes that never summarize a filled range on their own: %v", off)
	}
}

// A sample that lands past the mark with an old timestamp (a late batch, a
// backlog replay) lands in a finished, summarized day: the reconcile still
// judges it, and fires its rule once, however many passes follow.
func TestReconcileFiresOnALateSampleOnce(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx, all := context.Background(), f.all
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "late", SampleKey: "http-response-time",
		Fire: criteria("gt", 30), Severity: "warning"}, all); err != nil {
		t.Fatal(err)
	}
	_, at := finishedDay(t, f)
	if _, err := f.gw.EnsureMetricPartitions(ctx); err != nil {
		t.Fatal(err)
	}
	if err := insertMetrics(ctx, f.gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "http-response-time", Instance: "late", Value: 10, TS: at}}); err != nil {
		t.Fatal(err)
	}
	st := &storage.RuleReconcileState{}
	reconcile(t, f, st)
	reconcile(t, f, st)
	// A slow sample written around the evaluator, an hour later on the same
	// old day: drift the next pass must find past its mark.
	if _, err := f.conn.Exec(ctx, `insert into metric (series_id, ts, value)
		select s.id, $1::timestamptz + interval '1 hour', 35 from metric_series s join metric_type t on t.id = s.metric_type_id
		join component c on c.id = s.owner_id where c.name = 'bar-1' and t.name = 'http-response-time' and s.instance = 'late'`, at); err != nil {
		t.Fatal(err)
	}
	if n := openRuleAlarms(t, f, "late"); n != 0 {
		t.Fatalf("setup: the slow sample was judged on landing (%d alarms); the drift was not built", n)
	}
	for range 3 {
		reconcile(t, f, st)
	}
	var raised int
	if err := f.conn.QueryRow(ctx, `select count(*) from alarm a join event_rule r on r.id = a.raised_by_rule_id
		where r.name = 'late'`).Scan(&raised); err != nil {
		t.Fatal(err)
	}
	if n := openRuleAlarms(t, f, "late"); n != 1 || raised != 1 {
		t.Fatalf("a late slow sample past the mark: %d open alarms and %d raised over three passes, want exactly 1 of each", n, raised)
	}
}
