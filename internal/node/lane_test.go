package node

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The endpoint lane (#1249): one discrete request in flight per endpoint,
// operator work ahead of scheduled work with no preemption, and the gate
// consulted at dispatch, so work queued behind a request that closed the
// gate answers with the gate's reason and sends nothing.

// orderGetter is the snmp fetch, noting the first oid each get asks for, in
// order, and holding the first get until released.
type orderGetter struct {
	mu      sync.Mutex
	order   []string
	hold    chan struct{}
	holding bool
}

func newOrderGetter() *orderGetter { return &orderGetter{hold: make(chan struct{})} }

func (g *orderGetter) Get(ctx context.Context, _, _ string, oids []string, _ time.Duration) (map[string]string, error) {
	g.mu.Lock()
	g.order = append(g.order, oids[0])
	first := !g.holding
	g.holding = true
	g.mu.Unlock()
	if first {
		select {
		case <-g.hold:
		case <-ctx.Done():
		}
	}
	return map[string]string{oids[0]: "Boreal Edge 86"}, nil
}

func (g *orderGetter) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.order...)
}

// oidPoll is a poll of ep-1 asking for one oid, so a get names its task.
func oidPoll(id, endpointID, oid string) collection.TaskSpec {
	return collection.TaskSpec{ID: id, Mode: "poll", Transport: "snmp", EndpointID: endpointID, EndpointName: "snmp",
		EndpointParams: json.RawMessage(`{"target":"10.20.4.40:161"}`),
		Spec: json.RawMessage(`{"driver": "edge", "function": "scalars", "schedule": {"every": "10s"},
			"request": {"get": ["` + oid + `"]},
			"emits": [{"name": "model-number", "lane": "property", "extract": {"oid": "` + oid + `"}}]}`),
		Secrets: map[string]map[string]string{"community": {"community": "lab-public"}}}
}

// oidPreview is a preview asking for one oid at target.
func oidPreview(target, oid string) collection.TaskSpec {
	t := oidPoll("preview-1", "", oid)
	t.EndpointName = "preview"
	t.EndpointParams = json.RawMessage(`{"target":"` + target + `"}`)
	return t
}

// laneRig is a dispatcher on real time whose lanes a test fills by hand.
type laneRig struct {
	l *reasonLoop
	d *dispatcher
}

func newLaneRig(t *testing.T, runner *collection.Runner, tasks ...collection.TaskSpec) *laneRig {
	l := newReasonLoop(t, runner)
	d := l.dispatcher()
	l.sessions.reconcile(tasks)
	t.Cleanup(d.settle)
	return &laneRig{l: l, d: d}
}

func (r *laneRig) route() runRoute {
	return runRoute{out: r.l.spool, node: "edge", runner: r.l.runner, sessions: r.l.sessions,
		sched: r.l.sched, report: r.l.report, pool: r.d.pool}
}

// replies gathers the answers to run requests, from any goroutine.
type replies struct {
	mu  sync.Mutex
	got []collection.RunReply
}

func (r *replies) respond(reply collection.RunReply) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, reply)
}

func (r *replies) all() []collection.RunReply {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]collection.RunReply(nil), r.got...)
}

// blockLane occupies lane key until the returned release is called (by the
// test, or at cleanup).
func blockLane(t *testing.T, pool *lanePool, key string) func() {
	t.Helper()
	gate := make(chan struct{})
	started := make(chan struct{})
	pool.submit(key, laneScheduled, func() {
		close(started)
		<-gate
	})
	<-started
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	return release
}

