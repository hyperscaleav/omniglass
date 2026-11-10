package node

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A disruptive command closes its endpoints' gates (#1651): the node adds
// disrupted to every endpoint of the component placed on it before the
// command is sent, runs nothing against them for the quiet period, then
// reopens each on its own first up verdict after it, with the hold the
// counted backstop.

// targetDialer answers each probe target from its own script.
type targetDialer struct {
	mu      sync.Mutex
	targets map[string]*scriptDialer
}

func (d *targetDialer) Dial(ctx context.Context, target string, timeout time.Duration) (float64, collection.Reachability, error) {
	d.mu.Lock()
	s := d.targets[target]
	d.mu.Unlock()
	return s.Dial(ctx, target, timeout)
}

func (d *targetDialer) dials() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, s := range d.targets {
		s.mu.Lock()
		n += s.dials
		s.mu.Unlock()
	}
	return n
}

// countingPinger answers every informational ping, counting them.
type countingPinger struct {
	mu    sync.Mutex
	pings int
}

func (p *countingPinger) Ping(context.Context, string, int, time.Duration) (collection.PingResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pings++
	return collection.PingResult{Received: 1, AvgRTT: time.Millisecond, Reason: collection.Responded}, nil
}

func (p *countingPinger) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pings
}

// disruptedProbe is a tcp probe of an endpoint that pings informationally.
func disruptedProbe(id, endpointID, target string) collection.TaskSpec {
	return collection.TaskSpec{ID: id, Mode: "poll", Transport: "tcp", EndpointID: endpointID, EndpointName: "tcp",
		EndpointParams: json.RawMessage(`{"target":"` + target + `"}`), Ladder: &driver.EffectiveLadder{Ping: true, Transport: "tcp"}}
}

// reboot is the reboot of the component holding ep-1 and ep-2.
func reboot(quiet, hold int) collection.CommandDelivery {
	return collection.CommandDelivery{ID: 42, CommandType: "reboot", Transport: "tcp", Target: "10.20.4.40:23",
		EndpointID: "ep-1", Line: "REBOOT", Disruptive: true, QuietSeconds: quiet, HoldSeconds: hold,
		HoldEndpoints: []string{"ep-1", "ep-2"}}
}

// gateCheckingLine sends a line, noting which endpoints' gates held as it
// went out.
type gateCheckingLine struct {
	sessions *sessionManager
	sent     []string
	held     map[string]reason
}

func (l *gateCheckingLine) Exchange(_ context.Context, _, line string, _ time.Duration) (string, error) {
	l.sent = append(l.sent, line)
	l.held = map[string]reason{}
	for _, ep := range []string{"ep-1", "ep-2", "ep-3"} {
		if r, ok := l.sessions.holding(ep); ok {
			l.held[ep] = r
		}
	}
	return "OK", nil
}

func TestADisruptiveDeliveryClosesItsHoldSetBeforeItSends(t *testing.T) {
	t.Parallel()
	rej := &rejectionLog{}
	m := newSessionManager(nil, sessionHooks{rejection: rej.record})
	m.now = func() time.Time { return gateT0 }
	m.reconcile([]collection.TaskSpec{
		disruptedProbe("t-1", "ep-1", "10.20.4.40:23"),
		disruptedProbe("t-2", "ep-2", "10.20.4.40:80"),
		disruptedProbe("t-3", "ep-3", "10.20.4.50:23"),
	})
	line := &gateCheckingLine{sessions: m}
	if _, out := executeDelivery(t.Context(), &collection.Runner{Line: line}, reboot(15, 300), m); out != "" {
		t.Fatalf("the reboot failed: %s", out)
	}
	if len(line.sent) != 1 || line.sent[0] != "REBOOT" {
		t.Fatalf("sent %v, want the reboot once", line.sent)
	}
	want := "disrupted by reboot (command 42) since 2026-11-09T14:03:00Z"
	for _, ep := range []string{"ep-1", "ep-2"} {
		r, ok := line.held[ep]
		if !ok || r.kind != reasonDisrupted || r.text != want || !r.since.Equal(gateT0) {
			t.Fatalf("%s as the reboot went out: %+v, %v; want %q", ep, r, ok, want)
		}
	}
	if r, ok := line.held["ep-3"]; ok {
		t.Fatalf("an endpoint outside the hold set was held: %+v", r)
	}
	var published []string
	for _, v := range rej.all() {
		if strings.HasPrefix(v, "disrupted ") {
			published = append(published, v)
		}
	}
	if len(published) != 2 || published[0] != "disrupted 2026-11-09T14:03:00Z command-42" {
		t.Fatalf("published %v, want disrupted for each held endpoint, naming the command", rej.all())
	}
}

