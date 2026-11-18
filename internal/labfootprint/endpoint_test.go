package labfootprint

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
)

// The outpost's cadence is read off its container's command.
func TestParseOutpostCadence(t *testing.T) {
	c := ParseOutpostCadence([]string{"outpost", "run", "--simulate", "--heartbeat", "2s", "--probe-every=10s", "--poll", "x"})
	if c.Heartbeat != 2*time.Second || c.ProbeEvery != 10*time.Second {
		t.Fatalf("cadence = %+v", c)
	}
	if d := ParseOutpostCadence([]string{"outpost", "run"}); d.Heartbeat != 30*time.Second || d.ProbeEvery != 0 {
		t.Fatalf("default cadence = %+v", d)
	}
}

// The report prints the closed loop's lines and keeps its JSON names: a
// gauge no pool reports reads "not wired yet", and the faces are named as
// counted since the sim started.
func TestEndpointTable(t *testing.T) {
	q := int64(7)
	r := closedloop.EndpointReport{
		Protocols:  []closedloop.EndpointProtocol{{Protocol: "http", Received: 120, PollTargets: 3, InFlightHigh: map[string]int64{"edge": 4}}},
		Outposts:   []closedloop.EndpointOutpost{{Outpost: "core"}, {Outpost: "edge", ArrivalHigh: 7, DispatchQueueDepth: &q}},
		GapTotals:  []closedloop.GapTotal{{Gap: closedloop.GapSpoolDropped, Loss: 2}},
		WorstFaces: []closedloop.EndpointFace{{Outpost: "edge"}},
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
	for _, want := range []string{`"endpoints":`, `"outpost_dispatch_queue_depth":null`, `"outpost_busy_workers":null`, `"gap":"spool-dropped"`, `"poll_targets":3`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("json lacks %s: %s", want, raw)
		}
	}
}
