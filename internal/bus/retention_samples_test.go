package bus

import (
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// rowsByKey indexes platform rows as "key|instance" -> value, failing on a
// row off the component or the pass's time, or one written twice.
func rowsByKey(t *testing.T, rows []storage.PlatformMetric, component string, now time.Time) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, r := range rows {
		if r.ComponentID != component || !r.TS.Equal(now) {
			t.Errorf("row %+v is not on %s at %s", r, component, now)
		}
		k := r.Key + "|" + r.Instance
		if _, dup := out[k]; dup {
			t.Errorf("row %s written twice", k)
		}
		out[k] = r.Value
	}
	return out
}

func diffRows(t *testing.T, got, want map[string]float64) {
	t.Helper()
	for _, k := range slices.Sorted(maps.Keys(want)) {
		if g, ok := got[k]; !ok {
			t.Errorf("missing %s (want %v)", k, want[k])
		} else if g != want[k] {
			t.Errorf("%s = %v, want %v", k, g, want[k])
		}
	}
	for _, k := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected %s = %v", k, got[k])
		}
	}
}

// TestRetentionSamplesMapAPassToItsRows (#1629): a fixed pass report and
// storage status map to exactly one deleted count and one behind flag per
// lane the pass pruned, a behind flag per compaction it ran, the pass's
// duration, failures and partition roll, the mark lags from now, and the
// status read's partitions and sizes.
func TestRetentionSamplesMapAPassToItsRows(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	rep := retentionReport{
		Deleted:    map[string]int64{"metric": 120, "property": 0, "metric_trend": 3, "log_line/info": 10000},
		Behind:     []string{"metric_trend compaction", "log_line/info"},
		Created:    []string{"metric_p20261112"},
		Dropped:    []string{"metric_p20261024", "metric_p20261025"},
		Trends:     storage.TrendCompaction{Hours: 2, Through: now.Add(-25*time.Hour - 30*time.Minute)},
		Blocks:     storage.BlockCompaction{Days: 1, Through: now.Add(-36 * time.Hour)},
		Compacted:  true,
		Duration:   2500 * time.Millisecond,
		Failures:   1,
		MetricDays: 14,
	}
	status := &storage.RetentionStatus{
		Partitions: []storage.MetricPartition{
			{Name: "metric_p20261026", From: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)},
			{Name: "metric_p20261027", From: time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)},
			{Name: "metric_p20261112", From: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)},
		},
		TableBytes:    map[string]int64{"metric": 81920, "metric_trend": 16384, "log_line": 8192},
		DatabaseBytes: 9 << 20,
	}
	got := rowsByKey(t, retentionSamples("srv", rep, status, now), "srv", now)
	want := map[string]float64{
		writerclass.RetentionDeletedCount + "|metric":            120,
		writerclass.RetentionDeletedCount + "|property":          0,
		writerclass.RetentionDeletedCount + "|metric_trend":      3,
		writerclass.RetentionDeletedCount + "|log_line/info":     10000,
		writerclass.RetentionBehind + "|metric":                  0,
		writerclass.RetentionBehind + "|property":                0,
		writerclass.RetentionBehind + "|metric_trend":            0,
		writerclass.RetentionBehind + "|log_line/info":           1,
		writerclass.RetentionBehind + "|metric_trend compaction": 1,
		writerclass.RetentionBehind + "|metric_block compaction": 0,
		writerclass.RetentionPassDuration + "|":                  2.5,
		writerclass.RetentionPassFailureCount + "|":              1,
		writerclass.MetricPartitionCreatedCount + "|":            1,
		writerclass.MetricPartitionDroppedCount + "|":            2,
		writerclass.MetricTrendMarkLag + "|":                     25.5,
		writerclass.MetricBlockMarkLag + "|":                     1.5,
		writerclass.RetentionWindowDays + "|metric":              14,
		writerclass.MetricPartitionCount + "|":                   3,
		writerclass.MetricOldestDayAge + "|":                     14.5,
		writerclass.TableBytes + "|metric":                       81920,
		writerclass.TableBytes + "|metric_trend":                 16384,
		writerclass.TableBytes + "|log_line":                     8192,
		writerclass.DatabaseBytes + "|":                          9 << 20,
	}
	diffRows(t, got, want)
}