// A non-disruptive command closes nothing.
func TestANonDisruptiveCommandClosesNoGate(t *testing.T) {
	t.Parallel()
	m := newSessionManager(nil, sessionHooks{})
	m.reconcile([]collection.TaskSpec{disruptedProbe("t-1", "ep-1", "10.20.4.40:23")})
	d := reboot(0, 0)
	d.Disruptive, d.HoldEndpoints, d.CommandType, d.Line = false, nil, "set-input", "SET INPUT hdmi2"
	if _, out := executeDelivery(t.Context(), &collection.Runner{Line: &lineRecorder{}}, d, m); out != "" {
		t.Fatalf("the command failed: %s", out)
	}
	if r, held := m.holding("ep-1"); held {
		t.Fatalf("a non-disruptive command held the endpoint: %+v", r)
	}
}

// Through the run loop on a fake clock: nothing probes or pings a held
// endpoint during the quiet period; after it each endpoint is released by its
// own first up verdict while the other stays held; the one with no up is
// released at the hold backstop, which is counted for it alone.
func TestADisruptedEndpointIsQuietThenReleasedByItsOwnUpOrTheBackstop(t *testing.T) {
	t.Parallel()
	one, two := &scriptDialer{script: []dialAnswer{dialUp}}, &scriptDialer{script: []dialAnswer{dialUp}}
	dialer := &targetDialer{targets: map[string]*scriptDialer{"10.20.4.40:23": one, "10.20.4.41:80": two}}
	pinger := &countingPinger{}
	l := newReasonLoop(t, &collection.Runner{TCP: dialer, Ping: pinger, Line: &lineRecorder{}})
	tasks := []collection.TaskSpec{disruptedProbe("t-1", "ep-1", "10.20.4.40:23"), disruptedProbe("t-2", "ep-2", "10.20.4.41:80")}
	l.tick(tasks...)
	l.tick(tasks...)
	if dialer.dials() == 0 || pinger.count() == 0 {
		t.Fatal("the probes never ran before the reboot")
	}

	since := l.now
	if _, out := executeDelivery(t.Context(), l.runner, reboot(15, 60), l.sessions); out != "" {
		t.Fatalf("the reboot failed: %s", out)
	}
	dials, pings := dialer.dials(), pinger.count()
	for l.now.Before(since.Add(15 * time.Second)) {
		l.tick(tasks...)
	}
	if n := dialer.dials() - dials; n != 0 {
		t.Fatalf("%d probe dials ran during the quiet period", n)
	}
	if n := pinger.count() - pings; n != 0 {
		t.Fatalf("%d pings ran during the quiet period", n)
	}
	for _, ep := range []string{"ep-1", "ep-2"} {
		if r, held := l.sessions.holding(ep); !held || r.kind != reasonDisrupted {
			t.Fatalf("%s at the end of the quiet period: %+v, %v; want disrupted", ep, r, held)
		}
	}

	// The quiet period is over: ep-1 reads up, ep-2's probe cannot tell.
	two.then(dialInconclusive)
	l.tick(tasks...)
	if dialer.dials() == dials {
		t.Fatal("no probe ran once the quiet period ended")
	}
	if r, held := l.sessions.holding("ep-1"); held {
		t.Fatalf("ep-1's up verdict left it held: %+v", r)
	}
	if r, held := l.sessions.holding("ep-2"); !held || r.kind != reasonDisrupted {
		t.Fatalf("ep-1's up released ep-2: %+v, %v", r, held)
	}

	for l.now.Before(since.Add(60 * time.Second)) {
		l.tick(tasks...)
	}
	if _, held := l.sessions.holding("ep-2"); !held {
		t.Fatal("ep-2 was released before its hold backstop")
	}
	l.tick(tasks...)
	if r, held := l.sessions.holding("ep-2"); held {
		t.Fatalf("ep-2 is held past its hold backstop: %+v", r)
	}
	got := map[string]map[string]float64{}
	for _, rep := range l.report.take(l.now, map[string]string{"ep-1": "t-1", "ep-2": "t-2"}) {
		got[rep.task] = map[string]float64{}
		for _, s := range rep.samples {
			got[rep.task][s.Name] = s.Value
		}
	}
	if got["t-2"][probekeys.HoldExpiryCount] != 1 {
		t.Fatalf("hold-expiry-count for ep-2 = %v, want 1: %v", got["t-2"][probekeys.HoldExpiryCount], got)
	}
	if _, expired := got["t-1"][probekeys.HoldExpiryCount]; expired {
		t.Fatalf("ep-1, released by its up, counted a hold expiry: %v", got)
	}
}

