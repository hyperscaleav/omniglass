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
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The node's session manager (#816) over a faked held connection: which
// endpoints get a session, that a transition publishes under the listen
// task, that polls borrow the session, and that removal and shutdown tear it
// down. The real wire is the integration test's (run_session_test.go).

type heldFake struct {
	mu      sync.Mutex
	asked   []string
	replies []*driver.Reply
	done    chan struct{}
	once    sync.Once
	closed  bool
}

func (c *heldFake) Exchange(_ context.Context, line string, reply *driver.Reply, _ time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, line)
	c.replies = append(c.replies, reply)
	if strings.HasPrefix(line, "SUBSCRIBE") {
		return "OK", nil
	}
	if line == "SET INPUT bogus" {
		return "", &collection.RefusedError{Request: line, Line: "-ERR unknown command: SET INPUT bogus"}
	}
	return "INPUT hdmi2", nil
}
func (c *heldFake) Done() <-chan struct{} { return c.done }
func (c *heldFake) Err() error            { return errors.New("EOF") }
func (c *heldFake) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *heldFake) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

type heldFakeDialer struct {
	mu      sync.Mutex
	conns   []*heldFake
	targets []string
}

func (d *heldFakeDialer) Dial(_ context.Context, target string, _ time.Duration, _ collection.Inbound) (collection.HeldConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := &heldFake{done: make(chan struct{})}
	d.conns = append(d.conns, c)
	d.targets = append(d.targets, target)
	return c, nil
}

func (d *heldFakeDialer) all() []*heldFake {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]*heldFake(nil), d.conns...)
}

type published struct {
	taskID, endpoint string
	tr               collection.SessionTransition
}

type transitionLog struct {
	mu  sync.Mutex
	got []published
}

func (l *transitionLog) record(taskID, endpoint string, tr collection.SessionTransition) {
	l.mu.Lock()
	l.got = append(l.got, published{taskID, endpoint, tr})
	l.mu.Unlock()
}

func (l *transitionLog) waitFor(t *testing.T, taskID string, state collection.SessionState) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		for _, p := range l.got {
			if p.taskID == taskID && p.tr.State == state {
				l.mu.Unlock()
				return
			}
		}
		l.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no %s transition published for task %s; saw %+v", state, taskID, l.got)
}

func listenTask(id, endpointID, target string) collection.TaskSpec {
	return collection.TaskSpec{
		ID: id, Mode: "listen", Transport: "tcp", EndpointID: endpointID, EndpointName: "tcp",
		EndpointParams: json.RawMessage(`{"target":"` + target + `"}`),
		Spec: json.RawMessage(`{"driver":"newtron-nvp","function":"events","arm":["SUBSCRIBE INPUT"],
			"match":{"prefix":"EVT "},
			"emits":[{"name":"video-input","lane":"property","extract":{"regex":"^EVT INPUT (\\S+)$"}}]}`),
	}
}

func TestDesiredSessions(t *testing.T) {
	dialers := map[string]collection.SessionDialer{"tcp": &heldFakeDialer{}}
	poll := listenTask("p1", "ep-1", "10.0.0.5:51325")
	poll.Mode = "poll"
	snmpListen := listenTask("s1", "ep-2", "10.0.0.6:161")
	snmpListen.Transport = "snmp"
	noID := listenTask("n1", "", "10.0.0.7:51325")
	noTarget := listenTask("b1", "ep-4", "")
	tasks := []collection.TaskSpec{
		poll,
		listenTask("l2", "ep-1", "10.0.0.5:51325"),
		listenTask("l1", "ep-1", "10.0.0.5:51325"),
		snmpListen, noID, noTarget,
	}
	want, refused := desiredSessions(tasks, dialers)
	if len(want) != 1 {
		t.Fatalf("desired = %+v, want one session (ep-1)", want)
	}
	got := want["ep-1"]
	if got.conn.target != "10.0.0.5:51325" || got.taskID != "l1" || got.endpoint != "tcp" {
		t.Fatalf("ep-1 session = %+v, want its target and its lowest listen task", got)
	}
	if _, ok := refused["ep-4"]; !ok || len(refused) != 1 {
		t.Fatalf("refused = %v, want the endpoint with no target named", refused)
	}
}

