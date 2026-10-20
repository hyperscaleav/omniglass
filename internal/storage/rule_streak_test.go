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

// streakRig is a reachability rule on bar-1 with a fire sustain and an
// immediate clear, and the helpers its streak tests drive it with.
type streakRig struct {
	f    *healthFixture
	name string
}

func newStreakRig(t *testing.T, name string, sustain time.Duration) *streakRig {
	t.Helper()
	f := newHealthFixture(t)
	if _, err := f.gw.CreateEventRule(context.Background(), "", storage.EventRuleSpec{FireEventType: "condition-started", Name: name, SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), FireSustain: sustain, Clear: ptrCriteria(criteria("eq", "up")),
		Severity: "high"}, scope.Set{All: true}); err != nil {
		t.Fatal(err)
	}
	return &streakRig{f: f, name: name}
}

func (r *streakRig) observe(t *testing.T, instance, v string, at time.Time) {
	t.Helper()
	if err := r.f.gw.InsertPropertySamples(context.Background(), []storage.PropertySampleWrite{{OwnerKind: "component",
		OwnerID: "bar-1", Key: "endpoint-reachable", Instance: instance, Value: v, TS: at}}); err != nil {
		t.Fatal(err)
	}
}

func (r *streakRig) sweep(t *testing.T, at time.Time) {
	t.Helper()
	if err := r.f.gw.SweepEventRules(context.Background(), at); err != nil {
		t.Fatal(err)
	}
}

// open counts the rig rule's open alarms on bar-1 (the official
// reachability rule reads the same key and is not counted).
func (r *streakRig) open(t *testing.T) int {
	t.Helper()
	return openRuleAlarms(t, r.f, r.name)
}

func openRuleAlarms(t *testing.T, f *healthFixture, rule string) int {
	t.Helper()
	alarms, err := f.gw.ListAlarms(context.Background(), "bar-1", storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range alarms {
		if a.RaisedByRule != nil && *a.RaisedByRule == rule {
			n++
		}
	}
	return n
}

// The sweep and an ingest of the contrary sample serialize on the streak: a
// sweep that raises while an "up" lands concurrently must not leave the
// alarm open on an endpoint that reads up. The test holds bar-1's health
// lock so the sweep stalls mid-raise, lands the "up" while it is stalled,
// then lets both go.
func TestRuleSweepSerializesWithIngest(t *testing.T) {
	r := newStreakRig(t, "race", 10*time.Second)
	ctx := context.Background()
	now := time.Now().UTC()
	r.observe(t, "tcp", "down", now)
	var compID string
	if err := r.f.conn.QueryRow(ctx, `select id::text from component where name = 'bar-1'`).Scan(&compID); err != nil {
		t.Fatal(err)
	}
	lockConn, err := pgx.Connect(ctx, r.f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lockConn.Close(ctx) }()
	ltx, err := lockConn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ltx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, "health/component/"+compID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := r.f.gw.SweepEventRules(ctx, now.Add(11*time.Second)); err != nil {
			t.Error(err)
		}
	}()
	time.Sleep(700 * time.Millisecond)
	go func() {
		defer wg.Done()
		if err := r.f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
			Key: "endpoint-reachable", Instance: "tcp", Value: "up", TS: now.Add(12 * time.Second)}}); err != nil {
			t.Error(err)
		}
	}()
	time.Sleep(700 * time.Millisecond)
	_ = ltx.Rollback(ctx)
	wg.Wait()
	if n := r.open(t); n != 0 {
		t.Errorf("the endpoint reads up but the rule holds %d alarms open", n)
	}
}

// A sustain is timed by when the server received the sample, not by the
// node's clock: a node running a minute behind does not get its fire
// sustain waived on the next sweep.
func TestRuleSustainTimedByServerReceipt(t *testing.T) {
	r := newStreakRig(t, "skew", 15*time.Second)
	r.observe(t, "tcp", "down", time.Now().UTC().Add(-time.Minute))
	r.sweep(t, time.Now())
	if n := r.open(t); n != 0 {
		t.Fatalf("a down received just now, stamped a minute ago, fired past a 15s sustain (%d alarms)", n)
	}
	r.sweep(t, time.Now().Add(16*time.Second))
	if n := r.open(t); n != 1 {
		t.Fatalf("16s of server time past receipt the sweep left %d alarms, want 1", n)
	}
}

// Only a material edit restarts a streak: a label edit mid-sustain on a
// transition-only input keeps it, and the sweep still fires. A sustain edit
// restarts it, primed from the newest sample, so it still fires once the new
// sustain has held.
func TestRuleStreakSurvivesCosmeticEdit(t *testing.T) {
	r := newStreakRig(t, "edit", 10*time.Second)
	ctx, all := context.Background(), scope.Set{All: true}
	now := time.Now().UTC()
	r.observe(t, "tcp", "down", now)
	lbl := "Renamed"
	if _, err := r.f.gw.UpdateEventRule(ctx, "", "edit", storage.EventRulePatch{Label: &lbl}, all); err != nil {
		t.Fatal(err)
	}
	r.sweep(t, now.Add(5*time.Minute))
	if n := r.open(t); n != 1 {
		t.Fatalf("down 5m with a label edited mid-sustain: %d alarms, want 1", n)
	}

	r.observe(t, "tcp", "up", now.Add(6*time.Minute))
	r.observe(t, "icmp", "down", now.Add(6*time.Minute))
	longer := time.Hour
	if _, err := r.f.gw.UpdateEventRule(ctx, "", "edit", storage.EventRulePatch{FireSustain: &longer}, all); err != nil {
		t.Fatal(err)
	}
	r.sweep(t, time.Now().Add(30*time.Minute))
	if n := r.open(t); n != 0 {
		t.Fatalf("30m into a sustain edited to 1h: %d alarms, want 0", n)
	}
	r.sweep(t, time.Now().Add(61*time.Minute))
	if n := r.open(t); n != 1 {
		t.Fatalf("past the edited 1h sustain on a down nothing resent: %d alarms, want 1", n)
	}
}

