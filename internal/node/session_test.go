package node

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The node's session manager (#816) over a faked held connection: which
// endpoints get a session, that a transition publishes under the listen
// task, that polls borrow the session, and that removal and shutdown tear it
// down. The real wire is the integration test's (run_session_test.go).

type heldFake struct {
	mu     sync.Mutex
	asked  []string
	done   chan struct{}
	once   sync.Once
	closed bool
}

func (c *heldFake) Exchange(_ context.Context, line string, _ time.Duration) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, line)
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

func (d *heldFakeDialer) Dial(_ context.Context, target string, _ time.Duration) (collection.HeldConn, error) {
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
		Spec:           json.RawMessage(`{"driver":"newtron-nvp","function":"events"}`),
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
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, log.record)
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
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, log.record)
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
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, log.record)
	defer m.closeAll()
	m.reconcile([]collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.5:51325")})
	log.waitFor(t, "l1", collection.SessionUp)

	rehashed := listenTask("l1-v2", "ep-1", "10.0.0.5:51325")
	rehashed.Spec = json.RawMessage(`{"driver":"newtron-nvp","function":"events","match":{"prefix":"EVT "}}`)
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
