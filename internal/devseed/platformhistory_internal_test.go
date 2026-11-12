package devseed

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Every key on a server panel, and every key one is read against, is seeded,
// measured, or exempt with a reason, so a panel key added later cannot draw an empty
// line in a capture unnoticed (#1790).
func TestPlatformHistoryCoversEveryServerPanelKey(t *testing.T) {
	t.Parallel()
	for _, g := range storage.PlatformServerGroups {
		for _, k := range g.Keys {
			for _, name := range []string{k.Name, k.Of} {
				if name == "" {
					continue
				}
				_, profiled := platformHistory[name]
				seeded := profiled || platformHistoryMeasured[name]
				_, exempt := platformHistoryExempt[name]
				if !seeded && !exempt {
					t.Errorf("panel %s key %s has no seeded history and no exemption", g.Name, name)
				}
				if seeded && exempt {
					t.Errorf("panel %s key %s is both seeded and exempt", g.Name, name)
				}
			}
		}
	}
}

// historyMax is the largest value any minute of key's series takes.
func historyMax(key string) float64 {
	top := 0.0
	for _, s := range platformHistory[key] {
		for i := range PlatformHistoryMinutes {
			top = max(top, historyValue(key, s, i, PlatformHistoryMinutes))
		}
	}
	return top
}

// The seeded hour is a healthy lab's: every failure and queue reads 0, and the
// pool sits well under its size.
func TestPlatformHistoryReadsHealthy(t *testing.T) {
	t.Parallel()
	for _, key := range []string{
		writerclass.DBPoolWaitCount,
		writerclass.BusConsumerPending, writerclass.BusConsumerAckPending, writerclass.CalcPendingDepth,
		writerclass.OutboxQueuedDepth, writerclass.OutboxDueAge,
		writerclass.RetentionBehind, writerclass.RetentionDeletedCount,
	} {
		if top := historyMax(key); top != 0 {
			t.Errorf("%s reaches %v, want 0 the whole hour", key, top)
		}
	}
	if acquired, size := historyMax(writerclass.DBPoolAcquired), historyMax(writerclass.DBPoolMax); acquired > size/4 {
		t.Errorf("the pool reaches %v of %v acquired, want under a quarter", acquired, size)
	}
}

// A minute's value depends only on its series and position, so two captures
// draw the same line, and a size that climbs ends the hour at its base.
func TestPlatformHistoryIsDeterministic(t *testing.T) {
	t.Parallel()
	s := historyFor(storage.RetentionStatus{DatabaseBytes: 1000}, time.Now())[writerclass.DatabaseBytes][0]
	if a, b := historyValue(writerclass.DatabaseBytes, s, 7, 60), historyValue(writerclass.DatabaseBytes, s, 7, 60); a != b {
		t.Fatalf("minute 7 read %v then %v", a, b)
	}
	if end := historyValue(writerclass.DatabaseBytes, s, 59, 60); end != s.base {
		t.Fatalf("the newest minute reads %v, want the base %v", end, s.base)
	}
	if start := historyValue(writerclass.DatabaseBytes, s, 0, 60); start >= s.base {
		t.Fatalf("the oldest minute reads %v, want under the base %v as the size climbs", start, s.base)
	}
}

// newest and oldest are the last and first minute of key's series at instance.
func newestOldest(t *testing.T, h map[string][]historySeries, key, instance string) (float64, float64) {
	t.Helper()
	for _, s := range h[key] {
		if s.instance == instance {
			return historyValue(key, s, PlatformHistoryMinutes-1, PlatformHistoryMinutes), historyValue(key, s, 0, PlatformHistoryMinutes)
		}
	}
	t.Fatalf("%s %q: no series", key, instance)
	return 0, 0
}

// The sizes and the oldest partition's age end the hour on what the database
// measures, so the server's next live sample continues the line instead of
// drawing a cliff into it (#1790).
func TestPlatformHistoryEndsOnTheMeasuredSizes(t *testing.T) {
	t.Parallel()
	end := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	status := storage.RetentionStatus{
		DatabaseBytes: 17_000_000,
		TableBytes:    map[string]int64{"metric": 1_800_000, "log_line": 90_000},
		Partitions:    []storage.MetricPartition{{From: end.Add(-48*time.Hour - time.Minute)}},
	}
	h := historyFor(status, end)
	if got, _ := newestOldest(t, h, writerclass.DatabaseBytes, ""); got != 17_000_000 {
		t.Errorf("the database size ends the hour at %v, want the measured 17000000", got)
	}
	var tables []string
	for _, s := range h[writerclass.TableBytes] {
		tables = append(tables, s.instance)
	}
	if !slices.Equal(tables, []string{"log_line", "metric"}) {
		t.Errorf("table sizes seeded for %v, want each measured table, sorted", tables)
	}
	if got, _ := newestOldest(t, h, writerclass.TableBytes, "metric"); got != 1_800_000 {
		t.Errorf("metric's size ends the hour at %v, want the measured 1800000", got)
	}
	newest, oldest := newestOldest(t, h, writerclass.MetricOldestDayAge, "")
	if math.Abs(newest-2) > 1e-9 {
		t.Errorf("the oldest partition's age ends the hour at %v days, want 2", newest)
	}
	if want := 2 - 59.0/(24*60); math.Abs(oldest-want) > 1e-9 {
		t.Errorf("the oldest partition's age starts the hour at %v days, want %v", oldest, want)
	}
	if _, ok := historyFor(storage.RetentionStatus{}, end)[writerclass.MetricOldestDayAge]; ok {
		t.Error("a database with no metric partition seeded an oldest age, want none")
	}
}
