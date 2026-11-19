package storage_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestFleetBucketReadProbe is the measurement behind #1566: a 30-day hourly
// read of each of 50 series sampled every 30 seconds (86,400 samples a series,
// 4.3 million in all), from the trends and from the series read alone, over
// raw rows and again once the days have folded into blocks. The type keeps
// its samples the full 30 days, so both ways answer the same buckets, which
// the probe checks.
//
// It is a probe, not a gate: set BUCKET_MEASURE=1 to run it, and
// BUCKET_EXPLAIN=1 as well to log each way's plan.
func TestFleetBucketReadProbe(t *testing.T) {
	// Serial: a timing probe; running beside other tests would skew the numbers it exists to report.
	if os.Getenv("BUCKET_MEASURE") == "" {
		t.Skip("set BUCKET_MEASURE=1 to measure")
	}
	const series, days, cadence = 50, 30, 30 * time.Second
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	stmts := &lastStmt{}
	gw, err := storage.NewPG(ctx, dsn, storage.WithQueryTracer(stmts))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into component (name, product_id) values ('probe-c1', (select id from product where name = 'generic-device'))`)
	mustExec(t, conn, `insert into metric_type (name, data_type, retention_days) values ('probe-rtt', 'float', $1)`, days)
	mustExec(t, conn, `insert into metric_series (metric_type_id, owner_kind, component_id, instance, provenance)
		select (select id from metric_type where name = 'probe-rtt'), 'component', (select id from component where name = 'probe-c1'),
			format('i%s', lpad(i::text, 2, '0')), 'observed'
		from generate_series(0, $1 - 1) i`, series)

	built := time.Now()
	now := time.Now().UTC()
	from := now.Truncate(24*time.Hour).AddDate(0, 0, -days)
	week := from.AddDate(0, 0, (8-int(from.Weekday()))%7)
	if err := gw.SetMetricBlockMarks(ctx, week); err != nil {
		t.Fatal(err)
	}
	for d := from; d.Before(now); d = d.Add(24 * time.Hour) {
		if err := gw.MakeMetricDays(ctx, d); err != nil {
			t.Fatal(err)
		}
		mustExec(t, conn, `insert into metric (series_id, ts, value)
			select s.id, ts, 10 + 5 * sin(extract(epoch from ts) / 3600 + s.id) + random()
			from metric_series s cross join generate_series($1::timestamptz, least($1::timestamptz + interval '1 day', now()) - interval '1 microsecond', $2::interval) ts
			order by s.id, ts`, d, cadence)
	}
	mustExec(t, conn, `analyze metric`)
	samples := time.Since(built)
	folded := time.Now()
	if err := gw.SetMetricTrendMark(ctx, now.Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := gw.FoldMetricTrendHours(ctx, from, now.Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	trends := time.Since(folded)
	t.Logf("fixture: %d series x %d days at %s: samples landed in %s, trends folded in %s",
		series, days, cadence, samples.Round(time.Millisecond), trends.Round(time.Millisecond))

	instances := make([]string, series)
	for i := range instances {
		instances[i] = fmt.Sprintf("i%02d", i)
	}
	// readAll reads every series once, the one way, and returns the median and
	// total wall time of the reads and their buckets.
	readAll := func(fromSamples bool) (median, total time.Duration, out [][]storage.MetricBucket) {
		read := gw.ListMetricBuckets
		if fromSamples {
			read = gw.ListMetricBucketsFromSamples
		}
		took := make([]time.Duration, 0, series)
		for _, inst := range instances {
			start := time.Now()
			got, err := read(ctx, "component", "probe-c1", "probe-rtt", &inst, days*24*time.Hour, 5*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			took = append(took, time.Since(start))
			if got.Resolution != time.Hour || len(got.Buckets) != days*24 {
				t.Fatalf("read %d buckets at %s, want %d at 1h", len(got.Buckets), got.Resolution, days*24)
			}
			out = append(out, got.Buckets)
		}
		for _, d := range took {
			total += d
		}
		slices.Sort(took)
		return took[len(took)/2], total, out
	}
	measure := func(name string, fromSamples bool) [][]storage.MetricBucket {
		readAll(fromSamples) // warm the cache: the first pass reads from disk
		median, total, out := readAll(fromSamples)
		t.Logf("%-28s median %8s a series, %9s for all %d", name, median.Round(time.Microsecond), total.Round(time.Millisecond), series)
		if os.Getenv("BUCKET_EXPLAIN") != "" {
			sql, args := stmts.get()
			rows, err := conn.Query(ctx, "explain (analyze, buffers) "+sql, args...)
			if err != nil {
				t.Fatal(err)
			}
			var plan []string
			for rows.Next() {
				var l string
				if err := rows.Scan(&l); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, l)
			}
			rows.Close()
			t.Logf("%s plan:\n%s", name, strings.Join(plan, "\n"))
		}
		return out
	}
	byTrends := measure("trends", false)
	compare := func(when string, got [][]storage.MetricBucket) {
		for i := range got {
			if err := sameBuckets(got[i], byTrends[i]); err != nil {
				t.Errorf("%s, series %s: the trends differ from the series read: %v", when, instances[i], err)
			}
		}
	}
	compare("over raw rows", measure("series read, raw rows", true))

	folded = time.Now()
	c, err := gw.CompactMetricBlocks(ctx, storage.BlockFold{MaxDays: days + 1, LaneDays: days})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("blocks: %d days and %d weeks folded into %d blocks in %s", c.Days, c.Weeks, c.Blocks, time.Since(folded).Round(time.Millisecond))
	mustExec(t, conn, `delete from metric m using metric_block k
		where k.series_id = m.series_id and m.ts >= k.span_start and m.ts < k.span_start + case k.span when 'hour' then interval '1 hour' when 'day' then interval '1 day' else interval '7 days' end`)
	mustExec(t, conn, `vacuum analyze metric`)
	mustExec(t, conn, `analyze metric_block`)
	compare("over blocks", measure("series read, folded blocks", true))
	measure("trends, days folded", false)
}

// lastStmt keeps the last bucket statement the gateway ran and its
// arguments, for BUCKET_EXPLAIN to plan again.
type lastStmt struct {
	mu   sync.Mutex
	sql  string
	args []any
}

func (s *lastStmt) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(d.SQL, "pieces as") {
		s.mu.Lock()
		s.sql, s.args = d.SQL, d.Args
		s.mu.Unlock()
	}
	return ctx
}

func (s *lastStmt) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (s *lastStmt) get() (string, []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sql, s.args
}
