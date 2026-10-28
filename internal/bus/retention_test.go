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
