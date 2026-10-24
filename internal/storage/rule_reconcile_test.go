package storage_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// holdLock takes an advisory lock on its own connection and transaction and
// returns the transaction, which the caller rolls back to let it go.
func holdLock(t *testing.T, f *healthFixture, key string) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
		t.Fatal(err)
	}
	return tx
}

func seriesLockKey(t *testing.T, f *healthFixture, rule, instance string) string {
	t.Helper()
	var key string
	if err := f.conn.QueryRow(context.Background(), `select 'event_rule/' || r.id || '/' || c.id || '/' || $2::text
		from event_rule r, component c where r.name = $1 and c.name = 'bar-1'`, rule, instance).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key
}

// reconcile runs one pass with st, or with a fresh state (a server just
// started) when st is nil.
func reconcile(t *testing.T, f *healthFixture, st *storage.RuleReconcileState) {
	t.Helper()
	if st == nil {
		st = &storage.RuleReconcileState{}
	}
	if err := f.gw.ReconcileEventRules(context.Background(), st); err != nil {
		t.Fatal(err)
	}
}

func maxSampleID(t *testing.T, f *healthFixture, table string) int64 {
	t.Helper()
	var id int64
	if err := f.conn.QueryRow(context.Background(), `select coalesce(max(id), 0) from `+table).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Window 1 of #928: a rule enabled while an ingest batch carrying its key is
// uncommitted primes from committed samples only, and the batch loaded its
// rules before the enable, so the batch's sample is judged by neither side.
// The test stalls the batch on the official reachability rule's series lock
// (after it has loaded its rules and landed the sample), enables the rule
// meanwhile, then lets the batch commit: the endpoint reads down and the rule
// holds nothing. One reconcile pass heals it.
func TestReconcileHealsEnableDuringBatch(t *testing.T) {
	f := newHealthFixture(t)
	ctx, all := context.Background(), scope.Set{All: true}
	off := false
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "late-on", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Clear: ptrCriteria(criteria("eq", "up")), Severity: "high", Enabled: &off}, all); err != nil {
		t.Fatal(err)
	}
	stall := holdLock(t, f, seriesLockKey(t, f, "reachability-lost", "tcp"))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := insertProperties(ctx, f.gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
			Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now().UTC()}}); err != nil {
			t.Error(err)
		}
	}()
	time.Sleep(700 * time.Millisecond)
	on := true
	if _, err := f.gw.UpdateEventRule(ctx, "", "late-on", storage.EventRulePatch{Enabled: &on}, all); err != nil {
		t.Fatal(err)
	}
	_ = stall.Rollback(ctx)
	wg.Wait()
	if n := openRuleAlarms(t, f, "late-on"); n != 0 {
		t.Fatalf("setup: the enable judged the in-flight sample (%d alarms); the window was not reproduced", n)
	}
	reconcile(t, f, nil)
	if n := openRuleAlarms(t, f, "late-on"); n != 1 {
		t.Fatalf("the endpoint reads down under an enabled zero-sustain rule; after a reconcile pass it holds %d alarms, want 1", n)
	}
}

