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
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "reach-lost", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), FireSustain: 10 * time.Second, Clear: ptrCriteria(criteria("eq", "up")),
		ClearSustain: 10 * time.Second, Severity: "high"}, all); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	prop := func(instance, v string, at time.Time) {
		t.Helper()
		if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
			Key: "endpoint-reachable", Instance: instance, Value: v, TS: at}}); err != nil {
			t.Fatal(err)
		}
	}
	// Count this rule's alarms only: the official reachability rule reads
	// the same key and sweeps on its own sustain.
	open := func() int {
		t.Helper()
		alarms, err := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, a := range alarms {
			if a.RaisedByRule != nil && *a.RaisedByRule == "reach-lost" {
				n++
			}
		}
		return n
	}
	sweep := func(at time.Time) {
		t.Helper()
		if err := gw.SweepEventRules(ctx, at); err != nil {
			t.Fatal(err)
		}
	}

	// A spike shorter than the sustain opens nothing.
	prop("tcp", "down", now)
	sweep(now.Add(5 * time.Second))
	prop("tcp", "up", now.Add(6*time.Second))
	sweep(now.Add(20 * time.Second))
	if n := open(); n != 0 {
		t.Fatalf("a 6s spike under a 10s sustain opened %d alarms", n)
	}

	// Held past the sustain with no further sample: the sweep opens it, and
	// the streak survives a restart (a fresh gateway on the same database).
	t1 := now.Add(30 * time.Second)
	prop("tcp", "down", t1)
	sweep(t1.Add(9 * time.Second))
	if n := open(); n != 0 {
		t.Fatalf("9s into a 10s sustain the sweep opened %d alarms", n)
	}
	restarted, err := storage.NewPG(ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := restarted.SweepEventRules(ctx, t1.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := open(); n != 1 {
		t.Fatalf("held past the sustain, after a restart, the sweep left %d alarms open, want 1", n)
	}

	// A recovery shorter than the clear sustain holds it; a longer one clears.
	t2 := t1.Add(60 * time.Second)
	prop("tcp", "up", t2)
	prop("tcp", "down", t2.Add(4*time.Second))
	sweep(t2.Add(30 * time.Second))
	if n := open(); n != 1 {
		t.Fatalf("a 4s recovery under a 10s clear sustain left %d alarms, want still 1", n)
	}
	t3 := t2.Add(90 * time.Second)
	prop("tcp", "up", t3)
	sweep(t3.Add(11 * time.Second))
	if n := open(); n != 0 {
		t.Fatalf("a recovery held past its clear sustain left %d alarms", n)
	}

	// A later sample past the sustain acts at once, without the sweep.
	t4 := t3.Add(60 * time.Second)
	prop("icmp", "down", t4)
	prop("icmp", "down", t4.Add(12*time.Second))
	if n := open(); n != 1 {
		t.Fatalf("a second down 12s into a 10s sustain left %d alarms, want 1", n)
	}
}
