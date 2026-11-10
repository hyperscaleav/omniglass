package devseed_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

func properties(names ...string) func(string) bool {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return func(n string) bool { return set[n] }
}

// The induced spec is the official one plus one poll that asks the first
// property poll's question again and answers it under the key no catalog
// holds: the official polls are untouched, and the new one emits nothing else.
func TestInduceSpecAddsOnePollAnsweringTheKey(t *testing.T) {
	t.Parallel()
	status := driver.Poll{
		Name: "status", Schedule: driver.Schedule{Every: "5m"},
		Request: driver.Request{Path: "/status"},
		Emits: []driver.Emit{
			{Name: "uptime", Extract: driver.Extract{JSONPath: "$.uptime"}},
			{Name: "firmware-version", Extract: driver.Extract{JSONPath: "$.fw"}, Transform: &driver.Transform{Cast: "text"}},
		},
	}
	listing := driver.Poll{
		Name: "inputs", Schedule: driver.Schedule{Every: "1m"},
		Request: driver.Request{Path: "/inputs"}, Records: "$.inputs[*]",
		Emits: []driver.Emit{{Name: "input-label", Extract: driver.Extract{JSONPath: "$.label"}}},
	}
	sp := driver.Spec{Version: 1, Transport: "http", Polls: []driver.Poll{listing, status}}

	got, from, ok := devseed.InduceSpec(sp, "lab-induced-unregistered", properties("firmware-version", "input-label"))
	if !ok {
		t.Fatal("a spec with a property poll was not induced")
	}
	if from != "firmware-version" {
		t.Errorf("copied %q, want firmware-version (the records poll is skipped)", from)
	}
	if len(sp.Polls) != 2 || len(got.Polls) != 3 {
		t.Fatalf("polls: official %d, induced %d; want 2 and 3", len(sp.Polls), len(got.Polls))
	}
	p := got.Polls[2]
	if p.Name != devseed.InducedPoll || p.Request.Path != "/status" || p.Schedule.Every != devseed.InducedEvery {
		t.Errorf("induced poll = %+v, want %s asking /status every %s", p, devseed.InducedPoll, devseed.InducedEvery)
	}
	if len(p.Emits) != 1 || p.Emits[0].Name != "lab-induced-unregistered" || p.Emits[0].Extract.JSONPath != "$.fw" ||
		p.Emits[0].Transform == nil || p.Emits[0].Transform.Cast != "text" {
		t.Errorf("induced emits = %+v, want the firmware read under the key", p.Emits)
	}
	if got.Polls[1].Emits[1].Name != "firmware-version" {
		t.Errorf("the official poll changed: %+v", got.Polls[1])
	}
}

// A spec with no plain property poll cannot be induced: nothing is invented.
func TestInduceSpecRefusesASpecWithNoPropertyPoll(t *testing.T) {
	t.Parallel()
	sp := driver.Spec{Version: 1, Transport: "http", Polls: []driver.Poll{{
		Name: "load", Schedule: driver.Schedule{Every: "30s"}, Request: driver.Request{Path: "/load"},
		Emits: []driver.Emit{{Name: "cpu-utilization", Extract: driver.Extract{JSONPath: "$.cpu"}}},
	}}}
	if _, _, ok := devseed.InduceSpec(sp, "lab-induced-unregistered", properties("firmware-version")); ok {
		t.Fatal("a metric-only spec was induced")
	}
}
