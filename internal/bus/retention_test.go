package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// backlogStore serves the retention windows it is given and a backlog of rows
// per lane, deleting at most the limit per call, and records each call.
type backlogStore struct {
	windows   settings.RetentionSettings
	overrides []storage.TypeRetention
	backlog   map[string]int64
	fail      map[string]error
	calls     []pruneCall
	// The metric partition roll: how often it ran, the cutoff each drop was
	// asked for, and an error each half answers with.
	ensured   int
	drops     []time.Time
	ensureErr error
	dropErr   error
	// The trend compaction's answer.
	trends   storage.TrendCompaction
	trendErr error
}

// CompactMetricTrends records itself as a call on the lane "compact", so a
// test reads where it ran among the prunes, and answers with trends.
func (b *backlogStore) CompactMetricTrends(_ context.Context, maxHours, laneDays int) (storage.TrendCompaction, error) {
	b.calls = append(b.calls, pruneCall{"compact", time.Time{}, laneDays})
	return b.trends, b.trendErr
}

// PruneMetricTrends is the trend lane of the same store, keyed "metric_trend".
func (b *backlogStore) PruneMetricTrends(ctx context.Context, before time.Time, limit int) (int64, error) {
	return b.PruneSamples(ctx, "metric_trend", before, limit)
}

func (b *backlogStore) EnsureMetricPartitions(context.Context) ([]string, error) {
	b.ensured++
	if b.ensureErr != nil {
		return nil, b.ensureErr
	}
	return []string{"metric_p209901"}, nil
}

func (b *backlogStore) DropMetricPartitions(_ context.Context, before time.Time) ([]string, error) {
	b.drops = append(b.drops, before)
	if b.dropErr != nil {
		return nil, b.dropErr
	}
	return []string{"metric_p200001"}, nil
}

type pruneCall struct {
	lane   string
	before time.Time
	limit  int
}

func (b *backlogStore) RetentionWindows(context.Context) (settings.RetentionSettings, error) {
	return b.windows, nil
}

func (b *backlogStore) RetentionOverrides(context.Context) ([]storage.TypeRetention, error) {
	return b.overrides, nil
}

// PruneSamplesOfType is a typed lane of the same store, keyed "<lane>#<type id>".
func (b *backlogStore) PruneSamplesOfType(ctx context.Context, lane, typeID string, _ int, before time.Time, limit int) (int64, error) {
	return b.PruneSamples(ctx, lane+"#"+typeID, before, limit)
}

// PruneLogs is a log lane of the same store, keyed "<table>/<band>".
func (b *backlogStore) PruneLogs(ctx context.Context, table, band string, before time.Time, limit int) (int64, error) {
	return b.PruneSamples(ctx, table+"/"+band, before, limit)
}

func (b *backlogStore) PruneSamples(_ context.Context, lane string, before time.Time, limit int) (int64, error) {
	b.calls = append(b.calls, pruneCall{lane, before, limit})
	if err := b.fail[lane]; err != nil {
		return 0, err
	}
	n := min(b.backlog[lane], int64(limit))
	b.backlog[lane] -= n
	return n, nil
}

func defaultWindows() settings.RetentionSettings {
	return settings.RetentionSettings{MetricDays: 14, PropertyDays: 90, TrendDays: 730,
		DebugLogDays: 3, InfoLogDays: 14, WarningLogDays: 30, ErrorLogDays: 90}
}

// The pass prunes both log tables too (#417 PR 2), each severity band to its
// own window, through the same chunked loop as the sample lanes.
func TestARetentionPassPrunesEachLogBandToItsWindow(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	store := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{"log_line/debug": 7, "node_log/error": 2}}
	rep, err := retentionPass(context.Background(), store, now, 100, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	cutoff := map[string]time.Time{}
	for _, c := range store.calls {
		cutoff[c.lane] = c.before
	}
	for _, table := range []string{"log_line", "node_log"} {
		for band, days := range map[string]int{"debug": 3, "info": 14, "warning": 30, "error": 90} {
			lane := table + "/" + band
			if want := now.AddDate(0, 0, -days); !cutoff[lane].Equal(want) {
				t.Errorf("%s cutoff %v, want %d days back", lane, cutoff[lane], days)
			}
		}
	}
	if rep.Deleted["log_line/debug"] != 7 || rep.Deleted["node_log/error"] != 2 {
		t.Errorf("report %+v, want 7 log_line debug and 2 node_log error lines removed", rep.Deleted)
	}
}

