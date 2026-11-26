package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// #2727: a throwaway lab's seed lands past derived state, a system's health
// edges and the alarms that explain them, raised and cleared at the time, each
// labelled with the seed's source so it stays apart from what the platform
// recorded itself. The health read serves the seeded edges as history, the
// member-alarm read serves the seeded alarms in their window, and the live
// verdict stays the platform's own.

func (f *healthFixture) idOf(t *testing.T, ctx context.Context, table, name string) string {
	t.Helper()
	var id string
	if err := f.conn.QueryRow(ctx, `select id::text from `+table+` where name = $1`, name).Scan(&id); err != nil {
		t.Fatalf("%s %s id: %v", table, name, err)
	}
	return id
}

// seriesByID reads one owner's health rows in id order: the order every
// reader of the live health uses. Its ts must rise with it.
func (f *healthFixture) seriesByID(t *testing.T, ctx context.Context, ownerKind, ownerID string) ([]string, []time.Time) {
	t.Helper()
	col := map[string]string{"component": "component_id", "system": "system_id", "location": "location_id"}[ownerKind]
	rows, err := f.conn.Query(ctx, `select value #>> '{}', ts from property
		where `+col+` = $1::uuid and property_type_id = (select id from property_type where name = 'health')
		order by id asc`, ownerID)
	if err != nil {
		t.Fatalf("read %s health: %v", ownerKind, err)
	}
	defer rows.Close()
	var vals []string
	var ts []time.Time
	for rows.Next() {
		var v string
		var at time.Time
		if err := rows.Scan(&v, &at); err != nil {
			t.Fatalf("scan: %v", err)
		}
		vals, ts = append(vals, v), append(ts, at)
	}
	return vals, ts
}

func TestLandSeededHealthServesThePastAsHistory(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	sys := f.idOf(t, ctx, "system", "hq-huddle")
	now := time.Now().UTC().Truncate(time.Second)
	start, broke := now.Add(-30*24*time.Hour), now.Add(-10*24*time.Hour)
	fixed := broke.Add(40 * time.Minute)
	edges := []storage.SeededHealthEdge{
		{OwnerKind: "system", OwnerID: sys, TS: start, Value: "healthy"},
		{OwnerKind: "system", OwnerID: sys, TS: broke, Value: "degraded"},
		{OwnerKind: "system", OwnerID: sys, TS: fixed, Value: "healthy"},
	}
	n, err := f.gw.LandSeededHealth(ctx, "devseed", edges)
	if err != nil || n != 3 {
		t.Fatalf("land seeded health = %d, %v; want 3 rows", n, err)
	}

	rep, err := f.gw.SystemHealth(ctx, "hq-huddle", storage.HealthRange{Window: 31 * 24 * time.Hour}, f.all, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if rep.FirstRecorded == nil || !rep.FirstRecorded.Equal(start) {
		t.Errorf("first recorded = %v, want the seeded start %v", rep.FirstRecorded, start)
	}
	// The whole series: the seeded past, then the system's own record (healthy
	// at creation, incomplete while its slot was declared and empty, healthy once
	// staffed). Its own first healthy carries on from the seeded recovery, so it
	// is not a second edge.
	var got []string
	for _, tr := range rep.Transitions {
		got = append(got, tr.Value)
	}
	if want := []string{"healthy", "degraded", "healthy", "incomplete", "healthy"}; !equalStrings(got, want) {
		t.Errorf("transitions = %v, want %v", got, want)
	}
	if rep.Verdict != "healthy" {
		t.Errorf("health = %q, want healthy", rep.Verdict)
	}
	// The series' invariants hold: no value follows itself, and the id order
	// every live read uses is the time order.
	f.assertTransitionOnly(t, ctx)
	_, ts := f.seriesByID(t, ctx, "system", sys)
	for i := 1; i < len(ts); i++ {
		if !ts[i].After(ts[i-1]) {
			t.Errorf("row %d at %v is not after row %d at %v: id order is not time order", i, ts[i], i-1, ts[i-1])
		}
	}
	var labelled int
	if err := f.conn.QueryRow(ctx, `select count(*) from property where system_id = $1::uuid and source = 'devseed'`, sys).Scan(&labelled); err != nil || labelled != 3 {
		t.Errorf("seeded health rows labelled devseed = %d (%v), want 3", labelled, err)
	}

	// The seed is its own sentinel: a second landing writes nothing.
	if n, err := f.gw.LandSeededHealth(ctx, "devseed", edges); err != nil || n != 0 {
		t.Errorf("second land = %d, %v; want 0 and no error", n, err)
	}
}

// A system whose health already moved before the seed keeps its live health:
// the seeded past goes before its own record, never after it, so the newest
// row is still the platform's.
func TestLandSeededHealthGoesBeforeTheLabsOwnRecord(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "high", Message: "No route to host"}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	sys := f.idOf(t, ctx, "system", "hq-huddle")
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := f.gw.LandSeededHealth(ctx, "devseed", []storage.SeededHealthEdge{
		{OwnerKind: "system", OwnerID: sys, TS: now.Add(-20 * 24 * time.Hour), Value: "healthy"},
		{OwnerKind: "system", OwnerID: sys, TS: now.Add(-5 * 24 * time.Hour), Value: "outage"},
		{OwnerKind: "system", OwnerID: sys, TS: now.Add(-5*24*time.Hour + time.Hour), Value: "healthy"},
	}); err != nil {
		t.Fatalf("land: %v", err)
	}
	vals, _ := f.seriesByID(t, ctx, "system", sys)
	if want := []string{"healthy", "outage", "healthy", "incomplete", "healthy", "degraded"}; !equalStrings(vals, want) {
		t.Errorf("series by id = %v, want %v (the seed, then the lab's own)", vals, want)
	}
	if rep, err := f.gw.SystemHealth(ctx, "hq-huddle", storage.HealthRange{}, f.all, f.all); err != nil || rep.Verdict != "degraded" {
		t.Errorf("live health = %v (%v), want degraded, the platform's own", rep, err)
	}
	f.assertTransitionOnly(t, ctx)
}

