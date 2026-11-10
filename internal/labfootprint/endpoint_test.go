package labfootprint

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
)

// The node's cadence is read off its container's command, and a target's
// interval is the slower of its own and the tick it waits on.
func TestNodeCadenceAndEffectiveInterval(t *testing.T) {
	c := ParseNodeCadence([]string{"node", "run", "--simulate", "--heartbeat", "2s", "--probe-every=10s", "--poll", "x"})
	if c.Heartbeat != 2*time.Second || c.ProbeEvery != 10*time.Second {
		t.Fatalf("cadence = %+v", c)
	}
	if d := ParseNodeCadence([]string{"node", "run"}); d.Heartbeat != 30*time.Second || d.ProbeEvery != 0 {
		t.Fatalf("default cadence = %+v", d)
	}
	for _, x := range []struct {
		every string
		want  time.Duration
	}{{"", 10 * time.Second}, {"1m", time.Minute}, {"500ms", 2 * time.Second}, {"nonsense", 2 * time.Second}} {
		if got := c.Interval(x.every); got != x.want {
			t.Errorf("Interval(%q) = %v, want %v", x.every, got, x.want)
		}
	}
	if got := (NodeCadence{Heartbeat: 2 * time.Second}).Interval(""); got != 2*time.Second {
		t.Errorf("a probe with no probe interval = %v, want the heartbeat", got)
	}
}

// The report prints the closed loop's lines and keeps its JSON names: a
// gauge no pool reports reads "not wired yet", and the faces are named as
// counted since the sim started.
func TestEndpointTable(t *testing.T) {
	q := int64(7)
	r := closedloop.EndpointReport{
		Protocols:  []closedloop.EndpointProtocol{{Protocol: "http", Received: 120, PollTargets: 3, InFlightHigh: map[string]int64{"edge": 4}}},
		Nodes:      []closedloop.EndpointNode{{Node: "core"}, {Node: "edge", ArrivalHigh: 7, DispatchQueueDepth: &q}},
		GapTotals:  []closedloop.GapTotal{{Gap: closedloop.GapSpoolDropped, Loss: 2}},
		WorstFaces: []closedloop.EndpointFace{{Node: "edge"}},
	}
	tbl := ScaleReport{Endpoints: &r}.Table()
	for _, want := range []string{"endpoints", "http received", "120", "3 poll targets", "spool-dropped", "overlap-ratio", "not wired yet", "since the sim started"} {
		if !strings.Contains(tbl, want) {
			t.Errorf("table lacks %q:\n%s", want, tbl)
		}
	}
	raw, err := json.Marshal(ScaleReport{Endpoints: &r})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"endpoints":`, `"node_dispatch_queue_depth":null`, `"node_busy_workers":null`, `"gap":"spool-dropped"`, `"poll_targets":3`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("json lacks %s: %s", want, raw)
		}
	}
}