// A rule created, or enabled, while its condition already holds primes from
// the newest sample, so a transition-only input that will send nothing more
// still fires once the sustain has held.
func TestRuleCreatedWhileConditionHolds(t *testing.T) {
	f := newHealthFixture(t)
	ctx, all := context.Background(), scope.Set{All: true}
	now := time.Now().UTC()
	if err := f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: now.Add(-time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "late", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), FireSustain: 10 * time.Second, Severity: "high"}, all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "at-once", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Severity: "high"}, all); err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "dormant", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), FireSustain: 10 * time.Second, Severity: "high", Enabled: &off}, all); err != nil {
		t.Fatal(err)
	}
	if n := openRuleAlarms(t, f, "at-once"); n != 1 {
		t.Fatalf("a zero-sustain rule created on a down: %d alarms, want 1 at once", n)
	}
	on := true
	if _, err := f.gw.UpdateEventRule(ctx, "", "dormant", storage.EventRulePatch{Enabled: &on}, all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.SweepEventRules(ctx, time.Now().Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{"late", "dormant"} {
		if n := openRuleAlarms(t, f, rule); n != 1 {
			t.Errorf("%s: endpoint down an hour, rule created or enabled, past its sustain: %d alarms, want 1", rule, n)
		}
	}
}

// The sweep does not act blind: it re-judges the series' newest sample
// before acting, so a streak the newest sample no longer supports (here a
// recovery written around the evaluator) is dropped, not fired.
func TestRuleSweepRejudgesNewestSample(t *testing.T) {
	r := newStreakRig(t, "rejudge", 10*time.Second)
	ctx := context.Background()
	now := time.Now().UTC()
	r.observe(t, "tcp", "down", now)
	if _, err := r.f.conn.Exec(ctx, `insert into property (ts, owner_kind, component_id, property_type_id, instance, value, provenance)
		select $1, 'component', c.id, t.id, 'tcp', to_jsonb('up'::text), 'observed'
		from component c, property_type t where c.name = 'bar-1' and t.name = 'endpoint-reachable'`, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	r.sweep(t, time.Now().Add(11*time.Second))
	if n := r.open(t); n != 0 {
		t.Fatalf("the newest sample reads up, yet the sweep fired %d alarms", n)
	}
	var left int
	if err := r.f.conn.QueryRow(ctx, `select count(*) from event_rule_state s join event_rule r on r.id = s.rule_id
		where r.name = 'rejudge'`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("the sweep left %d streaks the newest sample no longer supports", left)
	}
}

// An official rule settles from the seed the way an operator's does from an
// edit: a seed insert primes from the newest sample, a re-seed that changes
// only its wording keeps the streak, and one that changes its sustain
// restarts it.
func TestOfficialRuleSeedPrimes(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	if err := f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	spec := storage.EventRuleSpec{FireEventType: "condition-started", Name: "seeded", SampleKey: "endpoint-reachable", Fire: criteria("eq", "down"),
		FireSustain: time.Hour, Severity: "high"}
	if err := f.gw.UpsertEventRule(ctx, spec); err != nil {
		t.Fatal(err)
	}
	spec.Message = "reworded"
	if err := f.gw.UpsertEventRule(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.SweepEventRules(ctx, time.Now().Add(61*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := openRuleAlarms(t, f, "seeded"); n != 1 {
		t.Fatalf("seeded on a down, reworded, past its sustain: %d alarms, want 1", n)
	}
	spec.FireSustain = 2 * time.Hour
	spec.Fire = criteria("eq", "up")
	if err := f.gw.UpsertEventRule(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if n := openRuleAlarms(t, f, "seeded"); n != 0 {
		t.Fatalf("re-seeded to fire on up while the endpoint reads down: %d alarms, want the stale one cleared", n)
	}
}

// A rule write serializes with the samples it judges: an ingest that read a
// zero-sustain rule as enabled, and stalls mid-raise while the rule is
// disabled, must not leave an alarm open on a rule that is off (nothing
// would ever clear it). The test holds the rule's series lock on bar-1 so
// the ingest stalls after it has loaded the rule and before it raises,
// disables the rule meanwhile, then lets both go.
func TestRuleDisableSerializesWithIngest(t *testing.T) {
	r := newStreakRig(t, "gone", 0)
	ctx, all := context.Background(), scope.Set{All: true}
	var seriesKey string
	if err := r.f.conn.QueryRow(ctx, `select 'event_rule/' || r.id || '/' || c.id || '/tcp' from event_rule r, component c
		where r.name = 'gone' and c.name = 'bar-1'`).Scan(&seriesKey); err != nil {
		t.Fatal(err)
	}
	lockConn, err := pgx.Connect(ctx, r.f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lockConn.Close(ctx) }()
	ltx, err := lockConn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ltx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, seriesKey); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := r.f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
			Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now().UTC()}}); err != nil {
			t.Error(err)
		}
	}()
	time.Sleep(700 * time.Millisecond)
	go func() {
		defer wg.Done()
		off := false
		if _, err := r.f.gw.UpdateEventRule(ctx, "", "gone", storage.EventRulePatch{Enabled: &off}, all); err != nil {
			t.Error(err)
		}
	}()
	time.Sleep(700 * time.Millisecond)
	_ = ltx.Rollback(ctx)
	wg.Wait()
	if n := r.open(t); n != 0 {
		t.Errorf("the rule is disabled but holds %d alarms open", n)
	}
}
