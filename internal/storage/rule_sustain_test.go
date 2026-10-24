package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestRuleSustains proves the sustains: a fire must hold for its sustain
// before the alarm opens, whether a later sample or the sweep (a
// transition-only input sends no later sample) finds it held; a spike
// shorter than the sustain opens nothing; a recovery shorter than the clear
// sustain holds the incident open; and the streak survives a restart.
func TestRuleSustains(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	gw, all := f.gw, scope.Set{All: true}
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "reach-lost", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), FireSustain: 10 * time.Second, Clear: ptrCriteria(criteria("eq", "up")),
		ClearSustain: 10 * time.Second, Severity: "high"}, all); err != nil {
		t.Fatal(err)
	}
	// A streak is timed by the server's receipt clock, so the sweep is
	// handed times relative to now, and elapse stands in for server time
	// passing between two samples by moving the pending streaks back.
	prop := func(instance, v string) {
		t.Helper()
		if err := insertProperties(ctx, gw, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
			Key: "endpoint-reachable", Instance: instance, Value: v, TS: time.Now().UTC()}}); err != nil {
			t.Fatal(err)
		}
	}
	elapse := func(d time.Duration) {
		t.Helper()
		if _, err := f.conn.Exec(ctx, `update event_rule_state set since = since - make_interval(secs => $1)`, d.Seconds()); err != nil {
			t.Fatal(err)
		}
	}
	// Count this rule's alarms only: the official reachability rule reads
	// the same key and sweeps on its own sustain.
	open := func() int {
		t.Helper()
		return openRuleAlarms(t, f, "reach-lost")
	}
	sweep := func(after time.Duration) {
		t.Helper()
		if err := gw.SweepEventRules(ctx, time.Now().Add(after)); err != nil {
			t.Fatal(err)
		}
	}

	// A spike shorter than the sustain opens nothing.
	prop("tcp", "down")
	sweep(5 * time.Second)
	prop("tcp", "up")
	sweep(20 * time.Second)
	if n := open(); n != 0 {
		t.Fatalf("a spike under a 10s sustain opened %d alarms", n)
	}

	// Held past the sustain with no further sample: the sweep opens it, and
	// the streak survives a restart (a fresh gateway on the same database).
	prop("tcp", "down")
	sweep(9 * time.Second)
	if n := open(); n != 0 {
		t.Fatalf("under 10s into a 10s sustain the sweep opened %d alarms", n)
	}
	restarted, err := storage.NewPG(ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := restarted.SweepEventRules(ctx, time.Now().Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := open(); n != 1 {
		t.Fatalf("held past the sustain, after a restart, the sweep left %d alarms open, want 1", n)
	}

	// A recovery shorter than the clear sustain holds it; a longer one clears.
	prop("tcp", "up")
	prop("tcp", "down")
	sweep(30 * time.Second)
	if n := open(); n != 1 {
		t.Fatalf("a recovery broken before its 10s clear sustain left %d alarms, want still 1", n)
	}
	prop("tcp", "up")
	sweep(11 * time.Second)
	if n := open(); n != 0 {
		t.Fatalf("a recovery held past its clear sustain left %d alarms", n)
	}

	// A later sample past the sustain acts at once, without the sweep.
	prop("icmp", "down")
	elapse(12 * time.Second)
	prop("icmp", "down")
	if n := open(); n != 1 {
		t.Fatalf("a second down 12s into a 10s sustain left %d alarms, want 1", n)
	}
}