func TestSessionManagerHoldsBorrowsAndTearsDown(t *testing.T) {
	dialer := &heldFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()

	pollSpec := json.RawMessage(`{"driver": "newtron-nvp", "function": "status",
		"request": {"line": "GET INPUT"},
		"emits": [{"name": "video-input", "lane": "property", "extract": {"regex": "^INPUT (\\S+)$"}}]}`)
	poll := collection.TaskSpec{
		ID: "p1", Mode: "poll", Transport: "tcp", EndpointID: "ep-1", EndpointName: "tcp",
		EndpointParams: json.RawMessage(`{"target":"10.0.0.5:51325"}`), Spec: pollSpec,
	}
	wl := []collection.TaskSpec{poll, listenTask("l1", "ep-1", "10.0.0.5:51325")}

	m.reconcile(wl)
	log.waitFor(t, "l1", collection.SessionEstablishing)
	log.waitFor(t, "l1", collection.SessionUp)

	// Polls borrow the held connection: two ticks, one dial.
	throwaway := &lineRecorder{}
	runner := &collection.Runner{Line: throwaway}
	for i := 0; i < 2; i++ {
		m.reconcile(wl) // an unchanged worklist keeps the session
		dps, faults := collectDriverTask(t.Context(), runner, poll, m)
		if len(faults) != 0 || len(dps) != 1 || dps[0].Text != "hdmi2" {
			t.Fatalf("tick %d: dps %v faults %v", i, dps, faults)
		}
	}
	conns := dialer.all()
	if len(conns) != 1 {
		t.Fatalf("dialed %d times across two ticks, want the one held connection", len(conns))
	}
	if len(throwaway.sent) != 0 {
		t.Fatalf("the poll dialed a throwaway connection: %v", throwaway.sent)
	}

	// A re-addressed endpoint recycles its session onto the new target.
	moved := []collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.9:51325")}
	m.reconcile(moved)
	if !conns[0].isClosed() {
		t.Fatal("re-addressing left the old connection held")
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(dialer.all()) < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	dialer.mu.Lock()
	targets := append([]string(nil), dialer.targets...)
	dialer.mu.Unlock()
	if len(targets) != 2 || targets[1] != "10.0.0.9:51325" {
		t.Fatalf("after re-address dialed %v", targets)
	}

	// Task removal tears the session down.
	m.reconcile(nil)
	if !dialer.all()[1].isClosed() {
		t.Fatal("task removal left the connection held")
	}
	if m.lookup("ep-1") != nil {
		t.Fatal("a removed endpoint still lends a session")
	}
	// Without a session, a poll falls back to the stateless exchange (the
	// recorder answers OK, so the emit faults; the dial is the point).
	collectDriverTask(t.Context(), &collection.Runner{Line: throwaway}, poll, m)
	if len(throwaway.sent) != 1 {
		t.Fatalf("stateless fallback sent %v", throwaway.sent)
	}
}

func TestSessionManagerCloseAllTearsDown(t *testing.T) {
	dialer := &heldFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	m.reconcile([]collection.TaskSpec{
		listenTask("l1", "ep-1", "10.0.0.5:51325"),
		listenTask("l2", "ep-2", "10.0.0.6:51325"),
	})
	log.waitFor(t, "l1", collection.SessionUp)
	log.waitFor(t, "l2", collection.SessionUp)
	m.closeAll()
	for i, c := range dialer.all() {
		if !c.isClosed() {
			t.Fatalf("connection %d survived shutdown", i)
		}
	}
	if m.lookup("ep-1") != nil || m.lookup("ep-2") != nil {
		t.Fatal("sessions survive closeAll")
	}
}

// A transition renders as one session-state property sample carrying the
// reason as a label.
func TestSessionSample(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	s := sessionSample(collection.SessionTransition{State: collection.SessionRecovering, Reason: "connection lost: EOF", At: at})
	ev := buildBatch("l1", "edge-1", []collection.Sample{s})
	if len(ev.Properties) != 1 || ev.Properties[0].Name != "session-state" || ev.Properties[0].ValueJson != `"recovering"` {
		t.Fatalf("property lane = %+v", ev.Properties)
	}
	if ev.Properties[0].Labels[collection.ReasonLabel] != "connection lost: EOF" || !ev.Properties[0].Ts.AsTime().Equal(at) {
		t.Fatalf("sample = %+v, want the reason label and the transition time", ev.Properties[0])
	}
}

// Review finding 6: a listen task's spec change re-hashes its id but changes
// nothing the connection depends on, so the healthy connection is kept and
// later transitions publish under the new task id. A credential change does
// recycle it.
func TestSessionManagerKeepsTheConnectionAcrossATaskRehash(t *testing.T) {
	dialer := &heldFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.5:51325")})
	log.waitFor(t, "l1", collection.SessionUp)

	rehashed := listenTask("l1-v2", "ep-1", "10.0.0.5:51325")
	// The listener's emits change (its id re-hashes); its arm conversation
	// does not, so the connection is kept.
	rehashed.Spec = json.RawMessage(`{"driver":"newtron-nvp","function":"events","arm":["SUBSCRIBE INPUT"],
		"match":{"prefix":"EVT "},
		"emits":[{"name":"video-input","lane":"property","extract":{"regex":"^EVT INPUT (\\w+)"}}]}`)
	m.reconcile([]collection.TaskSpec{rehashed})
	conns := dialer.all()
	if len(conns) != 1 || conns[0].isClosed() {
		t.Fatalf("a task re-hash recycled the healthy connection (%d dials, closed=%v)", len(conns), conns[0].isClosed())
	}
	conns[0].Close() // the device drops it
	log.waitFor(t, "l1-v2", collection.SessionRecovering)
	for wait := time.Now().Add(3 * time.Second); len(dialer.all()) < 2 && time.Now().Before(wait); {
		time.Sleep(2 * time.Millisecond) // the recovery redial
	}

	withCred := rehashed
	withCred.Secrets = map[string]map[string]string{"login": {"password": "new"}}
	m.reconcile([]collection.TaskSpec{withCred})
	deadline := time.Now().Add(3 * time.Second)
	for len(dialer.all()) < 3 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if got := dialer.all(); len(got) < 3 || !got[1].isClosed() {
		t.Fatalf("a credential change kept the old connection (%d dials)", len(got))
	}
}

// capture records what a router or manager published: samples and faults by
// listen task, and the arm conversation.
type capture struct {
	mu      sync.Mutex
	samples map[string][]collection.Sample
	faults  map[string][]string
	arms    []string
}

func newCapture() *capture {
	return &capture{samples: map[string][]collection.Sample{}, faults: map[string][]string{}}
}

func (c *capture) hooks(log *transitionLog) sessionHooks {
	return sessionHooks{
		transition: log.record,
		samples: func(taskID string, dps []collection.Sample) {
			c.mu.Lock()
			c.samples[taskID] = append(c.samples[taskID], dps...)
			c.mu.Unlock()
		},
		fault: func(taskID string, err error) {
			c.mu.Lock()
			c.faults[taskID] = append(c.faults[taskID], err.Error())
			c.mu.Unlock()
		},
		arm: func(taskID, endpoint, line, reply string, err error) {
			c.mu.Lock()
			c.arms = append(c.arms, line+" -> "+reply)
			c.mu.Unlock()
		},
	}
}

func (c *capture) faultsOf(taskID string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.faults[taskID]...)
}

