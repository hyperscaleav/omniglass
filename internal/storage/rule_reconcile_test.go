package storage_test

import (
	"context"
	"sync"
	"testing"
	"time"

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

func reconcile(t *testing.T, f *healthFixture, from storage.RuleMark) storage.RuleMark {
	t.Helper()
	next, err := f.gw.ReconcileEventRules(context.Background(), from)
	if err != nil {
		t.Fatal(err)
	}
	return next
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
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "late-on", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Clear: ptrCriteria(criteria("eq", "up")), Severity: "high", Enabled: &off}, all); err != nil {
		t.Fatal(err)
	}
	stall := holdLock(t, f, seriesLockKey(t, f, "reachability-lost", "tcp"))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
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
	reconcile(t, f, storage.RuleMark{})
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
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "dl", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Clear: ptrCriteria(criteria("eq", "up")), Severity: "high"}, all); err != nil {
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
		if err := f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{
			{OwnerKind: "component", OwnerID: "bar-1", Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: now},
			{OwnerKind: "component", OwnerID: "bar-1", Key: "endpoint-reachable", Instance: "icmp", Value: "down", TS: now},
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
		where t.name = 'endpoint-reachable' and p.instance = 'icmp'`).Scan(&landed); err != nil {
		t.Fatal(err)
	}
	if landed != 1 {
		t.Fatalf("setup: icmp's down did not land (%d rows)", landed)
	}
	if n := openRuleAlarms(t, f, "dl"); n != 1 {
		t.Fatalf("setup: after the deadlock the rule holds %d alarms, want tcp's alone; the window was not reproduced", n)
	}
	reconcile(t, f, storage.RuleMark{})
	if n := openRuleAlarms(t, f, "dl"); n != 2 {
		t.Fatalf("both endpoints read down; after a reconcile pass the rule holds %d alarms, want 2", n)
	}
}

// A reconcile pass corrects only drift: a streak that agrees with the newest
// sample keeps its clock (a pass must never restart a sustain), an alarm in
// force stays put, and a pass reads only samples past its mark, so a mark
// past the drift heals nothing until a later sample touches the series.
func TestReconcileLeavesAgreementAlone(t *testing.T) {
	r := newStreakRig(t, "steady", 10*time.Second)
	ctx := context.Background()
	start := time.Now().UTC()
	r.observe(t, "tcp", "down", start)
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
	mark := reconcile(t, r.f, storage.RuleMark{})
	if after := since(); !after.Equal(before) {
		t.Fatalf("a pass over an agreeing streak moved its clock from %v to %v", before, after)
	}
	if mark.Property == 0 {
		t.Fatalf("a pass over landed samples returned a zero property mark")
	}

	// Drift left behind the mark: a streak deleted around the evaluator.
	if _, err := r.f.conn.Exec(ctx, `delete from event_rule_state s using event_rule e where e.id = s.rule_id and e.name = 'steady'`); err != nil {
		t.Fatal(err)
	}
	if next := reconcile(t, r.f, mark); next != mark {
		t.Fatalf("a pass with nothing new moved the mark from %+v to %+v", mark, next)
	}
	var left int
	if err := r.f.conn.QueryRow(ctx, `select count(*) from event_rule_state s join event_rule e on e.id = s.rule_id
		where e.name = 'steady'`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("a pass past its mark re-judged a series no new sample touched")
	}
	reconcile(t, r.f, storage.RuleMark{})
	if left := since(); left.IsZero() {
		t.Fatal("a pass from the start left the dropped streak unhealed")
	}
	r.sweep(t, time.Now().Add(11*time.Second))
	if n := r.open(t); n != 1 {
		t.Fatalf("the healed streak, past its sustain: %d alarms, want 1", n)
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
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "hot", SampleKey: "room-temperature",
		Fire: criteria("gt", 30), FireSustain: time.Minute, Severity: "warning",
		Selector: storage.EventRuleSelector{Components: []string{"bar-1"}}}, all); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"bar-1", "other-c"} {
		if err := f.gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: c,
			Key: "room-temperature", Value: 35, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	streaks := func() int {
		t.Helper()
		var n int
		if err := f.conn.QueryRow(ctx, `select count(*) from event_rule_state s join event_rule r on r.id = s.rule_id
			where r.name = 'hot'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := streaks(); n != 1 {
		t.Fatalf("setup: %d streaks, want bar-1's alone", n)
	}
	if _, err := f.conn.Exec(ctx, `delete from event_rule_state s using event_rule r where r.id = s.rule_id and r.name = 'hot'`); err != nil {
		t.Fatal(err)
	}
	mark := reconcile(t, f, storage.RuleMark{})
	if n := streaks(); n != 1 {
		t.Fatalf("after a pass over a dropped metric streak: %d streaks, want bar-1's restored and other-c left alone", n)
	}
	if mark.Metric == 0 {
		t.Fatal("a pass over landed metric samples returned a zero metric mark")
	}
	if got := (storage.RuleMark{Property: 3, Metric: 9}).Min(storage.RuleMark{Property: 5, Metric: 2}); got != (storage.RuleMark{Property: 3, Metric: 2}) {
		t.Fatalf("Min = %+v, want each lane's lower mark", got)
	}
}