// TestRetentionSamplesOmitWhatThePassDidNotDo (#1629): an empty trend or
// block compaction (no mark yet) writes no lag, a pass that ran no
// compaction writes no compaction flag and no window, a failed status read
// writes no status rows, and a server with no platform component writes
// nothing at all.
func TestRetentionSamplesOmitWhatThePassDidNotDo(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 30, 0, 0, time.UTC)
	rep := retentionReport{Deleted: map[string]int64{"metric": 4}, Failures: 1, Duration: time.Second}
	got := rowsByKey(t, retentionSamples("srv", rep, nil, now), "srv", now)
	diffRows(t, got, map[string]float64{
		writerclass.RetentionDeletedCount + "|metric": 4,
		writerclass.RetentionBehind + "|metric":       0,
		writerclass.RetentionPassDuration + "|":       1,
		writerclass.RetentionPassFailureCount + "|":   1,
		writerclass.MetricPartitionCreatedCount + "|": 0,
		writerclass.MetricPartitionDroppedCount + "|": 0,
	})

	// The compactions ran but neither has a mark: their flags, no lags.
	rep = retentionReport{Deleted: map[string]int64{}, Compacted: true, MetricDays: 7}
	got = rowsByKey(t, retentionSamples("srv", rep, &storage.RetentionStatus{}, now), "srv", now)
	for _, k := range []string{writerclass.MetricTrendMarkLag, writerclass.MetricBlockMarkLag, writerclass.MetricOldestDayAge} {
		if _, ok := got[k+"|"]; ok {
			t.Errorf("%s written with no mark or partition to read it from", k)
		}
	}
	if got[writerclass.MetricPartitionCount+"|"] != 0 || got[writerclass.RetentionWindowDays+"|metric"] != 7 {
		t.Errorf("an empty status read %v, want a partition count of 0 and the 7-day window", got)
	}

	if rows := retentionSamples("", rep, &storage.RetentionStatus{}, now); rows != nil {
		t.Errorf("with no platform component the pass wrote %v, want nothing", rows)
	}
}

// TestRetentionSamplesKeyEveryLaneByItsName (#1629): an override lane
// (<lane>/<type>) and each log band lane is its own instance, keyed on the
// name retentionLanes gives it, so a lane added later reports with no change.
func TestRetentionSamplesKeyEveryLaneByItsName(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	lanes := []string{"metric/fan-speed", "property/power-state", "log_line/warn", "node_log/warn", "some_future_lane"}
	rep := retentionReport{Deleted: map[string]int64{}, Behind: []string{"node_log/warn"}}
	for i, l := range lanes {
		rep.Deleted[l] = int64(i + 1)
	}
	got := rowsByKey(t, retentionSamples("srv", rep, nil, now), "srv", now)
	for i, l := range lanes {
		if v, ok := got[writerclass.RetentionDeletedCount+"|"+l]; !ok || v != float64(i+1) {
			t.Errorf("deleted count for %s = %v (%v), want %d", l, v, ok, i+1)
		}
		behind := 0.0
		if l == "node_log/warn" {
			behind = 1
		}
		if v, ok := got[writerclass.RetentionBehind+"|"+l]; !ok || v != behind {
			t.Errorf("behind for %s = %v (%v), want %v", l, v, ok, behind)
		}
	}
	if n := len(got); n != 2*len(lanes)+4 {
		t.Errorf("%d rows, want %d: two per lane and the pass's four", n, 2*len(lanes)+4)
	}
}

// TestRetentionPassCountsItsFailuresAndCompactions (#1629): the report
// counts the pass's errors one by one and says whether it ran the
// compactions, which it does only on a valid metric window.
func TestRetentionPassCountsItsFailuresAndCompactions(t *testing.T) {
	ctx := t.Context()
	s := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{}, fail: map[string]error{"property": fmt.Errorf("boom"), "log_line/debug": fmt.Errorf("bang")}}
	rep, err := retentionPass(ctx, s, time.Now(), 10, 2, 0)
	if err == nil || rep.Failures != 2 {
		t.Errorf("failures = %d (err %v), want 2", rep.Failures, err)
	}
	if !rep.Compacted || rep.MetricDays != s.windows.MetricDays {
		t.Errorf("compacted %v window %d, want the compactions run on the %d-day window", rep.Compacted, rep.MetricDays, s.windows.MetricDays)
	}
	s = &backlogStore{windows: defaultWindows(), backlog: map[string]int64{}}
	s.windows.MetricDays = 0
	rep, _ = retentionPass(ctx, s, time.Now(), 10, 2, 0)
	if rep.Compacted {
		t.Error("a pass with no valid metric window ran the compactions")
	}
	if rep.Failures != 1 {
		t.Errorf("failures = %d, want 1: the out-of-range metric lane", rep.Failures)
	}
}