func (c *capture) samplesOf(taskID string) []collection.Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]collection.Sample(nil), c.samples[taskID]...)
}

// #817: a listener claims the lines its match rule picks and lands them as
// push samples under its listen task; a line no listener claims is counted,
// and the spec's policy decides whether the tick lands a collection-failed.
func TestListenerRouterClaimsAndCountsTheRest(t *testing.T) {
	c := newCapture()
	want, _ := desiredSessions([]collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.5:51325")},
		map[string]collection.SessionDialer{"tcp": &heldFakeDialer{}})
	w := want["ep-1"]
	r := newListenerRouter(c.hooks(&transitionLog{}))
	r.set(w.listeners, w.policy)

	if !r.Claim(driver.LineMessage("EVT INPUT hdmi2")) {
		t.Fatal("the listener did not claim its line")
	}
	got := c.samplesOf("l1")
	if len(got) != 1 || got[0].Name != "video-input" || got[0].Text != "hdmi2" || !got[0].IsText {
		t.Fatalf("pushed samples = %+v, want video-input hdmi2 under the listen task", got)
	}
	if r.Claim(driver.LineMessage("INPUT hdmi1")) {
		t.Fatal("a line the match rule does not pick was claimed")
	}
	// A claimed line whose emit does not extract lands one fault, not one
	// per repeat.
	r.Claim(driver.LineMessage("EVT VOLUME 3"))
	r.Claim(driver.LineMessage("EVT VOLUME 3"))
	if f := c.faultsOf("l1"); len(f) != 1 {
		t.Fatalf("faults = %v, want one for the unextractable push", f)
	}

	r.Unclaimed(driver.LineMessage("NOISE 1"))
	r.Unclaimed(driver.LineMessage("NOISE 2"))
	n, last, policy := r.drain()
	if n != 2 || last != "NOISE 2" || policy != driver.UnmatchedDiscard {
		t.Fatalf("drain = %d, %q, %q; want the count, the last line, and the default discard", n, last, policy)
	}
	if n, _, _ := r.drain(); n != 0 {
		t.Fatalf("a second drain counted %d", n)
	}
}