// A command and a run now that queued on an open endpoint behind a command
// that closes its gate (a reboot) answer with the gate's reason once their
// turn comes, and send nothing: the lane consults the gate at dispatch, not
// only at arrival.
func TestACommandAndARunNowQueuedBehindAGateClosingCommandAnswerWithTheGate(t *testing.T) {
	t.Parallel()
	getter := &countingGetter{reply: map[string]string{"1.3.6.1.2.1.1.1.0": "Boreal Edge 86"}}
	line := &lineRecorder{}
	rig := newLaneRig(t, &collection.Runner{SNMP: getter, Line: line}, reasonProbeTask(), reasonPollTask())
	rig.l.sessions.now = func() time.Time { return gateT0 }
	release := blockLane(t, rig.d.pool, "ep-1")

	// Each answer is keyed by its command: the two delivering goroutines
	// report in whatever order the scheduler runs them, not lane order.
	type outcome struct {
		id         int64
		reply, out string
	}
	outcomes := make(chan outcome, 2)
	deliver := func(d collection.CommandDelivery) {
		reply, out := deliverOnLane(t.Context(), rig.l.runner, d, rig.l.sessions, rig.d.pool)
		outcomes <- outcome{d.ID, reply, out}
	}
	go deliver(reboot(15, 300))
	waitFor(t, func() bool { return rig.d.pool.queued() == 1 }, "the reboot to queue on the lane")
	go deliver(collection.CommandDelivery{ID: 43, CommandType: "set-input", Transport: "tcp", Target: "10.20.4.40:23",
		EndpointID: "ep-1", Line: "SET INPUT hdmi2"})
	waitFor(t, func() bool { return rig.d.pool.queued() == 2 }, "the command to queue behind the reboot")
	var got replies
	takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunNow, Tasks: []collection.TaskSpec{reasonPollTask()}}, got.respond)
	if n := len(got.all()); n != 0 {
		t.Fatalf("a run now on an open endpoint was answered before its turn: %+v", got.all())
	}
	release()

	named := "disrupted by reboot (command 42) since 2026-11-09T14:03:00Z"
	byID := map[int64]outcome{}
	for range 2 {
		o := <-outcomes
		byID[o.id] = o
	}
	first, second := byID[42], byID[43]
	if first.out != "" {
		t.Fatalf("the reboot failed: %s", first.out)
	}
	if want := "node: the endpoint is " + named + ", so the command was not sent"; second.out != want {
		t.Fatalf("the queued command answered %q, want %q", second.out, want)
	}
	if len(line.sent) != 1 || line.sent[0] != "REBOOT" {
		t.Fatalf("sent %v, want the reboot alone", line.sent)
	}
	waitFor(t, func() bool { return len(got.all()) == 1 }, "the queued run now to answer")
	reply := got.all()[0]
	if !reply.Gated || reply.GateReason != string(reasonDisrupted) || !strings.Contains(reply.Error, named) {
		t.Fatalf("the queued run now answered %+v, want gated by the reboot", reply)
	}
	if getter.count() != 0 {
		t.Fatal("the queued run now fetched")
	}
}

// A scheduled poll queued behind a gate-closing command is held when its
// turn comes, not run: counted held, sent nothing.
func TestAPollQueuedBehindADisruptiveCommandIsHeld(t *testing.T) {
	t.Parallel()
	getter := &countingGetter{reply: map[string]string{"1.3.6.1.2.1.1.1.0": "Boreal Edge 86"}}
	rig := newLaneRig(t, &collection.Runner{SNMP: getter, Line: &lineRecorder{}}, reasonPollTask())
	rig.l.sessions.now = func() time.Time { return gateT0 }
	release := blockLane(t, rig.d.pool, "ep-1")
	done := make(chan string, 1)
	go func() {
		_, out := deliverOnLane(t.Context(), rig.l.runner, reboot(15, 300), rig.l.sessions, rig.d.pool)
		done <- out
	}()
	waitFor(t, func() bool { return rig.d.pool.queued() == 1 }, "the reboot to queue")
	rig.d.sync(collection.WorklistReply{Tasks: []collection.TaskSpec{reasonPollTask()}}, gateT0)
	if n := rig.d.pass(t.Context(), gateT0); n != 1 {
		t.Fatalf("the pass dispatched %d, want the poll queued on its lane", n)
	}
	release()
	if out := <-done; out != "" {
		t.Fatalf("the reboot failed: %s", out)
	}
	rig.d.settle()
	if getter.count() != 0 {
		t.Fatal("a poll queued behind a reboot fetched")
	}
	if got := rig.l.metrics(); got["poll-held-count"] != 1 {
		t.Fatalf("poll-held-count = %v, want the poll held once: %v", got["poll-held-count"], got)
	}
}