// TestARetentionPassPrunesEachLaneToItsWindow is #417's pass: each sample lane
// is pruned to its own window, counted back from now, and the pass reports
// what it removed.
func TestARetentionPassPrunesEachLaneToItsWindow(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	store := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{"metric": 3, "property": 1}}
	rep, err := retentionPass(context.Background(), store, now, 100, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	cutoff := map[string]time.Time{}
	for _, c := range store.calls {
		cutoff[c.lane] = c.before
	}
	if want := now.AddDate(0, 0, -14); !cutoff["metric"].Equal(want) {
		t.Errorf("metric cutoff %v, want 14 days back (%v)", cutoff["metric"], want)
	}
	if want := now.AddDate(0, 0, -90); !cutoff["property"].Equal(want) {
		t.Errorf("property cutoff %v, want 90 days back (%v)", cutoff["property"], want)
	}
	if rep.Deleted["metric"] != 3 || rep.Deleted["property"] != 1 || len(rep.Behind) != 0 {
		t.Errorf("report %+v, want 3 metric and 1 property rows removed, nothing left behind", rep)
	}
}

// A backlog larger than one chunk drains chunk by chunk within the pass, and
// the pass stops at the first short chunk: there is nothing more to find.
func TestARetentionPassDrainsABacklogInChunks(t *testing.T) {
	store := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{"metric": 25}}
	rep, err := retentionPass(context.Background(), store, time.Now(), 10, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	var metric []int
	for _, c := range store.calls {
		if c.lane == "metric" {
			metric = append(metric, c.limit)
		}
	}
	if len(metric) != 3 || rep.Deleted["metric"] != 25 {
		t.Fatalf("metric calls %v deleting %d, want three chunks of 10 deleting 25", metric, rep.Deleted["metric"])
	}
}

// A pass is bounded: a backlog beyond its chunk budget stops at the budget,
// names the lane still behind, and the next pass carries on.
func TestARetentionPassIsBoundedAndSaysWhatIsLeft(t *testing.T) {
	store := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{"metric": 1000}}
	rep, err := retentionPass(context.Background(), store, time.Now(), 10, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Deleted["metric"] != 50 || len(rep.Behind) != 1 || rep.Behind[0] != "metric" {
		t.Fatalf("report %+v, want 50 metric rows removed and metric named as behind", rep)
	}
	if rep, err = retentionPass(context.Background(), store, time.Now(), 10, 5, 0); err != nil || rep.Deleted["metric"] != 50 {
		t.Fatalf("the next pass removed %d (err %v), want it to carry on with 50", rep.Deleted["metric"], err)
	}
}

// One lane failing does not stop the other: the pass prunes what it can and
// returns the failure for the loop to log.
func TestARetentionPassPrunesTheOtherLaneWhenOneFails(t *testing.T) {
	store := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{"property": 4},
		fail: map[string]error{"metric": errors.New("boom")}}
	rep, err := retentionPass(context.Background(), store, time.Now(), 10, 5, 0)
	if err == nil {
		t.Fatal("a failed metric prune returned no error")
	}
	if rep.Deleted["property"] != 4 {
		t.Fatalf("property removed %d, want 4 despite the metric failure", rep.Deleted["property"])
	}
	if len(rep.Behind) != 1 || rep.Behind[0] != "metric" {
		t.Fatalf("behind %v, want the failed metric lane, so the next pass comes in a minute", rep.Behind)
	}
}

// stallStore stalls the metric lane until its context ends, as a chunk that
// outlives its deadline would.
type stallStore struct{ backlogStore }