// The unclaimed policy: discard counts and logs; collection-failed lands one
// fault per tick under the session's listen task naming the count.
func TestSessionManagerFlushesUnclaimedByPolicy(t *testing.T) {
	c := newCapture()
	task := listenTask("l1", "ep-1", "10.0.0.5:51325")
	task.Spec = json.RawMessage(`{"driver":"newtron-nvp","function":"events","arm":["SUBSCRIBE INPUT"],
		"match":{"prefix":"EVT "}, "unmatched":"collection-failed",
		"emits":[{"name":"video-input","lane":"property","extract":{"regex":"^EVT INPUT (\\S+)$"}}]}`)
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": &heldFakeDialer{}}, c.hooks(&transitionLog{}))
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{task})
	h := m.held["ep-1"]
	h.router.Unclaimed(driver.LineMessage("NOISE"))
	h.router.Unclaimed(driver.LineMessage("MORE NOISE"))
	m.flushUnclaimed()
	f := c.faultsOf("l1")
	if len(f) != 1 || !strings.Contains(f[0], "2 ") || !strings.Contains(f[0], "MORE NOISE") {
		t.Fatalf("faults = %v, want one naming the count and the last line", f)
	}
	m.flushUnclaimed()
	if f := c.faultsOf("l1"); len(f) != 1 {
		t.Fatalf("an empty tick landed a fault: %v", f)
	}
}

// The collection report counts a held session's pushes (#1369): the lines a
// listener claimed and the ones none did, every window, a quiet one as zero.
// A session recycled before the window is taken (a credential edit, a moved
// pin) keeps the counts its router held: they are the endpoint's, not the
// connection's.
func TestSessionManagerReportsItsListenersPushes(t *testing.T) {
	c := newCapture()
	task := listenTask("l1", "ep-1", "10.0.0.5:51325")
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": &heldFakeDialer{}}, c.hooks(&transitionLog{}))
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{task})
	tasks := publishTasks([]collection.TaskSpec{task})
	report := newCollectionReport(time.Minute, t0)
	take := func(at time.Time) map[string]float64 {
		t.Helper()
		m.reportPushes(report)
		return reportValues(t, report.take(at, tasks))["l1"]
	}

	h := m.held["ep-1"]
	if !h.router.Claim(driver.LineMessage("EVT INPUT hdmi2")) {
		t.Fatal("the listener did not claim its line")
	}
	h.router.Unclaimed(driver.LineMessage("NOISE 1"))
	h.router.Unclaimed(driver.LineMessage("NOISE 2"))
	m.flushUnclaimed() // the per-tick self-log drains its own count, not the report's
	if got := take(t0.Add(time.Minute)); got[probekeys.PushMessageCount] != 1 || got[probekeys.PushUnclaimedCount] != 2 {
		t.Fatalf("window = %v, want 1 claimed and 2 unclaimed", got)
	}
	if got := take(t0.Add(2 * time.Minute)); len(got) != 1 || got[probekeys.PushMessageCount] != 0 {
		t.Fatalf("a quiet window = %v, want push-message-count 0 and nothing else", got)
	}

	m.held["ep-1"].router.Claim(driver.LineMessage("EVT INPUT hdmi3"))
	m.teardown("ep-1", m.held["ep-1"], "recycled")
	m.reconcile([]collection.TaskSpec{task})
	if got := take(t0.Add(3 * time.Minute)); got[probekeys.PushMessageCount] != 1 {
		t.Fatalf("after a recycle = %v, want the claim made before it still counted", got)
	}
}