// overlapMeter counts the requests in flight to one device at once and the
// most it saw.
type overlapMeter struct {
	mu       sync.Mutex
	inflight int
	most     int
	calls    int
}

func (m *overlapMeter) enter() {
	m.mu.Lock()
	m.inflight++
	m.calls++
	m.most = max(m.most, m.inflight)
	m.mu.Unlock()
	time.Sleep(2 * time.Millisecond)
}

func (m *overlapMeter) exit() {
	m.mu.Lock()
	m.inflight--
	m.mu.Unlock()
}

func (m *overlapMeter) read() (most, calls int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.most, m.calls
}

type meteredGetter struct{ m *overlapMeter }

func (g meteredGetter) Get(_ context.Context, _, _ string, oids []string, _ time.Duration) (map[string]string, error) {
	g.m.enter()
	defer g.m.exit()
	return map[string]string{oids[0]: "Boreal Edge 86"}, nil
}

type meteredDialer struct{ m *overlapMeter }

func (d meteredDialer) Dial(context.Context, string, time.Duration) (float64, collection.Reachability, error) {
	d.m.enter()
	defer d.m.exit()
	return 1, collection.Responded, nil
}

type meteredLine struct{ m *overlapMeter }

func (l meteredLine) Exchange(context.Context, string, string, time.Duration) (string, error) {
	l.m.enter()
	defer l.m.exit()
	return "OK", nil
}

// A poll, a connecting probe, a command and a run now on one endpoint never
// talk to it at once, however they arrive.
func TestEveryKindOfWorkOnOneEndpointNeverOverlaps(t *testing.T) {
	t.Parallel()
	meter := &overlapMeter{}
	runner := &collection.Runner{SNMP: meteredGetter{meter}, TCP: meteredDialer{meter}, Line: meteredLine{meter}}
	tasks := []collection.TaskSpec{reasonProbeTask(), reasonPollTask(), oidPoll("t-poll-2", "ep-1", "1.3.6.1.2.1.1.5.0")}
	rig := newLaneRig(t, runner, tasks...)
	var got replies
	var wg sync.WaitGroup
	for round := range 5 {
		at := gateT0.Add(time.Duration(round) * time.Minute)
		rig.l.now = at
		rig.d.sync(collection.WorklistReply{Tasks: tasks}, at)
		rig.d.pass(t.Context(), at)
		wg.Go(func() {
			deliverOnLane(t.Context(), runner, collection.CommandDelivery{ID: int64(100 + round), CommandType: "set-input",
				Transport: "tcp", Target: "10.20.4.40:23", EndpointID: "ep-1", Line: "SET INPUT hdmi2"}, rig.l.sessions, rig.d.pool)
		})
		takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunNow, Tasks: []collection.TaskSpec{reasonPollTask()}}, got.respond)
		wg.Wait()
		rig.d.settle()
	}
	most, calls := meter.read()
	if calls < 15 {
		t.Fatalf("the device saw %d requests, want every kind run each round", calls)
	}
	if most != 1 {
		t.Fatalf("the device saw %d requests at once, want one", most)
	}
	if n := len(got.all()); n != 5 {
		t.Fatalf("%d run nows answered, want 5", n)
	}
}