func (s *stallStore) PruneSamples(ctx context.Context, lane string, before time.Time, limit int) (int64, error) {
	if lane == "metric" {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return s.backlogStore.PruneSamples(ctx, lane, before, limit)
}

// Each lane has a deadline of its own: a metric lane that runs out of time is
// behind, and the property lane still gets its own time rather than starting
// on an expired context.
func TestARetentionLaneThatRunsOutOfTimeIsBehindAndTheOtherStillPrunes(t *testing.T) {
	store := &stallStore{backlogStore{windows: defaultWindows(), backlog: map[string]int64{"property": 3}}}
	rep, err := retentionPass(context.Background(), store, time.Now(), 10, 5, 20*time.Millisecond)
	if err == nil {
		t.Fatal("a lane past its deadline returned no error")
	}
	if len(rep.Behind) != 1 || rep.Behind[0] != "metric" || rep.Deleted["property"] != 3 {
		t.Fatalf("report %+v, want metric behind and 3 property rows removed", rep)
	}
}

// A window outside the settings' range (stored past the validator, say by hand
// in the database) prunes nothing on its lane: a cutoff a billion days back
// overflows the timestamp encoding and would reach the database as a date every
// row is older than. The pass refuses the lane and says so.
func TestARetentionPassRefusesAWindowOutOfRange(t *testing.T) {
	w := defaultWindows()
	w.MetricDays = 999999999
	store := &backlogStore{windows: w, backlog: map[string]int64{"metric": 10, "property": 2}}
	rep, err := retentionPass(context.Background(), store, time.Now(), 10, 5, 0)
	if err == nil {
		t.Fatal("an out-of-range window returned no error")
	}
	for _, c := range store.calls {
		if c.lane == "metric" {
			t.Fatalf("the metric lane was pruned with cutoff %v, want it refused", c.before)
		}
	}
	if rep.Deleted["property"] != 2 {
		t.Fatalf("property removed %d, want 2: the other lane still prunes", rep.Deleted["property"])
	}
}

// The pass runs hourly while it keeps up, since its last, short chunk can walk
// much of a table once BRIN ranges widen (#420 ends that), and comes back within
// a minute while a lane is behind, so a backlog still drains at the chunk
// budget's pace rather than an hour's.
func TestTheRetentionPassComesBackSoonerWhileBehind(t *testing.T) {
	if got := nextRetentionWait(retentionReport{}); got != time.Hour {
		t.Errorf("a pass that kept up waits %v, want an hour", got)
	}
	if got := nextRetentionWait(retentionReport{Behind: []string{"metric"}}); got != time.Minute {
		t.Errorf("a pass that left metric behind waits %v, want a minute", got)
	}
}

// A type with a window of its own (#417, PR 3) gets a lane of its own, pruned
// to that window; the lane's own prune skips it (storage's job, held there).
func TestARetentionPassPrunesATypeByItsOwnWindow(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	store := &backlogStore{windows: defaultWindows(),
		overrides: []storage.TypeRetention{{Lane: "metric", TypeID: "t-1", TypeName: "tcp-connect-time", Days: 3},
			{Lane: "property", TypeID: "t-2", TypeName: "endpoint-reachable", Days: 365}},
		backlog: map[string]int64{"metric#t-1": 4}}
	rep, err := retentionPass(context.Background(), store, now, 100, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	cutoff := map[string]time.Time{}
	for _, c := range store.calls {
		cutoff[c.lane] = c.before
	}
	if want := now.AddDate(0, 0, -3); !cutoff["metric#t-1"].Equal(want) {
		t.Errorf("tcp-connect-time cutoff %v, want 3 days back", cutoff["metric#t-1"])
	}
	if want := now.AddDate(0, 0, -365); !cutoff["property#t-2"].Equal(want) {
		t.Errorf("endpoint-reachable cutoff %v, want 365 days back", cutoff["property#t-2"])
	}
	if rep.Deleted["metric/tcp-connect-time"] != 4 {
		t.Errorf("report %+v, want 4 tcp-connect-time rows under metric/tcp-connect-time", rep.Deleted)
	}
}

// Each pass rolls the metric table's months (#420) before it prunes: the
// partitions ahead made, and the months wholly past the metric window
// dropped, the cutoff the metric lane's own. A roll that fails is reported,
// and every lane is still pruned.
func TestRetentionPassRollsTheMetricMonths(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{}}
	rep, err := retentionPass(context.Background(), store, now, 10, 5, 0)
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if store.ensured != 1 || len(store.drops) != 1 || !store.drops[0].Equal(now.AddDate(0, 0, -14)) {
		t.Errorf("roll: ensured %d, drops %v; want one of each, dropping before %v", store.ensured, store.drops, now.AddDate(0, 0, -14))
	}
	if len(rep.Created) != 1 || len(rep.Dropped) != 1 {
		t.Errorf("report: created %v, dropped %v; want what the roll said", rep.Created, rep.Dropped)
	}

	failing := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{"metric": 3},
		ensureErr: errors.New("lock timeout"), dropErr: errors.New("lock timeout")}
	rep, err = retentionPass(context.Background(), failing, now, 10, 5, 0)
	if err == nil {
		t.Fatal("a failed roll: no error, want it reported")
	}
	if rep.Deleted["metric"] != 3 {
		t.Errorf("a failed roll: metric lane deleted %d, want its 3 still pruned", rep.Deleted["metric"])
	}
}

