package bus

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	"github.com/jackc/pgx/v5"
)

// TestARetentionPassReportsOnItsPlatformComponent (#1629), against real
// Postgres: a pass that prunes 25 raw metric rows past the window in chunks
// of 10, capped at one chunk, lands its figures on the platform component as
// the process: 10 deleted and the metric lane behind, with the partitions and
// sizes the status read gives. Held behind past the seeded rule's sustain,
// the sweep opens a warning alarm on that lane alone; the next uncapped pass
// removes the other 15, reads the lane caught up, and the alarm clears. A
// node's platform component stands in for the server's (#1627).
func TestARetentionPassReportsOnItsPlatformComponent(t *testing.T) {
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
	node, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "srv-stand-in"}, all, all)
	if err != nil || node.PlatformComponentID == nil {
		t.Fatalf("create node: %v (platform component %v)", err, node)
	}
	comp := *node.PlatformComponentID

	// Behind is about the metric lane here, not the compactions: stand their
	// marks at their targets, as the node_log test does, so neither has work.
	t0 := time.Now().Add(-2 * time.Second).UTC()
	trendMark := t0.Add(-storage.MetricTrendHorizon).Truncate(time.Hour)
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

	// 25 raw rows on one series, each just past the 14-day window, in the
	// window's cutoff day: the rows a chunked prune removes (an older day's
	// partition is dropped whole, uncounted).
	cutoff := t0.AddDate(0, 0, -14)
	day := cutoff.Truncate(24 * time.Hour)
	if _, err := conn.Exec(ctx, `create table if not exists metric_p`+day.Format("20060102")+
		` partition of metric for values from ('`+day.Format(time.RFC3339)+`') to ('`+
		day.AddDate(0, 0, 1).Format(time.RFC3339)+`')`); err != nil {
		t.Fatalf("create the cutoff day's partition: %v", err)
	}
	if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: comp,
		Key: writerclass.CheckinAge, Value: 1, TS: t0}}); err != nil {
		t.Fatalf("make the series: %v", err)
	}
	var series int64
	if err := conn.QueryRow(ctx, `select s.id from metric_series s join metric_type t on t.id = s.metric_type_id
		where s.component_id = $1 and t.name = $2`, comp, writerclass.CheckinAge).Scan(&series); err != nil {
		t.Fatalf("read the series: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into metric (series_id, ts, value)
		select $1, $2::timestamptz - make_interval(secs => i / 1000.0), i from generate_series(1, 25) i`,
		series, cutoff); err != nil {
		t.Fatalf("plant the expired rows: %v", err)
	}

	values := func(key, instance string) []float64 {
		t.Helper()
		rows, err := conn.Query(ctx, `select m.value from `+storagetest.MetricRows+` m
			join metric_type t on t.id = m.metric_type_id
			where m.component_id = $1 and t.name = $2 and m.instance = $3 order by m.ts`, comp, key, instance)
		if err != nil {
			t.Fatalf("read %s|%s: %v", key, instance, err)
		}
		vs, err := pgx.CollectRows(rows, pgx.RowTo[float64])
		if err != nil {
			t.Fatalf("read %s|%s: %v", key, instance, err)
		}
		return vs
	}
	pass := func(now time.Time, maxChunks int) retentionReport {
		t.Helper()
		rep, err := retentionPass(ctx, gw, now, 10, maxChunks, 0)
		if err != nil {
			t.Fatalf("pass: %v", err)
		}
		if err := recordRetention(ctx, gw, comp, rep, now); err != nil {
			t.Fatalf("record the pass: %v", err)
		}
		return rep
	}
	metricAlarms := func(filter storage.AlarmFilter) []storage.Alarm {
		t.Helper()
		alarms, err := gw.ListAlarms(ctx, "srv-stand-in", filter)
		if err != nil {
			t.Fatal(err)
		}
		var out []storage.Alarm
		for _, a := range alarms {
			if a.RaisedByRule != nil && *a.RaisedByRule == "retention-behind" {
				out = append(out, a)
			}
		}
		return out
	}

	pass(t0, 1)
	if got := values(writerclass.RetentionDeletedCount, "metric"); len(got) != 1 || got[0] != 10 {
		t.Errorf("deleted count on the metric lane %v, want one pass's 10", got)
	}
	if got := values(writerclass.RetentionBehind, "metric"); len(got) != 1 || got[0] != 1 {
		t.Errorf("behind on the metric lane %v, want 1 after a capped pass", got)
	}
	for _, table := range []string{"metric", "metric_trend", "metric_block", "property", "log_line", "node_log"} {
		if got := values(writerclass.TableBytes, table); len(got) != 1 || got[0] < 0 {
			t.Errorf("%s sized %v, want one non-negative read", table, got)
		}
	}
	if got := values(writerclass.TableBytes, "metric"); len(got) != 1 || got[0] <= 0 {
		t.Errorf("metric sized %v, want its partitions' bytes", got)
	}
	if got := values(writerclass.DatabaseBytes, ""); len(got) != 1 || got[0] <= 0 {
		t.Errorf("database sized %v, want one positive read", got)
	}
	parts, err := gw.MetricPartitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := values(writerclass.MetricPartitionCount, ""); len(got) != 1 || got[0] != float64(len(parts)) {
		t.Errorf("partition count %v, want %d", got, len(parts))
	}
	wantAge := t0.Sub(day).Hours() / 24
	if got := values(writerclass.MetricOldestDayAge, ""); len(got) != 1 || got[0] < wantAge-1e-6 || got[0] > wantAge+1e-6 {
		t.Errorf("oldest day age %v, want %.4f: the cutoff day's partition, every older one dropped", got, wantAge)
	}
	if got := values(writerclass.RetentionWindowDays, "metric"); len(got) != 1 || got[0] != 14 {
		t.Errorf("metric window %v, want the 14-day default", got)
	}

	// Under the sustain nothing opens; held past it, the sweep opens the
	// lane's alarm at warning.
	if err := gw.SweepEventRules(ctx, time.Now().Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := metricAlarms(storage.AlarmFilter{}); len(got) != 0 {
		t.Fatalf("under the 5m sustain %d retention alarms opened", len(got))
	}
	if err := gw.SweepEventRules(ctx, time.Now().Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	open := metricAlarms(storage.AlarmFilter{})
	if len(open) != 1 || !strings.HasSuffix(open[0].DedupKey, ":metric") || open[0].Severity != "warning" {
		t.Fatalf("after the sustain retention alarms %+v, want one warning on the metric lane", open)
	}

	t1 := t0.Add(time.Second)
	if rep := pass(t1, 10); rep.Deleted["metric"] != 15 || len(rep.Behind) != 0 {
		t.Fatalf("the uncapped pass %+v, want the other 15 removed and nothing behind", rep)
	}
	var left int
	if err := conn.QueryRow(ctx, `select count(*) from metric where series_id = $1 and ts < $2`, series, cutoff).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d expired rows left, want none", left)
	}
	if got := values(writerclass.RetentionBehind, "metric"); len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Errorf("behind on the metric lane %v, want [1 0]", got)
	}
	if got := metricAlarms(storage.AlarmFilter{}); len(got) != 0 {
		t.Errorf("caught up, %d retention alarms still open", len(got))
	}
	if got := metricAlarms(storage.AlarmFilter{IncludeCleared: true}); len(got) != 1 || got[0].ClearedAt == nil {
		t.Errorf("history %+v, want the one alarm, cleared", got)
	}
}