// A run now arriving while a poll is in flight waits for it (no preemption),
// then runs ahead of the two scheduled polls queued behind it.
func TestARunNowGoesAheadOfQueuedPolls(t *testing.T) {
	t.Parallel()
	getter := newOrderGetter()
	tasks := []collection.TaskSpec{oidPoll("p-0", "ep-1", "1.0"), oidPoll("p-1", "ep-1", "1.1"), oidPoll("p-2", "ep-1", "1.2")}
	rig := newLaneRig(t, &collection.Runner{SNMP: getter}, tasks...)
	rig.d.sync(collection.WorklistReply{Tasks: tasks}, gateT0)
	if n := rig.d.pass(t.Context(), gateT0); n != 3 {
		t.Fatalf("the pass dispatched %d, want the three polls", n)
	}
	waitFor(t, func() bool { return len(getter.seen()) == 1 }, "the first poll to be in flight")
	var got replies
	takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunNow,
		Tasks: []collection.TaskSpec{oidPoll("p-now", "ep-1", "9.9")}}, got.respond)
	if len(got.all()) != 0 || len(getter.seen()) != 1 {
		t.Fatal("the run now interrupted the poll in flight")
	}
	close(getter.hold)
	rig.d.settle()
	if order := strings.Join(getter.seen(), ","); order != "1.0,9.9,1.1,1.2" {
		t.Fatalf("the lane ran %s, want the poll in flight, the run now, then the queued polls", order)
	}
	if r := got.all(); len(r) != 1 || r[0].Error != "" {
		t.Fatalf("the run now answered %+v", r)
	}
}

// A preview whose transport and target match an inventoried endpoint takes
// that endpoint's lane: it waits for the poll in flight and goes ahead of
// the polls queued behind it.
func TestAPreviewTakesItsEndpointsLane(t *testing.T) {
	t.Parallel()
	getter := newOrderGetter()
	tasks := []collection.TaskSpec{oidPoll("p-0", "ep-1", "1.0"), oidPoll("p-1", "ep-1", "1.1")}
	rig := newLaneRig(t, &collection.Runner{SNMP: getter}, tasks...)
	rig.d.sync(collection.WorklistReply{Tasks: tasks}, gateT0)
	rig.d.pass(t.Context(), gateT0)
	waitFor(t, func() bool { return len(getter.seen()) == 1 }, "the first poll to be in flight")
	var got replies
	takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunPreview,
		Tasks: []collection.TaskSpec{oidPreview("10.20.4.40:161", "9.9")}}, got.respond)
	time.Sleep(20 * time.Millisecond)
	if len(getter.seen()) != 1 {
		t.Fatal("the preview talked to the endpoint beside the poll in flight")
	}
	close(getter.hold)
	rig.d.settle()
	if order := strings.Join(getter.seen(), ","); order != "1.0,9.9,1.1" {
		t.Fatalf("the lane ran %s, want the poll in flight, the preview, then the queued poll", order)
	}
	if r := got.all(); len(r) != 1 || r[0].Error != "" || len(r[0].Results) != 1 {
		t.Fatalf("the preview answered %+v", r)
	}
}

// A preview resolving to an endpoint whose gate is closed answers at once
// with the gate's reason, a refusal included, and sends nothing, even with
// the endpoint's lane busy.
func TestAResolvedPreviewOnAClosedGateAnswersAtOnce(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		close func(m *sessionManager)
		kind  reasonKind
		named string
	}{
		{"refused", func(m *sessionManager) {
			m.gates.record("ep-1", "", &collection.TerminalError{Class: driver.ClassAuth, Reason: "authorizationError"})
		}, reasonRefused, "authorizationError"},
		{"disrupted", func(m *sessionManager) {
			m.disrupt(reboot(15, 300))
		}, reasonDisrupted, "disrupted by reboot (command 42)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			getter := &countingGetter{reply: map[string]string{"9.9": "x"}}
			rig := newLaneRig(t, &collection.Runner{SNMP: getter}, reasonProbeTask(), reasonPollTask())
			rig.l.sessions.now = func() time.Time { return gateT0 }
			blockLane(t, rig.d.pool, "ep-1")
			tc.close(rig.l.sessions)
			var got replies
			takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunPreview,
				Tasks: []collection.TaskSpec{oidPreview("10.20.4.40:161", "9.9")}}, got.respond)
			r := got.all()
			if len(r) != 1 {
				t.Fatalf("the preview was not answered at once: %+v", r)
			}
			if !r[0].Gated || r[0].GateReason != string(tc.kind) || !strings.Contains(r[0].Error, tc.named) ||
				!strings.Contains(r[0].Error, "10.20.4.40:161") {
				t.Fatalf("the preview answered %+v, want gated %s naming %q and its target", r[0], tc.kind, tc.named)
			}
			if getter.count() != 0 {
				t.Fatal("a gated preview fetched")
			}
		})
	}
}

