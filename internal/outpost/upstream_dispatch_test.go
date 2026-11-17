package node

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// The dispatcher under a path dependency (#1652): while the upstream's gate
// is closed, the dependent's polls and probes are held, counted, and nothing
// is sent; a command answers with the upstream at once. When the upstream
// opens, the dependent's probe runs and its own up verdict releases its
// polls.

// gateway is the upstream: its own probe. Reached through nothing, it
// carries no component on the wire; the node reads comp-gw off the edge that
// names it.
func gatewayProbe() collection.TaskSpec {
	return collection.TaskSpec{ID: "t-gw", Mode: "poll", Transport: "tcp", EndpointID: "gw-1", EndpointName: "tcp",
		EndpointParams: json.RawMessage(`{"target":"10.20.4.1:23"}`)}
}

// behindGateway marks a task of ep-1 as reached through the gateway.
func behindGateway(t collection.TaskSpec) collection.TaskSpec {
	t.ComponentID = "comp-dev"
	t.PathUpstreams = []collection.PathUpstream{{ComponentID: "comp-gw", ComponentName: "gateway", EndpointIDs: []string{"gw-1"}}}
	return t
}

func TestADependentsPollsAndProbesAreHeldWhileItsUpstreamIsClosed(t *testing.T) {
	t.Parallel()
	gw := &scriptDialer{script: []dialAnswer{dialUp}}
	dev := &scriptDialer{script: []dialAnswer{dialUp}}
	dialer := &targetDialer{targets: map[string]*scriptDialer{"10.20.4.1:23": gw, "10.20.4.40:161": dev}}
	getter := &countingGetter{reply: map[string]string{"1.3.6.1.2.1.1.1.0": "Boreal Edge 86"}}
	l := newReasonLoop(t, &collection.Runner{TCP: dialer, SNMP: getter})
	tasks := []collection.TaskSpec{gatewayProbe(), behindGateway(reasonProbeTask()), behindGateway(reasonPollTask())}
	l.tick(tasks...)
	if n := getter.count(); n != 1 {
		t.Fatalf("the dependent's poll ran %d times with its upstream up, want 1", n)
	}

	gw.then(dialDown)
	l.tick(tasks...)
	l.tick(tasks...)
	r, closed := l.sessions.gates.closed("ep-1")
	if !closed || r.kind != reasonUpstream || r.source != "comp-gw" {
		t.Fatalf("ep-1 = %+v, %v; want held by upstream comp-gw", r, closed)
	}
	devDials, gets := dev.dials, getter.count()
	l.metrics()
	for range 12 {
		l.tick(tasks...)
	}
	if dev.dials != devDials {
		t.Fatalf("the held dependent's probe dialed %d times", dev.dials-devDials)
	}
	if n := getter.count(); n != gets {
		t.Fatalf("the held dependent's poll fetched %d times", n-gets)
	}
	got := l.metrics()
	// Each 10 s poll due and each 1 s probe due counts once held.
	if got[probekeys.PollHeldCount] < 12 {
		t.Fatalf("poll-held-count = %v, want the held polls and probes counted: %v", got[probekeys.PollHeldCount], got)
	}
	if _, failed := got[probekeys.PollFailureCount]; failed {
		t.Fatalf("a held dependent counted a failure: %v", got)
	}
	if _, faulted := l.coll.faultseen["t-poll"]; faulted {
		t.Fatal("a held poll published a collection-failed")
	}
	if _, ok := l.sessions.gates.others["ep-1"][reasonUnreachable]; ok {
		t.Fatal("the held dependent took a verdict of its own")
	}

	line := &lineRecorder{}
	_, out := executeDelivery(t.Context(), &collection.Runner{Line: line}, collection.CommandDelivery{
		ID: 7, CommandType: "set-input", Transport: "tcp", Target: "10.20.4.40:23", EndpointID: "ep-1", Line: "SET INPUT hdmi2"}, l.sessions)
	if !strings.HasPrefix(out, "node: the endpoint is held by upstream gateway since ") || !strings.HasSuffix(out, ", so the command was not sent") {
		t.Fatalf("a command answered %q; want it held by upstream gateway since, unsent", out)
	}
	if len(line.sent) != 0 {
		t.Fatalf("a command to a held dependent sent %v", line.sent)
	}

	// The gateway reads up: the dependent's probe runs, reads it up, and its
	// polls run.
	gw.then(dialUp)
	l.tick(tasks...)
	l.tick(tasks...)
	if r, closed := l.sessions.gates.closed("ep-1"); closed {
		t.Fatalf("ep-1 = %+v; want released by its own up verdict once the gateway opened", r)
	}
	if dev.dials == devDials {
		t.Fatal("the dependent's probe did not run once its upstream opened")
	}
	if n := getter.count(); n == gets {
		t.Fatal("the dependent's poll did not run once it was released")
	}
}
