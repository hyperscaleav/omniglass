package node

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// laneWatchGetter is an snmp getter that notes, per target, how many gets
// are in flight, so a test can see two tasks on one endpoint overlap, and
// how many run at once across the node.
type laneWatchGetter struct {
	mu       sync.Mutex
	inflight map[string]int
	overlap  []string
	now, max int
	gets     int
}

func (g *laneWatchGetter) Get(_ context.Context, target, _ string, oids []string, _ time.Duration) (map[string]string, error) {
	g.mu.Lock()
	g.inflight[target]++
	if g.inflight[target] > 1 {
		g.overlap = append(g.overlap, target)
	}
	g.now++
	g.max = max(g.max, g.now)
	g.gets++
	g.mu.Unlock()
	time.Sleep(2 * time.Millisecond)
	g.mu.Lock()
	g.inflight[target]--
	g.now--
	g.mu.Unlock()
	out := map[string]string{}
	for _, o := range oids {
		out[o] = "model"
	}
	return out, nil
}

// fleetPoll is a 1s driver poll of one endpoint of one component.
func fleetPoll(component, endpoint, poll int) collection.TaskSpec {
	return collection.TaskSpec{ID: fmt.Sprintf("p-%02d-%d-%d", component, endpoint, poll), Mode: "poll", Transport: "snmp",
		EndpointID: fmt.Sprintf("ep-%02d-%d", component, endpoint), EndpointName: fmt.Sprintf("snmp-%d", endpoint),
		EndpointParams: json.RawMessage(fmt.Sprintf(`{"target":"10.30.%d.%d:161"}`, component, endpoint)),
		Fingerprint:    "fp",
		Spec: json.RawMessage(`{"driver": "edge", "function": "scalars", "schedule": {"every": "1s"},
			"request": {"get": ["1.3.6.1.2.1.1.1.0"]},
			"emits": [{"name": "model-number", "lane": "property", "extract": {"oid": "1.3.6.1.2.1.1.1.0"}}]}`),
		Secrets: map[string]map[string]string{"community": {"community": "lab-public"}}}
}

// Sixty polls across ten components, two endpoints each, run on sixteen
// workers through the real collector, the session manager reconciling the
// worklist while they run. Under -race this is the dispatcher's
// concurrency check; it also holds the lane rule: two tasks on one endpoint
// never overlap, while the node runs many endpoints at once.
func TestSixtyPollsAcrossTenComponentsRunConcurrentlyOneAtATimePerEndpoint(t *testing.T) {
	t.Parallel()
	var tasks []collection.TaskSpec
	for c := range 10 {
		for e := range 2 {
			for p := range 3 {
				tasks = append(tasks, fleetPoll(c, e, p))
			}
		}
	}
	getter := &laneWatchGetter{inflight: map[string]int{}}
	sessions := newSessionManager(nil, sessionHooks{})
	sched := newPollSchedule()
	report := newCollectionReport(time.Minute, gateT0)
	spool := newTelemetrySpool("og.v1.telemetry.edge", "edge", func(string, []byte, string) (ackWait, error) {
		return func(context.Context) error { return nil }, nil
	})
	var mu sync.Mutex
	now := gateT0
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	coll := newCollector(spool, "edge", &collection.Runner{SNMP: getter}, sessions, sched, report)
	coll.clock = clock
	d := newDispatcher(dispatchConfig{workers: 16, heartbeat: 30 * time.Second, clock: clock,
		sched: sched, sessions: sessions, report: report, work: coll.run})
	coll.every, coll.wake = d.interval, d.kick
	// The loop sleeps to each next due and passes there, as the run loop's
	// timer does, for five seconds of fake time.
	end := gateT0.Add(5 * time.Second)
	sessions.reconcile(tasks)
	d.sync(collection.WorklistReply{Tasks: tasks}, gateT0)
	for at := gateT0; !at.After(end); {
		d.pass(t.Context(), at)
		// The loop goes on with its housekeeping while the runs are in
		// flight, as the run loop does.
		sessions.reconcile(tasks)
		d.sync(collection.WorklistReply{Tasks: tasks}, at)
		sessions.flushUnclaimed()
		sessions.reportPushes(report)
		d.settle()
		next, ok := d.next()
		if !ok {
			t.Fatal("nothing booked")
		}
		mu.Lock()
		now = next
		mu.Unlock()
		at = next
	}
	getter.mu.Lock()
	defer getter.mu.Unlock()
	if len(getter.overlap) > 0 {
		t.Fatalf("tasks on one endpoint overlapped at %s", strings.Join(getter.overlap, ", "))
	}
	// A first run at once, then a run on each slot of its grid a half
	// interval on: five or six runs in five seconds, by its phase.
	if lo, hi := len(tasks)*5, len(tasks)*6; getter.gets < lo || getter.gets > hi {
		t.Fatalf("%d gets in five seconds, want each 1s poll five or six times (%d to %d)", getter.gets, lo, hi)
	}
	if getter.max < 2 {
		t.Fatalf("at most %d gets ran at once, want the endpoints run side by side", getter.max)
	}
}