// An up verdict from a probe that began before the quiet period ended read
// the device before it went down: it cannot release the reason.
func TestAStaleUpCannotReleaseADisruption(t *testing.T) {
	t.Parallel()
	g := newGates()
	g.disrupt([]string{"ep-1"}, "reboot", 42, gateT0, 15*time.Second, 300*time.Second)
	for _, at := range []time.Time{gateT0.Add(-time.Second), gateT0.Add(5 * time.Second), gateT0.Add(15*time.Second - time.Millisecond)} {
		if g.verdict("ep-1", "t-1", true, at) {
			t.Fatalf("an up from a probe begun at %v moved the gate", at)
		}
		if r, held := g.holding("ep-1"); !held || r.kind != reasonDisrupted {
			t.Fatalf("an up from a probe begun at %v released the disruption: %+v", at, r)
		}
	}
	if !g.verdict("ep-1", "t-1", true, gateT0.Add(15*time.Second)) {
		t.Fatal("the first up after the quiet period did not move the gate")
	}
	if r, held := g.holding("ep-1"); held {
		t.Fatalf("the first up after the quiet period left %+v", r)
	}
}

// The race through the probe itself: a probe that is in flight as the reboot
// goes out and answers up after the quiet period ends is judged by when it
// began, so it releases nothing.
func TestAProbeInFlightAcrossTheDeliveryCannotReleaseIt(t *testing.T) {
	t.Parallel()
	l := newReasonLoop(t, nil)
	task := disruptedProbe("t-1", "ep-1", "10.20.4.40:23")
	l.runner = &collection.Runner{TCP: dialFunc(func() collection.Reachability {
		// The reboot goes out while the probe waits on its connect, and the
		// connect comes back after the quiet period.
		l.sessions.disrupt(reboot(15, 300))
		l.now = l.now.Add(20 * time.Second)
		return collection.Responded
	})}
	l.dispatcher()
	l.sessions.reconcile([]collection.TaskSpec{task})
	if err := l.coll.runProbe(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if r, held := l.sessions.holding("ep-1"); !held || r.kind != reasonDisrupted {
		t.Fatalf("an up from a probe begun before the reboot released it: %+v, %v", r, held)
	}
}

// dialFunc is a tcp dial answered by a function.
type dialFunc func() collection.Reachability

func (f dialFunc) Dial(context.Context, string, time.Duration) (float64, collection.Reachability, error) {
	return 1, f(), nil
}

// During the hold a due poll is held, never failed, and a command, a run now
// and a preview answer at once, naming the command and since when.
func TestADisruptedEndpointHoldsItsPollsAndAnswersAtOnce(t *testing.T) {
	t.Parallel()
	getter := &countingGetter{reply: map[string]string{"1.3.6.1.2.1.1.1.0": "Boreal Edge 86"}}
	l := newReasonLoop(t, &collection.Runner{SNMP: getter})
	l.dispatcher()
	l.sessions.reconcile([]collection.TaskSpec{reasonPollTask()})
	d := reboot(15, 300)
	d.HoldEndpoints = []string{"ep-1"}
	l.sessions.disrupt(d)
	for range 12 {
		l.tick(reasonPollTask())
	}
	if n := getter.count(); n != 0 {
		t.Fatalf("a held poll fetched %d times", n)
	}
	got := l.metrics()
	if got[probekeys.PollHeldCount] < 1 {
		t.Fatalf("poll-held-count = %v, want the held dues counted: %v", got[probekeys.PollHeldCount], got)
	}
	if _, failed := got[probekeys.PollFailureCount]; failed {
		t.Fatalf("a held poll counted as a failure: %v", got)
	}
	if _, faulted := l.coll.faultseen["t-poll"]; faulted {
		t.Fatal("a held poll published a collection-failed")
	}

	named := "disrupted by reboot (command 42) since 2026-11-09T14:03:00Z"
	line := &lineRecorder{}
	_, out := executeDelivery(t.Context(), &collection.Runner{Line: line}, collection.CommandDelivery{
		ID: 43, CommandType: "set-input", Transport: "tcp", Target: "10.20.4.40:23", EndpointID: "ep-1", Line: "SET INPUT hdmi2"}, l.sessions)
	if want := "node: the endpoint is " + named + ", so the command was not sent"; out != want {
		t.Fatalf("a command answered %q, want %q", out, want)
	}
	if len(line.sent) != 0 {
		t.Fatalf("a command to a disrupted endpoint sent %v", line.sent)
	}

	reply := runNowTask(t.Context(), l.spool, "edge", l.runner, l.sessions, l.sched, l.report, reasonPollTask(), time.Now().Add(time.Minute))
	if !reply.Gated || reply.GateReason != string(reasonDisrupted) || !strings.Contains(reply.Error, named) {
		t.Fatalf("run now = %+v, want gated by disrupted, naming the command", reply)
	}
	var answered []collection.RunReply
	takeRun(t.Context(), runRoute{node: "edge", sessions: l.sessions, pool: newLanePool(1)},
		collection.RunRequest{Kind: collection.RunNow, Tasks: []collection.TaskSpec{reasonPollTask()}},
		func(r collection.RunReply) { answered = append(answered, r) })
	if len(answered) != 1 || answered[0].GateReason != string(reasonDisrupted) {
		t.Fatalf("a run now answered %+v; want answered at once by disrupted", answered)
	}
	preview := previewRun(t.Context(), l.runner, l.sessions.gates, []collection.TaskSpec{snmpPreviewTask()}, nil, 5*time.Second)
	if !preview.Gated || preview.GateReason != string(reasonDisrupted) || !strings.Contains(preview.Error, named) {
		t.Fatalf("preview = %+v, want gated by disrupted, naming the command", preview)
	}
	if n := getter.count(); n != 0 {
		t.Fatalf("a run now or a preview fetched %d times", n)
	}
}

// A held session dials nothing during the quiet period, on the session's own
// clock: it connects once the period ends.
func TestADisruptedEndpointsSessionDialsAfterItsQuietPeriod(t *testing.T) {
	t.Parallel()
	dialer := &authFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()
	quiet := 400 * time.Millisecond
	m.gates.disrupt([]string{"ep-1"}, "reboot", 42, time.Now(), quiet, time.Minute)
	m.reconcile([]collection.TaskSpec{pollTaskWithLogin("p1", "ep-1", "newtron", 0)})
	time.Sleep(quiet / 2)
	if n := dialer.connectCount(); n != 0 {
		t.Fatalf("the session dialed %d times during the quiet period", n)
	}
	log.waitFor(t, "p1", collection.SessionUp)
	if n := dialer.connectCount(); n != 1 {
		t.Fatalf("the session dialed %d times, want once after the quiet period", n)
	}
}

// A restarted node does not adopt disrupted from the published value: the
// command that caused it is the old process's, and its probes re-derive
// whatever still holds.
func TestARestartedNodeDoesNotAdoptADisruption(t *testing.T) {
	t.Parallel()
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.record("ep-1", "fp-a", &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"})
	g.disrupt([]string{"ep-1"}, "reboot", 42, gateT0, 15*time.Second, 300*time.Second)
	value, _ := g.rejectionOf("ep-1")
	if want := "authenticated auth fp-a;disrupted 2026-11-09T14:03:00Z command-42"; value != want {
		t.Fatalf("published %q, want %q", value, want)
	}
	restarted := newGates()
	restarted.observe("ep-1", "fp-a", value)
	if r, held := restarted.holding("ep-1"); held {
		t.Fatalf("a restarted node adopted %+v", r)
	}
	if _, quiet := restarted.quietUntil("ep-1", gateT0); quiet {
		t.Fatal("a restarted node adopted a quiet period")
	}
	if _, ok := restarted.refusedAt("ep-1", rungAuthenticated); !ok {
		t.Fatal("a restarted node lost the refusal beside the disruption")
	}
}

// An endpoint that leaves the worklist forgets its disruption.
func TestADisruptionLeavesWithItsEndpoint(t *testing.T) {
	t.Parallel()
	g := newGates()
	g.disrupt([]string{"ep-1"}, "reboot", 42, gateT0, 15*time.Second, 300*time.Second)
	g.retain(map[string]bool{"ep-2": true}, map[string]bool{})
	if _, held := g.holding("ep-1"); held {
		t.Fatal("a disruption outlived its endpoint")
	}
	if _, pending := g.nextExpiry(); pending {
		t.Fatal("a gone endpoint's backstop is still pending")
	}
}

// The probe streak is neither fed nor reset by a verdict read during quiet,
// and the delivery starts it over (#1651): a down read as the device dropped
// (or one read before the reboot went out) would otherwise let a single
// down after quiet add unreachable, dated before the reboot.
func TestADownReadBeforeQuietEndsDoesNotCountTowardUnreachable(t *testing.T) {
	t.Parallel()
	for name, before := range map[string]time.Time{
		"before the delivery": gateT0.Add(-time.Second),
		"during quiet":        gateT0.Add(5 * time.Second),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := newGates()
			if before.Before(gateT0) {
				g.verdict("ep-1", "t-1", false, before)
			}
			g.disrupt([]string{"ep-1"}, "reboot", 42, gateT0, 15*time.Second, 300*time.Second)
			if !before.Before(gateT0) {
				if g.verdict("ep-1", "t-1", false, before) {
					t.Fatal("a down read during quiet moved the gate")
				}
			}
			g.verdict("ep-1", "t-1", false, gateT0.Add(20*time.Second))
			if _, ok := g.others["ep-1"][reasonUnreachable]; ok {
				t.Fatalf("one down after quiet added unreachable: %+v", g.others["ep-1"])
			}
			g.verdict("ep-1", "t-1", false, gateT0.Add(30*time.Second))
			r, ok := g.others["ep-1"][reasonUnreachable]
			if !ok {
				t.Fatal("two downs after quiet did not add unreachable")
			}
			if want := gateT0.Add(20 * time.Second); !r.since.Equal(want) {
				t.Fatalf("unreachable since %v, want the first down after quiet %v", r.since, want)
			}
		})
	}
}

// The backstop names the command whose disruption it ended, for the
// self-log line the spec asks for.
func TestTheHoldBackstopNamesTheCommand(t *testing.T) {
	t.Parallel()
	g := newGates()
	g.disrupt([]string{"ep-2", "ep-1"}, "reboot", 42, gateT0, 15*time.Second, 300*time.Second)
	if got := g.expire(gateT0.Add(299 * time.Second)); len(got) != 0 {
		t.Fatalf("expired before the backstop: %+v", got)
	}
	got := g.expire(gateT0.Add(300 * time.Second))
	want := []expiredHold{{endpoint: "ep-1", source: "command-42"}, {endpoint: "ep-2", source: "command-42"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expire = %+v, want %+v", got, want)
	}
}
