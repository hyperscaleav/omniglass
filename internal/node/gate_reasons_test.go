package node

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/collection/rejection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The gate is a set of reasons (#1650). Today's terminal refusal is the
// refused reason, unchanged; the endpoint's own probe adds unreachable on its
// second down verdict in a row and removes it on the next up. The gate is
// open only when the set is empty. Admission (a poll, a command, a run now, a
// preview) asks whether the gate is closed; the ladder asks refusedAt, which
// sees the refused reason alone.

var gateT0 = time.Date(2026, 11, 9, 14, 3, 0, 0, time.UTC)

func TestTheGateStaysClosedUntilEveryReasonIsRemoved(t *testing.T) {
	t.Parallel()
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.record("ep-1", "fp-a", &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"})
	g.verdict("ep-1", "probe-1", false, gateT0)
	g.verdict("ep-1", "probe-1", false, gateT0.Add(time.Second))

	r, closed := g.closed("ep-1")
	if !closed || r.kind != reasonRefused {
		t.Fatalf("closed = %+v, %v; want refused, the more causal of the two", r, closed)
	}
	if _, ok := g.refusedAt("ep-1", rungAuthenticated); !ok {
		t.Fatal("refusedAt lost the refusal beside unreachable")
	}
	if r, ok := g.holding("ep-1"); !ok || r.kind != reasonUnreachable {
		t.Fatalf("holding = %+v, %v; want unreachable", r, ok)
	}

	// The rung passing removes refused alone.
	g.clear("ep-1")
	if r, closed := g.closed("ep-1"); !closed || r.kind != reasonUnreachable {
		t.Fatalf("after the refusal cleared: closed = %+v, %v; want unreachable still", r, closed)
	}
	if _, ok := g.refusedAt("ep-1", rungAuthenticated); ok {
		t.Fatal("refusedAt read unreachable as a refusal")
	}
	// The up verdict removes unreachable, and the gate opens.
	g.verdict("ep-1", "probe-1", true, gateT0.Add(2*time.Second))
	if r, closed := g.closed("ep-1"); closed {
		t.Fatalf("an empty set left the gate closed: %+v", r)
	}
}

func TestAnUpVerdictLeavesTheRefusal(t *testing.T) {
	t.Parallel()
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.record("ep-1", "fp-a", &collection.TerminalError{Class: driver.ClassHostKey, Reason: "the host key changed"})
	g.verdict("ep-1", "probe-1", false, gateT0)
	g.verdict("ep-1", "probe-1", false, gateT0.Add(time.Second))
	g.verdict("ep-1", "probe-1", true, gateT0.Add(2*time.Second))
	if r, closed := g.closed("ep-1"); !closed || r.kind != reasonRefused {
		t.Fatalf("closed = %+v, %v; want the refusal still", r, closed)
	}
	if _, held := g.holding("ep-1"); held {
		t.Fatal("an up verdict left unreachable holding")
	}
}

// The reason an answer names is the most causal: by rank, then the oldest.
func TestTheMostCausalReasonWinsAndTiesGoToTheOldest(t *testing.T) {
	t.Parallel()
	older := reason{kind: reasonUnreachable, since: gateT0, source: "probe-a"}
	newer := reason{kind: reasonUnreachable, since: gateT0.Add(time.Minute), source: "probe-b"}
	refused := reason{kind: reasonRefused, since: gateT0.Add(time.Hour)}
	if got := mostCausal([]reason{newer, older}); got.source != "probe-a" {
		t.Fatalf("a tie picked %+v, want the oldest", got)
	}
	if got := mostCausal([]reason{older, refused}); got.kind != reasonRefused {
		t.Fatalf("picked %+v, want refused, which ranks above unreachable however new", got)
	}
	// The table orders every kind, the ones later slices add among them.
	if !(reasonRules[reasonRefused].rank < reasonRules[reasonUnreachable].rank) {
		t.Fatal("refused does not rank above unreachable")
	}
}

func TestAnUnreachableEndpointRefusesNoRung(t *testing.T) {
	t.Parallel()
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.verdict("ep-1", "probe-1", false, gateT0)
	g.verdict("ep-1", "probe-1", false, gateT0.Add(time.Second))
	for _, r := range []rung{rungReachable, rungResponsive, rungAuthenticated} {
		if _, ok := g.refusedAt("ep-1", r); ok {
			t.Fatalf("unreachable refused the %s rung", r)
		}
	}
	r, closed := g.closed("ep-1")
	if !closed || r.kind != reasonUnreachable || r.source != "probe-1" || !r.since.Equal(gateT0) {
		t.Fatalf("closed = %+v, %v; want unreachable since the first down, from the probe", r, closed)
	}
	if want := "unreachable since 2026-11-09T14:03:00Z (probe probe-1)"; r.text != want {
		t.Fatalf("text = %q, want %q", r.text, want)
	}
}

// The streak: one down does not close, the second in a row does with since
// the first down, a third keeps that since, and the next up removes it.
func TestTwoConsecutiveDownVerdictsCloseTheGate(t *testing.T) {
	t.Parallel()
	g := newGates()
	if g.verdict("ep-1", "probe-1", false, gateT0) {
		t.Fatal("one down verdict changed the gate")
	}
	if _, closed := g.closed("ep-1"); closed {
		t.Fatal("one down verdict closed the gate")
	}
	if !g.verdict("ep-1", "probe-1", false, gateT0.Add(10*time.Second)) {
		t.Fatal("the second down verdict did not change the gate")
	}
	if g.verdict("ep-1", "probe-1", false, gateT0.Add(20*time.Second)) {
		t.Fatal("a third down verdict changed the gate again")
	}
	if r, _ := g.closed("ep-1"); !r.since.Equal(gateT0) {
		t.Fatalf("since = %v, want the first down %v", r.since, gateT0)
	}
	if !g.verdict("ep-1", "probe-1", true, gateT0.Add(30*time.Second)) {
		t.Fatal("the up verdict did not change the gate")
	}
	if _, closed := g.closed("ep-1"); closed {
		t.Fatal("the up verdict left the gate closed")
	}
	// The up reset the streak: one down after it does not close.
	g.verdict("ep-1", "probe-1", false, gateT0.Add(40*time.Second))
	if _, closed := g.closed("ep-1"); closed {
		t.Fatal("a down after an up closed the gate on its own")
	}
}

// scriptDialer is a tcp probe primitive answering from a script, one entry a
// dial; the last entry repeats.
type scriptDialer struct {
	mu     sync.Mutex
	script []dialAnswer
	dials  int
}

type dialAnswer struct {
	reach collection.Reachability
	err   error
}

var (
	dialUp           = dialAnswer{reach: collection.Responded}
	dialDown         = dialAnswer{reach: collection.Refused}
	dialInconclusive = dialAnswer{err: errors.New("no route out of the node")}
)

func (d *scriptDialer) Dial(context.Context, string, time.Duration) (float64, collection.Reachability, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	a := d.script[min(d.dials, len(d.script)-1)]
	d.dials++
	if a.err != nil {
		return 0, "", a.err
	}
	return 1, a.reach, nil
}

func (d *scriptDialer) then(a ...dialAnswer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// The last answer repeats past the script's end, so the dials made so
	// far keep their answers and the next dial reads a.
	for len(d.script) < d.dials {
		d.script = append(d.script, d.script[len(d.script)-1])
	}
	d.script = append(d.script[:d.dials:d.dials], a...)
}

// countingGetter is the snmp fetch, counting its gets.
type countingGetter struct {
	mu    sync.Mutex
	gets  int
	err   error
	reply map[string]string
}

func (g *countingGetter) Get(context.Context, string, string, []string, time.Duration) (map[string]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gets++
	return g.reply, g.err
}

func (g *countingGetter) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.gets
}

func reasonProbeTask() collection.TaskSpec {
	return collection.TaskSpec{ID: "t-probe", Mode: "poll", Transport: "tcp", EndpointID: "ep-1", EndpointName: "snmp",
		EndpointParams: json.RawMessage(`{"target":"10.20.4.40:161"}`)}
}

func reasonPollTask() collection.TaskSpec {
	return collection.TaskSpec{ID: "t-poll", Mode: "poll", Transport: "snmp", EndpointID: "ep-1", EndpointName: "snmp",
		EndpointParams: json.RawMessage(`{"target":"10.20.4.40:161"}`),
		Spec: json.RawMessage(`{"driver": "edge", "function": "scalars", "schedule": {"every": "10s"},
			"request": {"get": ["1.3.6.1.2.1.1.1.0"]},
			"emits": [{"name": "model-number", "lane": "property", "extract": {"oid": "1.3.6.1.2.1.1.1.0"}}]}`),
		Secrets: map[string]map[string]string{"community": {"community": "lab-public"}}}
}

// reasonLoop drives the dispatcher a second a tick on a fake clock, the way
// the run loop does: a pass at each tick, every run it started settled, and
// a pass again whenever a run asked for one (a verdict moved the gate).
type reasonLoop struct {
	t        *testing.T
	runner   *collection.Runner
	sessions *sessionManager
	sched    *pollSchedule
	report   *collectionReport
	spool    *telemetrySpool
	coll     *collector
	d        *dispatcher
	now      time.Time
	// probeEvery is the probe interval the dispatcher is made with: half a
	// tick unless a test sets it before its first tick.
	probeEvery time.Duration
}

func newReasonLoop(t *testing.T, runner *collection.Runner) *reasonLoop {
	return &reasonLoop{t: t, runner: runner,
		sessions: newSessionManager(nil, sessionHooks{}), sched: newPollSchedule(),
		report: newCollectionReport(time.Minute, gateT0),
		spool: newTelemetrySpool("og.v1.telemetry.edge", "edge", func(string, []byte, string) (ackWait, error) {
			return func(context.Context) error { return nil }, nil
		}),
		now: gateT0, probeEvery: 500 * time.Millisecond}
}

// dispatcher is made on the first tick, so a test may swap the sessions or
// the probe interval before it. A probe runs every half second unless set,
// so each tick runs it once.
func (l *reasonLoop) dispatcher() *dispatcher {
	if l.d == nil {
		clock := func() time.Time { return l.now }
		l.coll = newCollector(l.spool, "edge", l.runner, l.sessions, l.sched, l.report)
		l.coll.clock = clock
		l.d = newDispatcher(dispatchConfig{workers: 4, heartbeat: time.Second, probeEvery: l.probeEvery,
			clock: clock, sched: l.sched, sessions: l.sessions, report: l.report, work: l.coll.run})
		l.coll.every, l.coll.wake = l.d.interval, l.d.kick
	}
	return l.d
}

func (l *reasonLoop) tick(tasks ...collection.TaskSpec) {
	l.t.Helper()
	d := l.dispatcher()
	l.sessions.reconcile(tasks)
	d.sync(collection.WorklistReply{Tasks: tasks}, l.now)
	d.pass(l.t.Context(), l.now)
	d.settle()
	for {
		select {
		case <-d.wake:
			d.pass(l.t.Context(), l.now)
			d.settle()
			continue
		default:
		}
		break
	}
	l.now = l.now.Add(time.Second)
}

// metrics is the report window's rows for the endpoint, by key.
func (l *reasonLoop) metrics() map[string]float64 {
	out := map[string]float64{}
	for _, rep := range l.report.take(l.now, map[string]string{"ep-1": "t-poll"}) {
		for _, s := range rep.samples {
			out[s.Name] = s.Value
		}
	}
	return out
}

// An inconclusive probe (the dial could not be made, so no verdict) neither
// adds to the streak nor resets it, through the run loop's own probe branch.
func TestAnInconclusiveProbeNeitherAddsToNorResetsTheStreak(t *testing.T) {
	t.Parallel()
	dialer := &scriptDialer{script: []dialAnswer{dialDown, dialInconclusive, dialDown, dialInconclusive, dialInconclusive}}
	l := newReasonLoop(t, &collection.Runner{TCP: dialer})
	l.tick(reasonProbeTask())
	l.tick(reasonProbeTask())
	if _, closed := l.sessions.gates.closed("ep-1"); closed {
		t.Fatal("one down and an inconclusive probe closed the gate")
	}
	l.tick(reasonProbeTask())
	r, closed := l.sessions.gates.closed("ep-1")
	if !closed || !r.since.Equal(gateT0) {
		t.Fatalf("closed = %+v, %v; want unreachable since the first down across the inconclusive one", r, closed)
	}
	l.tick(reasonProbeTask())
	l.tick(reasonProbeTask())
	if _, closed := l.sessions.gates.closed("ep-1"); !closed {
		t.Fatal("an inconclusive probe reopened the gate")
	}
}

// A due poll on an unreachable endpoint sends nothing, books nothing on its
// schedule, counts as held, not failed, and runs on the first tick after the
// probe's up verdict.
func TestADuePollOnAnUnreachableEndpointIsHeld(t *testing.T) {
	t.Parallel()
	dialer := &scriptDialer{script: []dialAnswer{dialDown}}
	getter := &countingGetter{reply: map[string]string{"1.3.6.1.2.1.1.1.0": "Boreal Edge 86"}}
	l := newReasonLoop(t, &collection.Runner{TCP: dialer, SNMP: getter})
	l.tick(reasonProbeTask())
	l.tick(reasonProbeTask())
	if _, closed := l.sessions.gates.closed("ep-1"); !closed {
		t.Fatal("two down verdicts did not close the gate")
	}
	for i := 0; i < 4; i++ {
		l.tick(reasonProbeTask(), reasonPollTask())
	}
	if n := getter.count(); n != 0 {
		t.Fatalf("a held poll fetched %d times", n)
	}
	every := 10 * time.Second
	if at, _ := l.sched.nextOf("t-poll"); !at.Equal(slotAfter(at.Add(-every), phaseOf("t-poll", every), every)) {
		t.Fatalf("a held poll booked %v, want its next slot, never a retry", at)
	}
	if _, backoff := l.sched.backoff["t-poll"]; backoff {
		t.Fatal("a held poll booked a backoff")
	}
	if _, faulted := l.coll.faultseen["t-poll"]; faulted {
		t.Fatal("a held poll published a collection-failed")
	}
	got := l.metrics()
	if got[probekeys.PollHeldCount] != 1 {
		t.Fatalf("poll-held-count = %v, want 1 (held once in its interval): %v", got[probekeys.PollHeldCount], got)
	}
	if _, failed := got[probekeys.PollFailureCount]; failed {
		t.Fatalf("a held poll counted as a failure: %v", got)
	}

	dialer.then(dialUp)
	l.tick(reasonProbeTask(), reasonPollTask())
	if _, closed := l.sessions.gates.closed("ep-1"); closed {
		t.Fatal("the up verdict did not open the gate")
	}
	if n := getter.count(); n != 1 {
		t.Fatalf("the poll ran %d times on the tick its gate opened, want 1", n)
	}
	if _, booked := l.sched.nextOf("t-poll"); !booked {
		t.Fatal("the resumed poll booked no next run")
	}
}

// A held poll counts once in each of its intervals, however many ticks it
// waits through, so a 10s poll held for 25s counts three times.
func TestAHeldPollCountsOncePerInterval(t *testing.T) {
	t.Parallel()
	dialer := &scriptDialer{script: []dialAnswer{dialDown}}
	getter := &countingGetter{}
	l := newReasonLoop(t, &collection.Runner{TCP: dialer, SNMP: getter})
	l.tick(reasonProbeTask())
	l.tick(reasonProbeTask())
	for i := 0; i < 25; i++ {
		l.tick(reasonProbeTask(), reasonPollTask())
	}
	if got := l.metrics()[probekeys.PollHeldCount]; got != 3 {
		t.Fatalf("poll-held-count = %v over 25s of a 10s poll, want 3", got)
	}
}

// An endpoint with no probe task is never unreachable: its poll attempts,
// fails and backs off exactly as before (schedule.go's failed).
func TestAPollOnAnEndpointWithNoProbeFailsAndBacksOff(t *testing.T) {
	t.Parallel()
	getter := &countingGetter{err: context.DeadlineExceeded}
	l := newReasonLoop(t, &collection.Runner{SNMP: getter})
	start := l.now
	l.tick(reasonPollTask())
	l.tick(reasonPollTask())
	l.tick(reasonPollTask())
	if n := getter.count(); n != 2 {
		t.Fatalf("a failing poll with no probe fetched %d times over 3s, want 2 (the second after its 1s backoff, the third waiting 2s)", n)
	}
	if b := l.sched.backoff["t-poll"]; b != 2*time.Second {
		t.Fatalf("backoff = %v, want 2s after two failures", b)
	}
	if at, _ := l.sched.nextOf("t-poll"); !at.Equal(start.Add(3 * time.Second)) {
		t.Fatalf("next run at %v, want %v", at, start.Add(3*time.Second))
	}
	got := l.metrics()
	if got[probekeys.PollFailureCount] != 2 {
		t.Fatalf("poll-failure-count = %v, want 2: %v", got[probekeys.PollFailureCount], got)
	}
	if _, held := got[probekeys.PollHeldCount]; held {
		t.Fatalf("a poll with no probe counted as held: %v", got)
	}
}

// Unreachable never strips the ssh probe's credential: that is the refused
// reason's, at the authenticated rung.
func TestAnUnreachableEndpointKeepsItsSSHProbeCredential(t *testing.T) {
	t.Parallel()
	prober := &recordingSSHProber{}
	runner := &collection.Runner{SSH: prober}
	m := newSessionManager(nil, sessionHooks{})
	task := collection.TaskSpec{ID: "r1", Mode: "poll", Transport: "ssh", EndpointID: "ep-1", EndpointName: "ssh",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.5:22","username":"probe","password":"right"}`),
		Spec:           json.RawMessage(`{}`), Fingerprint: "fp-1"}
	m.reconcile([]collection.TaskSpec{task})
	m.gates.verdict("ep-1", "r1", false, gateT0)
	m.gates.verdict("ep-1", "r1", false, gateT0.Add(time.Second))
	if _, err := collectProbe(t.Context(), runner, task, m); err != nil {
		t.Fatal(err)
	}
	prober.mu.Lock()
	defer prober.mu.Unlock()
	if len(prober.creds) != 1 || prober.creds[0].Password != "right" {
		t.Fatalf("probe credentials = %+v, want the credential kept", prober.creds)
	}
}

// A held session is never gated by unreachable: it starts and comes up.
func TestAnUnreachableEndpointStartsItsHeldSessionUnrejected(t *testing.T) {
	t.Parallel()
	dialer := &authFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()
	m.gates.verdict("ep-1", "t-probe", false, gateT0)
	m.gates.verdict("ep-1", "t-probe", false, gateT0.Add(time.Second))
	// The probe task rides the worklist beside the session's: a reason whose
	// probe left the worklist is forgotten.
	m.reconcile([]collection.TaskSpec{reasonProbeTask(), pollTaskWithLogin("p1", "ep-1", "newtron", 0)})
	log.waitFor(t, "p1", collection.SessionUp)
	if _, closed := m.gates.closed("ep-1"); !closed {
		t.Fatal("the session start lifted unreachable; only the probe's up verdict does")
	}
}

// A datagram listener re-arms when its rung recovers, while unreachable is
// still on the gate: the re-arm runs inside the probe, before the gate hears
// its up verdict.
func TestAnUnreachableUDPEndpointReArmsOnRecovery(t *testing.T) {
	t.Parallel()
	peer := newComponent(t, nvpAnswers)
	m := newDatagramManager(t, newCapture())
	p := udpParams(peer.addr())
	probe, listen := udpProbeTask("r1", "ep-1", p), udpListenTask("l1", "ep-1", p)
	m.reconcile([]collection.TaskSpec{probe, listen})
	eventually(t, "the arm at open", func() bool { return peer.count("SUBSCRIBE INPUT") == 1 })
	runner := &collection.Runner{}
	peer.set(nvpAnswers, func(int32) bool { return true })
	for i := 0; i < 2; i++ {
		if _, err := collectProbe(t.Context(), runner, probe, m); err != nil {
			t.Fatal(err)
		}
		m.gates.verdict("ep-1", "r1", false, gateT0.Add(time.Duration(i)*time.Second))
	}
	if _, closed := m.gates.closed("ep-1"); !closed {
		t.Fatal("two down verdicts did not close the gate")
	}
	peer.set(nvpAnswers, nil)
	if _, err := collectProbe(t.Context(), runner, probe, m); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the re-arm on recovery under unreachable", func() bool { return peer.count("SUBSCRIBE INPUT") >= 2 })
}

// A command to an unreachable endpoint answers at once with the reason and
// dials nothing; a refusal still answers with today's text.
func TestACommandToAnUnreachableEndpointAnswersAtOnce(t *testing.T) {
	t.Parallel()
	m := newSessionManager(nil, sessionHooks{})
	m.reconcile([]collection.TaskSpec{{ID: "r1", Transport: "tcp", EndpointID: "ep-1", EndpointName: "tcp",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.5:23"}`), Spec: json.RawMessage(`{}`), Fingerprint: "fp-1"}})
	m.gates.verdict("ep-1", "r1", false, gateT0)
	m.gates.verdict("ep-1", "r1", false, gateT0.Add(time.Second))
	line := &lineRecorder{}
	_, out := executeDelivery(t.Context(), &collection.Runner{Line: line}, collection.CommandDelivery{
		ID: 1, Transport: "tcp", Target: "10.0.0.5:23", EndpointID: "ep-1", Line: "SET INPUT hdmi2"}, m)
	if want := "node: the endpoint is unreachable since 2026-11-09T14:03:00Z (probe r1), so the command was not sent"; out != want {
		t.Fatalf("outcome %q, want %q", out, want)
	}
	if len(line.sent) != 0 {
		t.Fatalf("a command to an unreachable endpoint sent %v", line.sent)
	}
}

// A run now on an unreachable endpoint is answered Gated with the reason's
// kind, and nothing is sent.
func TestARunNowOnAnUnreachableEndpointAnswersWithTheReason(t *testing.T) {
	t.Parallel()
	getter := &countingGetter{}
	l := newReasonLoop(t, &collection.Runner{SNMP: getter})
	l.sessions.reconcile([]collection.TaskSpec{reasonPollTask()})
	l.sessions.gates.verdict("ep-1", "t-probe", false, gateT0)
	l.sessions.gates.verdict("ep-1", "t-probe", false, gateT0.Add(time.Second))
	reply := runNowTask(t.Context(), l.spool, "edge", l.runner, l.sessions, l.sched, l.report, reasonPollTask(), time.Now().Add(time.Minute))
	if !reply.Gated || reply.GateReason != string(reasonUnreachable) || !strings.Contains(reply.Error, "unreachable since 2026-11-09T14:03:00Z") {
		t.Fatalf("reply = %+v, want gated by unreachable", reply)
	}
	if getter.count() != 0 {
		t.Fatal("a run now on an unreachable endpoint fetched")
	}
}

// A run now on an unreachable endpoint is answered as it arrives, not queued
// behind the run loop: a tick running long (a probe timing out against the
// gone device) would otherwise outlast the caller's budget, and the caller
// would read a timeout where the gate had its reason ready. A run now on an
// open endpoint is queued as before.
func TestARunNowOnAnUnreachableEndpointIsAnsweredBeforeTheRunLoop(t *testing.T) {
	t.Parallel()
	m := newSessionManager(nil, sessionHooks{})
	m.reconcile([]collection.TaskSpec{reasonPollTask()})
	jobs := make(chan runJob, 1)
	var replies []collection.RunReply
	respond := func(r collection.RunReply) { replies = append(replies, r) }
	req := collection.RunRequest{Kind: collection.RunNow, Tasks: []collection.TaskSpec{reasonPollTask()}}

	takeRun(t.Context(), nil, m.gates, jobs, req, respond)
	if len(replies) != 0 || len(jobs) != 1 {
		t.Fatalf("an open endpoint's run now: replies %+v, queued %d; want queued, unanswered", replies, len(jobs))
	}
	<-jobs

	m.gates.verdict("ep-1", "t-probe", false, gateT0)
	m.gates.verdict("ep-1", "t-probe", false, gateT0.Add(time.Second))
	takeRun(t.Context(), nil, m.gates, jobs, req, respond)
	if len(jobs) != 0 {
		t.Fatal("a run now on an unreachable endpoint was queued to the run loop")
	}
	if len(replies) != 1 || !replies[0].Gated || replies[0].GateReason != string(reasonUnreachable) ||
		!strings.Contains(replies[0].Error, "unreachable since 2026-11-09T14:03:00Z") {
		t.Fatalf("replies = %+v, want one gated by unreachable", replies)
	}
}

// A preview carries no endpoint: it answers Gated when its target's host is
// unreachable on this node, and runs when only a refusal holds (a preview
// connects with its own inputs).
func TestAPreviewToAnUnreachableHostAnswersWithTheReason(t *testing.T) {
	t.Parallel()
	getter := &countingGetter{reply: map[string]string{"1.3.6.1.2.1.1.1.0": "Boreal Edge 86"}}
	runner := &collection.Runner{SNMP: getter}
	m := newSessionManager(nil, sessionHooks{})
	m.reconcile([]collection.TaskSpec{reasonPollTask()})
	m.gates.record("ep-1", "", &collection.TerminalError{Class: driver.ClassAuth, Reason: "authorizationError"})
	preview := snmpPreviewTask()
	if reply := previewRun(t.Context(), runner, m.gates, []collection.TaskSpec{preview}, nil, 5*time.Second); reply.Gated {
		t.Fatalf("a refusal gated a preview: %+v", reply)
	}
	m.gates.verdict("ep-1", "t-probe", false, gateT0)
	m.gates.verdict("ep-1", "t-probe", false, gateT0.Add(time.Second))
	before := getter.count()
	reply := previewRun(t.Context(), runner, m.gates, []collection.TaskSpec{preview}, nil, 5*time.Second)
	if !reply.Gated || reply.GateReason != string(reasonUnreachable) || !strings.Contains(reply.Error, "unreachable since") {
		t.Fatalf("reply = %+v, want gated by unreachable", reply)
	}
	if getter.count() != before {
		t.Fatal("a preview to an unreachable host fetched")
	}
	other := snmpPreviewTask()
	other.EndpointParams = json.RawMessage(`{"target":"10.20.4.41:161"}`)
	if reply := previewRun(t.Context(), runner, m.gates, []collection.TaskSpec{other}, nil, 5*time.Second); reply.Gated {
		t.Fatalf("another host's preview was gated: %+v", reply)
	}
}

func TestHostOfReadsEveryTargetForm(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"10.0.0.5:23":                "10.0.0.5",
		"10.0.0.5":                   "10.0.0.5",
		"https://codec.lab:8443/api": "codec.lab",
		"http://10.0.0.5/status":     "10.0.0.5",
		"[fe80::1]:161":              "fe80::1",
		"Codec.Lab":                  "codec.lab",
		"":                           "",
	} {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// endpoint-rejection carries every reason, the refused entry first; Parse
// still reads the refused entry; a restarted node adopts refused alone, since
// the probe re-derives unreachable.
func TestTheGatePublishesEveryReasonAndARestartAdoptsTheRefusal(t *testing.T) {
	t.Parallel()
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.record("ep-1", "fp-a", &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"})
	g.verdict("ep-1", "probe-1", false, gateT0)
	g.verdict("ep-1", "probe-1", false, gateT0.Add(time.Second))
	value, reason := g.rejectionOf("ep-1")
	if want := "authenticated auth fp-a;unreachable 2026-11-09T14:03:00Z probe-1"; value != want {
		t.Fatalf("published %q, want %q", value, want)
	}
	if reason != "auth: auth rejected" {
		t.Fatalf("reason = %q, want the refusal's own", reason)
	}
	if gt, ok := rejection.Parse(value); !ok || gt.Rung != "authenticated" || gt.Fingerprint != "fp-a" {
		t.Fatalf("Parse(%q) = %+v, %v; want the refused entry", value, gt, ok)
	}

	restarted := newGates()
	restarted.observe("ep-1", "fp-a", value)
	if _, ok := restarted.refusedAt("ep-1", rungAuthenticated); !ok {
		t.Fatal("a restarted node lost the refusal")
	}
	if r, held := restarted.holding("ep-1"); held {
		t.Fatalf("a restarted node adopted %+v; unreachable is the probe's to re-derive", r)
	}
	if got, _ := restarted.rejectionOf("ep-1"); got != "authenticated auth fp-a" {
		t.Fatalf("a restarted node publishes %q, want the refusal alone", got)
	}

	// Unreachable alone publishes no reason beside it: the reason property is
	// a refusal's words.
	alone := newGates()
	alone.verdict("ep-1", "probe-1", false, gateT0)
	alone.verdict("ep-1", "probe-1", false, gateT0.Add(time.Second))
	if v, why := alone.rejectionOf("ep-1"); v != "unreachable 2026-11-09T14:03:00Z probe-1" || why != "" {
		t.Fatalf("unreachable alone published %q with reason %q", v, why)
	}
}

// The run loop publishes the gate when the probe's verdict moves it.
func TestTheProbePublishesTheGateWhenItCloses(t *testing.T) {
	t.Parallel()
	dialer := &scriptDialer{script: []dialAnswer{dialDown}}
	rej := &rejectionLog{}
	l := newReasonLoop(t, &collection.Runner{TCP: dialer})
	l.sessions = newSessionManager(nil, sessionHooks{rejection: rej.record})
	l.tick(reasonProbeTask())
	if got := rej.all(); len(got) != 0 {
		t.Fatalf("one down verdict published %v", got)
	}
	l.tick(reasonProbeTask())
	got := rej.all()
	if len(got) != 1 || !strings.HasPrefix(got[0], "unreachable "+gateT0.Format(time.RFC3339)+" t-probe") {
		t.Fatalf("published %v, want unreachable since the first down", got)
	}
	dialer.then(dialUp)
	l.tick(reasonProbeTask())
	if got := rej.all(); got[len(got)-1] != rejectionNone {
		t.Fatalf("published %v, want none once the probe read up", got)
	}
}

// A refused community holds snmp's get, and the rung's held reading (down,
// rejected) is the refusal's echo, not the agent's answer: it never adds
// unreachable, so once the refusal lifts nothing else holds the endpoint.
func TestARefusedSNMPEndpointNeverReadsUnreachable(t *testing.T) {
	t.Parallel()
	fake := &snmpFake{err: &collection.TerminalError{Class: driver.ClassAuth, Reason: "the agent answered authorizationError for the community"}}
	l := newReasonLoop(t, &collection.Runner{SNMP: fake})
	for range 4 {
		l.tick(snmpProbeTask("fp-1"))
	}
	if _, ok := l.sessions.refusedAt("ep-s", rungAuthenticated); !ok {
		t.Fatal("the authorizationError did not refuse the endpoint")
	}
	if r, held := l.sessions.holding("ep-s"); held {
		t.Fatalf("the refusal's own held reading added %+v", r)
	}
	if value, _ := l.sessions.gates.rejectionOf("ep-s"); strings.Contains(value, string(reasonUnreachable)) {
		t.Fatalf("endpoint-rejection = %q, want the refusal alone", value)
	}
}

// The same for a udp endpoint whose component answered a terminal reply: the
// reply gates the rung, and neither that probe's down reading nor the held
// ones after it count toward unreachable.
func TestARefusedUDPEndpointNeverReadsUnreachable(t *testing.T) {
	t.Parallel()
	peer := newComponent(t, func(string) []string { return []string{"-ERR locked"} })
	l := newReasonLoop(t, &collection.Runner{})
	t.Cleanup(l.sessions.closeAll)
	task := udpProbeTask("r1", "ep-1", udpParams(peer.addr()))
	task.Session = &driver.SessionSpec{Terminal: []driver.TerminalRule{{Class: driver.ClassAuth, Match: driver.Match{Prefix: "-ERR locked"}}}}
	for range 4 {
		l.tick(task)
	}
	if _, ok := l.sessions.refusedAt("ep-1", rungResponsive); !ok {
		t.Fatal("the terminal reply did not refuse the endpoint")
	}
	if r, held := l.sessions.holding("ep-1"); held {
		t.Fatalf("the refusal's own down readings added %+v", r)
	}
}

// probeVerdict drops only a refusal's echo: a responsive rung that reads down,
// rejected, with no probe result beside it. A port that did not open is the
// endpoint's own down, and so is a rung that timed out.
func TestProbeVerdictDropsOnlyARefusalsEcho(t *testing.T) {
	t.Parallel()
	responsive := func(verdict, why string) collection.Sample {
		return collection.Sample{Name: collection.SignalEndpointResponsive, Text: verdict, IsText: true, TS: gateT0,
			Labels: map[string]string{collection.ReasonLabel: why}}
	}
	closedPort := collection.ProbeResult(collection.SignalTCPOpen, false, gateT0, collection.Refused)
	for _, tc := range []struct {
		name   string
		dps    []collection.Sample
		up, ok bool
	}{
		{"a held rung", []collection.Sample{responsive(collection.VerdictDown, rejectedReason+driver.ClassAuth)}, false, false},
		{"a silent rung", []collection.Sample{responsive(collection.VerdictDown, string(collection.Timedout))}, false, true},
		{"an answered rung", []collection.Sample{responsive(collection.VerdictUp, string(collection.Responded))}, true, true},
		{"a closed port beside a held rung", []collection.Sample{closedPort, responsive(collection.VerdictDown, rejectedReason+driver.ClassAuth)}, false, true},
		{"nothing", nil, false, false},
	} {
		if up, ok := probeVerdict(tc.dps); up != tc.up || ok != tc.ok {
			t.Errorf("%s: probeVerdict = %v, %v; want %v, %v", tc.name, up, ok, tc.up, tc.ok)
		}
	}
}
