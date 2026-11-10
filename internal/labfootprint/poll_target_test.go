package labfootprint

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
)

// Each row lands on its node's targets at the interval that node runs it: a
// driver poll at its schedule (never faster than the heartbeat, each tick
// when it declares none), a probe at the node's probe interval.
func TestParsePollTargetsAppliesEachNodesCadence(t *testing.T) {
	out := "edge-a|c1|http|45s\nedge-a|c1|tcp|\nedge-a|c2|snmp|0s\nedge-b|c3|tcp|\nedge-z|c4|tcp|\n"
	cadence := map[string]closedloop.NodeCadence{
		"edge-a": {Heartbeat: 5 * time.Second, ProbeEvery: 30 * time.Second},
		"edge-b": {Heartbeat: 10 * time.Second},
	}
	got, err := parsePollTargets(out, cadence)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]closedloop.PollTarget{
		"edge-a": {{ComponentID: "c1", Transport: "http", Interval: 45 * time.Second}, {ComponentID: "c1", Transport: "tcp", Interval: 30 * time.Second}, {ComponentID: "c2", Transport: "snmp", Interval: 5 * time.Second}},
		"edge-b": {{ComponentID: "c3", Transport: "tcp", Interval: 10 * time.Second}},
		"edge-z": {{ComponentID: "c4", Transport: "tcp", Interval: ParseNodeCadence(nil).Heartbeat}},
	}
	if len(got) != len(want) {
		t.Fatalf("nodes = %v, want %v", got, want)
	}
	for n, ts := range want {
		if len(got[n]) != len(ts) {
			t.Fatalf("%s = %v, want %v", n, got[n], ts)
		}
		for i := range ts {
			if got[n][i] != ts[i] {
				t.Errorf("%s target %d = %+v, want %+v", n, i, got[n][i], ts[i])
			}
		}
	}
	if got, err := parsePollTargets("", nil); err != nil || len(got) != 0 {
		t.Fatalf("empty = %v, %v", got, err)
	}
	for _, bad := range []string{"edge-a|c1|http", "|c1|http|", "edge-a||http|"} {
		if _, err := parsePollTargets(bad, nil); err == nil {
			t.Errorf("parse %q: want an error", bad)
		}
	}
}