// A node override (#1350) moves node_log's band and only node_log's: with the
// node debug window at 1 day, node_log/debug is cut 1 day back while
// log_line/debug keeps the shared 3, and an unset band follows the shared
// window on both tables.
func TestARetentionPassPrunesNodeLogsByTheirOverride(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	w := defaultWindows()
	w.NodeDebugLogDays = 1
	store := &backlogStore{windows: w, backlog: map[string]int64{}}
	if _, err := retentionPass(context.Background(), store, now, 100, 10, 0); err != nil {
		t.Fatal(err)
	}
	cutoff := map[string]time.Time{}
	for _, c := range store.calls {
		cutoff[c.lane] = c.before
	}
	for lane, days := range map[string]int{"node_log/debug": 1, "log_line/debug": 3, "node_log/info": 14, "log_line/info": 14, "node_log/error": 90} {
		if want := now.AddDate(0, 0, -days); !cutoff[lane].Equal(want) {
			t.Errorf("%s cutoff %v, want %d days back", lane, cutoff[lane], days)
		}
	}
}

// A node override stored out of range (past the settings validator) is
// refused like any window, not read as "follow the shared band".
func TestARetentionPassRefusesANodeOverrideOutOfRange(t *testing.T) {
	w := defaultWindows()
	w.NodeErrorLogDays = settings.RetentionMaxDays + 1
	store := &backlogStore{windows: w, backlog: map[string]int64{}}
	_, err := retentionPass(context.Background(), store, time.Now(), 100, 10, 0)
	if err == nil {
		t.Fatal("pass took a node override past the ceiling, want it refused")
	}
	for _, c := range store.calls {
		if c.lane == "node_log/error" {
			t.Errorf("node_log/error pruned with an out-of-range override")
		}
	}
}

// Each pass closes the hours past the horizon into trends before it prunes
// anything (#1276), with the metric lane's window, and prunes the trends to the
// trend window. A compaction left behind, or failed, brings the next pass in a
// minute; the lanes are still pruned.
func TestRetentionPassCompactsTrendsBeforeItPrunes(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{"metric": 3, "metric_trend": 2}}
	rep, err := retentionPass(context.Background(), store, now, 10, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.calls) == 0 || store.calls[0].lane != "compact" || store.calls[0].limit != 14 {
		t.Fatalf("calls %+v, want the compaction first, with the metric window of 14 days", store.calls)
	}
	var trendCut time.Time
	for _, c := range store.calls {
		if c.lane == "metric_trend" {
			trendCut = c.before
		}
	}
	if !trendCut.Equal(now.AddDate(0, 0, -730)) || rep.Deleted["metric_trend"] != 2 {
		t.Errorf("trend lane cut %v deleting %d, want 730 days back deleting 2", trendCut, rep.Deleted["metric_trend"])
	}
	if nextRetentionWait(rep) != sampleRetentionEvery {
		t.Errorf("a pass that kept up waits %v", nextRetentionWait(rep))
	}

	behind := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{}, trends: storage.TrendCompaction{Hours: 48, Behind: true}}
	if rep, _ = retentionPass(context.Background(), behind, now, 10, 5, 0); nextRetentionWait(rep) != sampleRetentionCatchUp {
		t.Errorf("a compaction left behind waits %v, want the catch-up", nextRetentionWait(rep))
	}
	failed := &backlogStore{windows: defaultWindows(), backlog: map[string]int64{"metric": 1}, trendErr: errors.New("a write outlasted the drain")}
	rep, err = retentionPass(context.Background(), failed, now, 10, 5, 0)
	if err == nil || rep.Deleted["metric"] != 1 || nextRetentionWait(rep) != sampleRetentionCatchUp {
		t.Errorf("a failed compaction: err %v, metric deleted %d, wait %v; want reported, pruned, and a pass in a minute",
			err, rep.Deleted["metric"], nextRetentionWait(rep))
	}
}