// Window 2 of #928: an ingest batch holding a component's health lock (from
// a raise on an earlier sample) deadlocks with another transaction on the
// same component; Postgres aborts the batch's savepoint, the sample lands,
// and the rule's action on it is skipped. The test builds the deadlock
// directly: it holds the rule's series lock on icmp, lets the batch raise on
// tcp (taking bar-1's health lock) and block on icmp, then asks for the
// health lock, so the batch (waiting first) detects the cycle and aborts
// itself. One reconcile pass raises the alarm icmp's down called for.
func TestReconcileHealsDeadlockAbort(t *testing.T) {
	f := newHealthFixture(t)
	ctx, all := context.Background(), scope.Set{All: true}
	// A collected key: one batch carrying two of its series is a push's (or
	// a driver's instanced series), where a node-probe key's batch is one
	// endpoint's alone (ADR-0161).
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "dl", SampleKey: "video-input",
		Fire: criteria("eq", "hdmi1"), Clear: ptrCriteria(criteria("eq", "hdmi2")), Severity: "high"}, all); err != nil {
		t.Fatal(err)
	}
	var compID string
	if err := f.conn.QueryRow(ctx, `select id::text from component where name = 'bar-1'`).Scan(&compID); err != nil {
		t.Fatal(err)
	}
	other := holdLock(t, f, seriesLockKey(t, f, "dl", "icmp"))
	// The other side waits long before it looks for a deadlock, so the batch,
	// waiting first, is the one that finds the cycle and aborts.
	if _, err := other.Exec(ctx, `set local deadlock_timeout = '30s'`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	now := time.Now().UTC()
	go func() {
		defer wg.Done()
		if err := insertProperties(ctx, f.gw, []storage.PropertySampleWrite{
			{OwnerKind: "component", OwnerID: "bar-1", Key: "video-input", Instance: "tcp", Value: "hdmi1", TS: now},
			{OwnerKind: "component", OwnerID: "bar-1", Key: "video-input", Instance: "icmp", Value: "hdmi1", TS: now},
		}); err != nil {
			t.Error(err)
		}
	}()
	time.Sleep(700 * time.Millisecond)
	go func() {
		defer wg.Done()
		if _, err := other.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, "health/component/"+compID); err != nil {
			t.Error(err)
		}
		_ = other.Rollback(ctx)
	}()
	wg.Wait()
	var landed int
	if err := f.conn.QueryRow(ctx, `select count(*) from property p join property_type t on t.id = p.property_type_id
		where t.name = 'video-input' and p.instance = 'icmp'`).Scan(&landed); err != nil {
		t.Fatal(err)
	}
	if landed != 1 {
		t.Fatalf("setup: icmp's hdmi1 did not land (%d rows)", landed)
	}
	if n := openRuleAlarms(t, f, "dl"); n != 1 {
		t.Fatalf("setup: after the deadlock the rule holds %d alarms, want tcp's alone; the window was not reproduced", n)
	}
	reconcile(t, f, nil)
	if n := openRuleAlarms(t, f, "dl"); n != 2 {
		t.Fatalf("both series read hdmi1; after a reconcile pass the rule holds %d alarms, want 2", n)
	}
}

