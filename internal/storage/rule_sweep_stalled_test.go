package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// seenWithin is how recently a peer must have been seen to be judged, as
// the bus's peer timer reads it.
const seenWithin = 20 * time.Second

// sweepAgeOf is the age the shared database holds for the server whose
// platform component is comp, and whether it holds one for a live server.
func sweepAgeOf(t *testing.T, ctx context.Context, gw *storage.PG, comp string) (time.Duration, bool) {
	t.Helper()
	ages, err := gw.ServerSweepAges(ctx, seenWithin)
	if err != nil {
		t.Fatalf("read server sweep ages: %v", err)
	}
	for _, a := range ages {
		if a.ComponentID == comp {
			return a.Age, true
		}
	}
	return 0, false
}

// TestRuleSweepStalledOpensFromAPeersRead proves the seeded rule-sweep-stalled
// rule (#1986) through the shared database, as a second server sees it. Server
// A stamps its sweep; the stamp is the age B's evaluation writes on A's
// component. A sweep that stops stamping ages past 30 s and opens the alarm
// on A, severity high, with no sustain; the stamp resuming reads back young
// and clears it. A server that stopped cleanly released its stamp and is not
// judged, and nor is one that died: it stops being seen.
func TestRuleSweepStalledOpensFromAPeersRead(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ctx, dsn := platformGatewayDSN(t)
	a := serverComponent(t, ctx, gw, "sweep-a")
	b := serverComponent(t, ctx, gw, "sweep-b")
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	if _, ok := sweepAgeOf(t, ctx, gw, a.ID); ok {
		t.Fatal("a server that never swept has a sweep age; nothing has begun to age")
	}
	if err := gw.StampRuleSweep(ctx, a.ID); err != nil {
		t.Fatalf("stamp A's sweep: %v", err)
	}
	if err := gw.StampRuleSweep(ctx, b.ID); err != nil {
		t.Fatalf("stamp B's sweep: %v", err)
	}
	if _, ok := sweepAgeOf(t, ctx, gw, a.ID); ok {
		t.Fatal("a server whose peer timer has not run is judged; it is not yet seen alive")
	}
	for _, c := range []string{a.ID, b.ID} {
		if err := gw.MarkServerSeen(ctx, c); err != nil {
			t.Fatalf("mark %s seen: %v", c, err)
		}
	}
	if age, ok := sweepAgeOf(t, ctx, gw, a.ID); !ok || age > 5*time.Second {
		t.Fatalf("A's age right after its stamp = %v (held %v), want about zero", age, ok)
	}

	// B reads A's age and writes it on A's component, as the peer timer does.
	record := func() {
		t.Helper()
		age, ok := sweepAgeOf(t, ctx, gw, a.ID)
		if !ok {
			t.Fatal("A's sweep age is not held")
		}
		if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: a.ID,
			Key: writerclass.RuleSweepAge, Value: age.Seconds(), TS: time.Now().UTC()}}); err != nil {
			t.Fatalf("record rule-sweep-age: %v", err)
		}
		if err := gw.SweepEventRules(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	record()
	if n := openAlarmsByRule(t, ctx, gw, a.Name, "rule-sweep-stalled"); n != 0 {
		t.Fatalf("a sweep that just ran opened %d rule-sweep-stalled alarms", n)
	}

	// A's sweep stops stamping: two minutes pass.
	if _, err := conn.Exec(ctx, `update platform_server set rule_swept_at = now() - interval '120 seconds' where platform_component_id = $1`, a.ID); err != nil {
		t.Fatalf("age A's stamp: %v", err)
	}
	record()
	if n := openAlarmsByRule(t, ctx, gw, a.Name, "rule-sweep-stalled"); n != 1 {
		t.Fatalf("a sweep stalled 120 s left %d rule-sweep-stalled alarms open on A, want 1", n)
	}
	if n := openAlarmsByRule(t, ctx, gw, b.Name, "rule-sweep-stalled"); n != 0 {
		t.Fatalf("B's live sweep opened %d rule-sweep-stalled alarms", n)
	}
	alarms, err := gw.ListAlarms(ctx, a.Name, storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, al := range alarms {
		if al.RaisedByRule != nil && *al.RaisedByRule == "rule-sweep-stalled" && al.Severity != "high" {
			t.Errorf("rule-sweep-stalled raised at %q, want high", al.Severity)
		}
	}

	// The sweep resumes.
	if err := gw.StampRuleSweep(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	record()
	if n := openAlarmsByRule(t, ctx, gw, a.Name, "rule-sweep-stalled"); n != 0 {
		t.Fatalf("a resumed sweep left %d rule-sweep-stalled alarms open, want it cleared", n)
	}

	// A dies: its stamps stay, but it is no longer seen. Once its last seen
	// passes the window it drops out of the read, so no peer raises a
	// rule-sweep-stalled that nothing would ever clear.
	if _, err := conn.Exec(ctx, `update platform_server set rule_swept_at = now() - interval '120 seconds',
		seen_at = now() - interval '125 seconds' where platform_component_id = $1`, a.ID); err != nil {
		t.Fatalf("age A's stamps: %v", err)
	}
	if _, ok := sweepAgeOf(t, ctx, gw, a.ID); ok {
		t.Error("a server no longer seen is still judged by its peers")
	}
	if _, ok := sweepAgeOf(t, ctx, gw, b.ID); !ok {
		t.Error("a live peer dropped out of the read with the dead one")
	}

	// A stops cleanly: its stamps are released, so no peer judges it.
	if err := gw.StampRuleSweep(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := gw.MarkServerSeen(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := gw.ReleaseRuleSweep(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := sweepAgeOf(t, ctx, gw, a.ID); ok {
		t.Error("a server that stopped cleanly still holds a sweep age")
	}
}