// The seed is the past: an edge at or after the owner's first recorded one
// would rewrite what the platform recorded, and is refused.
func TestLandSeededHealthRefusesAnEdgeAfterTheOwnRecordBegins(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	sys := f.idOf(t, ctx, "system", "hq-huddle")
	if _, err := f.gw.LandSeededHealth(ctx, "devseed", []storage.SeededHealthEdge{
		{OwnerKind: "system", OwnerID: sys, TS: time.Now().Add(time.Hour), Value: "degraded"},
	}); err == nil {
		t.Fatal("a seeded edge after the system's own record began landed")
	}
}

func TestLandSeededHealthRefusesAValueHealthDoesNotHave(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	sys := f.idOf(t, ctx, "system", "hq-huddle")
	if _, err := f.gw.LandSeededHealth(ctx, "devseed", []storage.SeededHealthEdge{
		{OwnerKind: "system", OwnerID: sys, TS: time.Now().Add(-time.Hour), Value: "commissioning"},
	}); err == nil {
		t.Fatal("a health value outside the vocabulary landed")
	}
}

func TestLandSeededAlarmsServesThemInTheirWindow(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	sys := f.idOf(t, ctx, "system", "hq-huddle")
	bar := f.idOf(t, ctx, "component", "bar-1")
	now := time.Now().UTC().Truncate(time.Second)
	raised := now.Add(-10 * 24 * time.Hour)
	cleared := raised.Add(40 * time.Minute)
	n, err := f.gw.LandSeededAlarms(ctx, "devseed", []storage.SeededAlarm{{
		ComponentID: bar, Rule: "reachability-lost", Instance: "snmp", Severity: "high",
		Message: "Reachability lost", RaisedAt: raised, ClearedAt: cleared,
	}})
	if err != nil || n != 1 {
		t.Fatalf("land seeded alarms = %d, %v; want 1", n, err)
	}

	got, err := f.gw.ListSystemAlarms(ctx, sys, storage.AlarmRange{Since: now.Add(-30 * 24 * time.Hour), Until: now}, 50)
	if err != nil {
		t.Fatalf("list system alarms: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("system alarms = %d, want the seeded one", len(got))
	}
	a := got[0]
	if !a.RaisedAt.Equal(raised) || a.ClearedAt == nil || !a.ClearedAt.Equal(cleared) {
		t.Errorf("alarm raised %v cleared %v, want %v and %v", a.RaisedAt, a.ClearedAt, raised, cleared)
	}
	if a.RaisedByRule == nil || *a.RaisedByRule != "reachability-lost" {
		t.Errorf("alarm rule = %v, want reachability-lost", a.RaisedByRule)
	}
	if a.Severity != "high" || a.ComponentName != "bar-1" {
		t.Errorf("alarm = %s on %s, want high on bar-1", a.Severity, a.ComponentName)
	}
	// A seeded alarm observed nothing: bar-1, which nothing collects from, still
	// reads unknown rather than healthy (#1454).
	if h, err := f.gw.ComponentHealth(ctx, bar); err != nil || h != "unknown" {
		t.Errorf("bar-1 health after a seeded alarm = %q (%v), want unknown", h, err)
	}
	var source string
	if err := f.conn.QueryRow(ctx, `select source from alarm where id = $1::uuid`, a.ID).Scan(&source); err != nil || source != "devseed" {
		t.Errorf("alarm source = %q (%v), want devseed", source, err)
	}
	// Seeding wrote no health of its own: the bar and its system read healthy now.
	if rep, err := f.gw.SystemHealth(ctx, "hq-huddle", storage.HealthRange{}, f.all, f.all); err != nil || rep.Verdict != "healthy" {
		t.Errorf("verdict after a seeded alarm = %v (%v), want healthy", rep, err)
	}

	if n, err := f.gw.LandSeededAlarms(ctx, "devseed", []storage.SeededAlarm{{
		ComponentID: bar, Rule: "reachability-lost", Instance: "snmp", Severity: "high",
		Message: "Reachability lost", RaisedAt: raised, ClearedAt: cleared,
	}}); err != nil || n != 0 {
		t.Errorf("second land = %d, %v; want 0 and no error", n, err)
	}
}

func TestLandSeededAlarmsRefusesARuleThatDoesNotExist(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	bar := f.idOf(t, ctx, "component", "bar-1")
	at := time.Now().Add(-time.Hour)
	if _, err := f.gw.LandSeededAlarms(ctx, "devseed", []storage.SeededAlarm{{
		ComponentID: bar, Rule: "no-such-rule", Severity: "high", Message: "x", RaisedAt: at, ClearedAt: at.Add(time.Minute),
	}}); err == nil {
		t.Fatal("an alarm naming no rule landed")
	}
}