// A preview matching no endpoint runs off-lane: a busy lane on its host
// does not hold it.
func TestAnUnmatchedPreviewRunsOffLane(t *testing.T) {
	t.Parallel()
	getter := &countingGetter{reply: map[string]string{"9.9": "x"}}
	rig := newLaneRig(t, &collection.Runner{SNMP: getter}, reasonProbeTask(), reasonPollTask())
	blockLane(t, rig.d.pool, "ep-1")
	var got replies
	takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunPreview,
		Tasks: []collection.TaskSpec{oidPreview("10.20.4.41:161", "9.9")}}, got.respond)
	waitFor(t, func() bool { return len(got.all()) == 1 }, "the unmatched preview to answer")
	if r := got.all()[0]; r.Error != "" || getter.count() != 1 {
		t.Fatalf("the unmatched preview answered %+v after %d gets", r, getter.count())
	}
}

// A preview on another transport to the host of an endpoint held unreachable
// answers at once with the host's reason.
func TestAnotherTransportToAnUnreachableHostAnswersAtOnce(t *testing.T) {
	t.Parallel()
	line := &lineRecorder{}
	rig := newLaneRig(t, &collection.Runner{Line: line}, reasonProbeTask(), reasonPollTask())
	rig.l.sessions.gates.verdict("ep-1", "t-probe", false, gateT0)
	rig.l.sessions.gates.verdict("ep-1", "t-probe", false, gateT0.Add(time.Second))
	preview := oidPreview("10.20.4.40:23", "9.9")
	preview.Transport = "tcp"
	var got replies
	takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunPreview, Tasks: []collection.TaskSpec{preview}}, got.respond)
	waitFor(t, func() bool { return len(got.all()) == 1 }, "the preview to answer")
	r := got.all()[0]
	if !r.Gated || r.GateReason != string(reasonUnreachable) || !strings.Contains(r.Error, "the host is unreachable since") {
		t.Fatalf("the preview answered %+v, want the host's unreachable reason", r)
	}
	if len(line.sent) != 0 {
		t.Fatalf("the preview sent %v", line.sent)
	}
}

// Three endpoints of one host whose probes ping informationally send one
// ping a probe slot between them, however many of them probe in it.
func TestThreeEndpointsOnOneHostPingOncePerSlot(t *testing.T) {
	t.Parallel()
	dialer := &targetDialer{targets: map[string]*scriptDialer{
		"10.20.4.40:23":  {script: []dialAnswer{dialUp}},
		"10.20.4.40:80":  {script: []dialAnswer{dialUp}},
		"10.20.4.40:161": {script: []dialAnswer{dialUp}},
	}}
	pinger := &countingPinger{}
	l := newReasonLoop(t, &collection.Runner{TCP: dialer, Ping: pinger})
	l.probeEvery = time.Second
	tasks := []collection.TaskSpec{
		disruptedProbe("t-1", "ep-1", "10.20.4.40:23"),
		disruptedProbe("t-2", "ep-2", "10.20.4.40:80"),
		disruptedProbe("t-3", "ep-3", "10.20.4.40:161"),
	}
	const ticks = 4
	for range ticks {
		l.tick(tasks...)
	}
	// A probe's first due is phased within its interval, so a probe may
	// sit out the first tick; from then on each runs once a tick.
	if dials := dialer.dials(); dials < 3*(ticks-1) {
		t.Fatalf("the probes dialed %d times in %d ticks, want each probe once a tick after its first due", dials, ticks)
	}
	if n := pinger.count(); n != ticks {
		t.Fatalf("the host was pinged %d times in %d ticks (%d probes), want once a tick", n, ticks, dialer.dials())
	}
}