// The manager arms every connection with the listeners' instantiation
// commands (plumbing: reported to the arm hook, never a command row).
func TestSessionManagerArmsTheListeners(t *testing.T) {
	dialer := &heldFakeDialer{}
	c := newCapture()
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, c.hooks(log))
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.5:51325")})
	log.waitFor(t, "l1", collection.SessionUp)
	conn := dialer.all()[0]
	conn.mu.Lock()
	asked := append([]string(nil), conn.asked...)
	conn.mu.Unlock()
	if len(asked) == 0 || asked[0] != "SUBSCRIBE INPUT" {
		t.Fatalf("the connection was asked %v, want the subscription first", asked)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.arms) != 1 || c.arms[0] != "SUBSCRIBE INPUT -> OK" {
		t.Fatalf("arm reports = %v", c.arms)
	}
}

// #817: a command whose endpoint has a held session actuates over it (with
// its reply shape); with none held it falls back to a throwaway connection.
func TestExecuteDeliveryOverTheHeldSession(t *testing.T) {
	dialer := &heldFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.5:51325")})
	log.waitFor(t, "l1", collection.SessionUp)

	throwaway := &lineRecorder{}
	runner := &collection.Runner{Line: throwaway}
	d := collection.CommandDelivery{ID: 9, CommandType: "set-input", Transport: "tcp", Target: "10.0.0.5:51325",
		EndpointID: "ep-1", Line: "SET INPUT hdmi2", Reply: &driver.Reply{Prefix: "INPUT "}}
	reply, out := executeDelivery(t.Context(), runner, d, m)
	if out != "" || reply != "INPUT hdmi2" {
		t.Fatalf("held actuation = %q, %q", reply, out)
	}
	if len(throwaway.sent) != 0 {
		t.Fatalf("a held endpoint's command dialed a throwaway connection: %v", throwaway.sent)
	}
	conn := dialer.all()[0]
	conn.mu.Lock()
	last, shape := conn.asked[len(conn.asked)-1], conn.replies[len(conn.replies)-1]
	conn.mu.Unlock()
	if last != "SET INPUT hdmi2" || shape == nil || shape.Prefix != "INPUT " {
		t.Fatalf("the held connection was asked %q with reply shape %+v", last, shape)
	}

	d.EndpointID = "ep-unheld"
	if _, out := executeDelivery(t.Context(), runner, d, m); out != "" {
		t.Fatalf("fallback actuation failed: %s", out)
	}
	if len(throwaway.sent) != 1 {
		t.Fatalf("with no held session the command did not fall back: %v", throwaway.sent)
	}
}

// Review of #817, finding 1: a command the device refuses reports the
// device's text, both as the execution error and as the reply.
func TestExecuteDeliveryCarriesTheRefusal(t *testing.T) {
	dialer := &heldFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.5:51325")})
	log.waitFor(t, "l1", collection.SessionUp)
	d := collection.CommandDelivery{ID: 10, CommandType: "set-input", Transport: "tcp", Target: "10.0.0.5:51325",
		EndpointID: "ep-1", Line: "SET INPUT bogus", Reply: &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Prefix: "-ERR"}}}
	reply, out := executeDelivery(t.Context(), &collection.Runner{Line: &lineRecorder{}}, d, m)
	if reply != "-ERR unknown command: SET INPUT bogus" || !strings.Contains(out, "-ERR unknown command") {
		t.Fatalf("refused command reported reply %q, outcome %q; want the device's text in both", reply, out)
	}
}

// A tls command (#1035) rides the endpoint's held session or fails: tls has
// no stateless line, since a throwaway handshake would bypass the pin.
func TestExecuteDeliveryOverTLSNeverDialsAThrowaway(t *testing.T) {
	throwaway := &lineRecorder{}
	d := collection.CommandDelivery{ID: 11, CommandType: "set-input", Transport: "tls", Target: "10.0.0.5:51326",
		EndpointID: "ep-unheld", Line: "SET INPUT hdmi2", Reply: &driver.Reply{Prefix: "INPUT "}}
	_, out := executeDelivery(t.Context(), &collection.Runner{Line: throwaway}, d, nil)
	if !strings.Contains(out, "ride only a held session") {
		t.Fatalf("an unheld tls command = %q, want a refusal: it rides only a held session", out)
	}
	if len(throwaway.sent) != 0 {
		t.Fatalf("a tls command dialed a throwaway connection: %v", throwaway.sent)
	}
}