// A reconcile pass corrects only drift: a streak that agrees with the newest
// sample keeps its clock (a pass must never restart a sustain), and a pass
// reads only samples past its mark, so once the lag has moved past the
// drift's sample, nothing heals it until a later sample touches the series.
func TestReconcileLeavesAgreementAlone(t *testing.T) {
	r := newStreakRig(t, "steady", 10*time.Second)
	ctx := context.Background()
	r.observe(t, "tcp", "down", time.Now().UTC())
	since := func() time.Time {
		t.Helper()
		var s time.Time
		if err := r.f.conn.QueryRow(ctx, `select s.since from event_rule_state s join event_rule e on e.id = s.rule_id
			where e.name = 'steady' and s.instance = 'tcp'`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := since()
	time.Sleep(50 * time.Millisecond)
	st := &storage.RuleReconcileState{}
	reconcile(t, r.f, st)
	reconcile(t, r.f, st)
	if after := since(); !after.Equal(before) {
		t.Fatalf("a pass over an agreeing streak moved its clock from %v to %v", before, after)
	}

	// Drift left behind the mark: a streak deleted around the evaluator.
	if _, err := r.f.conn.Exec(ctx, `delete from event_rule_state s using event_rule e where e.id = s.rule_id and e.name = 'steady'`); err != nil {
		t.Fatal(err)
	}
	reconcile(t, r.f, st)
	var left int
	if err := r.f.conn.QueryRow(ctx, `select count(*) from event_rule_state s join event_rule e on e.id = s.rule_id
		where e.name = 'steady'`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("a pass past its marks re-judged a series no new sample touched")
	}
	reconcile(t, r.f, nil)
	if left := since(); left.IsZero() {
		t.Fatal("a fresh pass left the dropped streak unhealed")
	}
	r.sweep(t, time.Now().Add(11*time.Second))
	if n := r.open(t); n != 1 {
		t.Fatalf("the healed streak, past its sustain: %d alarms, want 1", n)
	}
}

// A restarted server's first pass reads back a bounded lookback, not the
// whole sample history (a race it could heal is one inside its own in-flight
// work), and its second pass does not reread from the start either. A
// drifted series older than the lookback is left; one inside it is healed.
func TestReconcileFirstPassIsBounded(t *testing.T) {
	r := newStreakRig(t, "bounded", 10*time.Second)
	ctx := context.Background()
	r.observe(t, "tcp", "down", time.Now().UTC())
	drop := func(instance string) {
		t.Helper()
		if _, err := r.f.conn.Exec(ctx, `delete from event_rule_state s using event_rule e where e.id = s.rule_id
			and e.name = 'bounded' and s.instance = $1`, instance); err != nil {
			t.Fatal(err)
		}
	}
	held := func(instance string) bool {
		t.Helper()
		var n int
		if err := r.f.conn.QueryRow(ctx, `select count(*) from event_rule_state s join event_rule e on e.id = s.rule_id
			where e.name = 'bounded' and s.instance = $1`, instance).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	drop("tcp")
	// Push the id sequence past the lookback: the tcp sample is now history.
	if _, err := r.f.conn.Exec(ctx, `select setval(pg_get_serial_sequence('public.property', 'id'), $1)`,
		maxSampleID(t, r.f, "property")+storage.RuleReconcileLookback+10); err != nil {
		t.Fatal(err)
	}
	r.observe(t, "icmp", "down", time.Now().UTC())
	drop("icmp")
	st := &storage.RuleReconcileState{}
	reconcile(t, r.f, st)
	reconcile(t, r.f, st)
	if !held("icmp") {
		t.Fatal("a first pass left drift inside its lookback unhealed")
	}
	if held("tcp") {
		t.Fatal("a first pass reread the sample history past its lookback")
	}
	if marks := storage.ExportRuleMarks(st); marks[0].Property == 0 {
		t.Fatalf("after two passes the marks %+v still read from the start", marks)
	}
}

// One series whose correction keeps failing does not pin the mark: the mark
// advances, the series is retried on its own, and after a few failed tries it
// is dropped (logged) rather than retried forever. The test makes a
// correction fail by holding the series' lock past the correction's lock
// timeout.
func TestReconcileRetriesAFailedSeriesWithoutPinningTheMark(t *testing.T) {
	f := newHealthFixture(t)
	ctx, all := context.Background(), scope.Set{All: true}
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "pin", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Clear: ptrCriteria(criteria("eq", "up")), Severity: "high"}, all); err != nil {
		t.Fatal(err)
	}
	// A down written around the evaluator: drift on the series.
	drift := func(instance string) {
		t.Helper()
		if _, err := f.conn.Exec(ctx, `insert into property (ts, owner_kind, component_id, property_type_id, instance, value, provenance)
			select now(), 'component', c.id, t.id, $1, to_jsonb('down'::text), 'observed'
			from component c, property_type t where c.name = 'bar-1' and t.name = 'endpoint-reachable'`, instance); err != nil {
			t.Fatal(err)
		}
	}
	open := func(instance string) bool {
		t.Helper()
		var n int
		if err := f.conn.QueryRow(ctx, `select count(*) from alarm a join event_rule r on r.id = a.raised_by_rule_id
			where r.name = 'pin' and a.dedup_key = 'rule:' || r.id::text || ':' || $1 and a.cleared_at is null`, instance).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}

	drift("tcp")
	st := &storage.RuleReconcileState{}
	lock := holdLock(t, f, seriesLockKey(t, f, "pin", "tcp"))
	reconcile(t, f, st)
	if marks := storage.ExportRuleMarks(st); marks[1].Property != maxSampleID(t, f, "property") {
		t.Fatalf("a failed correction pinned the mark: %+v, want the newest pass at %d", marks, maxSampleID(t, f, "property"))
	}
	_ = lock.Rollback(ctx)
	reconcile(t, f, st)
	reconcile(t, f, st) // the lag has moved past the sample: only the retry set holds it
	if !open("tcp") {
		t.Fatal("a series whose correction failed was not retried once its lock was free")
	}

	drift("icmp")
	lock = holdLock(t, f, seriesLockKey(t, f, "pin", "icmp"))
	for range storage.RuleReconcileRetries {
		reconcile(t, f, st)
	}
	_ = lock.Rollback(ctx)
	reconcile(t, f, st)
	if open("icmp") {
		t.Fatal("a series that failed every retry was still retried; it should have been dropped")
	}
}

// The metric lane reconciles the same way, and a selector still confines a
// rule: a series its rule does not watch is not drift, however it reads.
func TestReconcileMetricLaneAndSelector(t *testing.T) {
	f := newHealthFixture(t)
	ctx, all := context.Background(), scope.Set{All: true}
	if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-c"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "slow", SampleKey: "http-response-time",
		Fire: criteria("gt", 30), FireSustain: time.Minute, Severity: "warning",
		Selector: storage.EventRuleSelector{Components: []string{"bar-1"}}}, all); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"bar-1", "other-c"} {
		if err := insertMetrics(ctx, f.gw, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: c,
			Key: "http-response-time", Instance: "http", Value: 35, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	streaks := func() int {
		t.Helper()
		var n int
		if err := f.conn.QueryRow(ctx, `select count(*) from event_rule_state s join event_rule r on r.id = s.rule_id
			where r.name = 'slow'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := streaks(); n != 1 {
		t.Fatalf("setup: %d streaks, want bar-1's alone", n)
	}
	if _, err := f.conn.Exec(ctx, `delete from event_rule_state s using event_rule r where r.id = s.rule_id and r.name = 'slow'`); err != nil {
		t.Fatal(err)
	}
	st := &storage.RuleReconcileState{}
	reconcile(t, f, st)
	if n := streaks(); n != 1 {
		t.Fatalf("after a pass over a dropped metric streak: %d streaks, want bar-1's restored and other-c left alone", n)
	}
	if marks := storage.ExportRuleMarks(st); marks[1].Metric != maxSampleID(t, f, "metric") {
		t.Fatalf("a pass over landed metric samples left the metric mark at %+v", marks)
	}
	if got := (storage.RuleMark{Property: 3, Metric: 9}).Min(storage.RuleMark{Property: 5, Metric: 2}); got != (storage.RuleMark{Property: 3, Metric: 2}) {
		t.Fatalf("Min = %+v, want each lane's lower mark", got)
	}
}

// The newest-sample lookup the reconcile, the sweep and a rule write run per
// series is an index probe on each lane, on the series' own (component, type,
// instance, ts desc) index, never a read of the key's whole history. Seqscan
// is off for the question because the fixture's tables are tiny; what the
// planner reaches for instead is what it will choose once the lane is real.
func TestSeriesNewestIsAnIndexProbe(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.conn.Exec(ctx, "set enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ lane, index string }{
		{"property", "property_series_idx"},
		{"metric", "metric_owner_idx"},
	} {
		rows, err := f.conn.Query(ctx, "explain "+storage.ExportSeriesNewestSQL(tc.lane), uuid.NewString(), uuid.NewString(), "tcp")
		if err != nil {
			t.Fatalf("explain %s: %v", tc.lane, err)
		}
		var plan string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan += line + "\n"
		}
		rows.Close()
		if !strings.Contains(plan, tc.index) {
			t.Errorf("the %s lane's newest-sample lookup does not reach for %s:\n%s", tc.lane, tc.index, plan)
		}
	}
}