// The operator queue is bounded: an offer past its cap is refused and
// counted, never queued; the window reports offers against refusals.
func TestTheOperatorQueueRefusesPastItsCapAndCounts(t *testing.T) {
	t.Parallel()
	p := newLanePool(1)
	release := blockLane(t, p, "ep-1")
	for i := range operatorQueueCap {
		if !p.offer("ep-1", laneOperator, func() {}) {
			t.Fatalf("offer %d refused under the cap", i)
		}
	}
	if p.offer("ep-1", laneOperator, func() {}) {
		t.Fatal("an offer past the cap was queued")
	}
	if !p.offer("ep-2", laneScheduled, func() {}) {
		t.Fatal("scheduled work was refused by the operator cap")
	}
	release()
	p.wait()
	w := p.takeWindow()
	if w.offers != operatorQueueCap+1 || w.refused != 1 {
		t.Fatalf("window offers %d refused %d, want %d and 1", w.offers, w.refused, operatorQueueCap+1)
	}
	if again := p.takeWindow(); again.offers != 0 || again.refused != 0 {
		t.Fatalf("the next window carried %+v", again)
	}
}

// A run now refused by a full operator queue answers at once naming why.
func TestARunNowPastTheOperatorCapAnswersFull(t *testing.T) {
	t.Parallel()
	rig := newLaneRig(t, &collection.Runner{SNMP: &countingGetter{}}, reasonPollTask())
	blockLane(t, rig.d.pool, "ep-1")
	for range operatorQueueCap {
		rig.d.pool.offer("ep-1", laneOperator, func() {})
	}
	var got replies
	takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunNow, Tasks: []collection.TaskSpec{reasonPollTask()}}, got.respond)
	if r := got.all(); len(r) != 1 || !strings.Contains(r[0].Error, "operator lanes are full") {
		t.Fatalf("the run now answered %+v, want refused as full", r)
	}
}

// A listen task never takes a lane: it holds its endpoint's session, so a
// closed gate leaves nothing of it queued.
func TestAListenTaskNeverTakesALane(t *testing.T) {
	t.Parallel()
	listen := reasonPollTask()
	listen.ID, listen.Mode = "t-listen", "listen"
	rig := newLaneRig(t, &collection.Runner{SNMP: &countingGetter{}}, listen)
	rig.l.sessions.gates.verdict("ep-1", "t-probe", false, gateT0)
	rig.l.sessions.gates.verdict("ep-1", "t-probe", false, gateT0.Add(time.Second))
	rig.d.sync(collection.WorklistReply{Tasks: []collection.TaskSpec{listen}}, gateT0)
	if n := rig.d.pass(t.Context(), gateT0); n != 0 {
		t.Fatalf("a listen task took a lane (%d dispatched)", n)
	}
}

// The tick races nothing: lanes on many endpoints run while a run now and a
// preview arrive (run under -race).
func TestTheTickRacesNothingWithLanesARunNowAndAPreview(t *testing.T) {
	t.Parallel()
	meter := &overlapMeter{}
	runner := &collection.Runner{SNMP: meteredGetter{meter}, TCP: meteredDialer{meter}}
	var tasks []collection.TaskSpec
	for _, ep := range []string{"ep-1", "ep-2", "ep-3", "ep-4"} {
		tasks = append(tasks, oidPoll("p-"+ep, ep, "1.0"))
	}
	rig := newLaneRig(t, runner, tasks...)
	var got replies
	var wg sync.WaitGroup
	for round := range 4 {
		at := gateT0.Add(time.Duration(round) * time.Minute)
		wg.Go(func() {
			rig.d.sync(collection.WorklistReply{Tasks: tasks}, at)
			rig.d.pass(t.Context(), at)
		})
		wg.Go(func() {
			takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunNow, Tasks: []collection.TaskSpec{tasks[0]}}, got.respond)
		})
		wg.Go(func() {
			takeRun(t.Context(), rig.route(), collection.RunRequest{Kind: collection.RunPreview,
				Tasks: []collection.TaskSpec{oidPreview("10.20.4.40:161", "9.9")}}, got.respond)
		})
		wg.Wait()
		rig.d.settle()
	}
	waitFor(t, func() bool { return len(got.all()) == 8 }, "every run now and preview to answer")
	for _, r := range got.all() {
		if r.Error != "" {
			t.Fatalf("a run answered %+v", r)
		}
	}
}
