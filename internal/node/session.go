package node

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The node's held sessions (#816) and the listeners that live on them
// (#817). An endpoint whose worklist carries a standing listen task over a
// transport with a session dialer gets one held session: the listen tasks
// are what need the connection held, their instantiation commands arm it on
// establish and on every recover, their match rules claim the device's
// pushes, and the endpoint's polls and commands borrow the connection rather
// than dialing. The manager reconciles the held set against every worklist
// pull: a new listen task starts a session, a removed one (or a change to
// what the connection depends on) tears it down, node shutdown tears down
// them all. It runs on the run loop's goroutine; what a session's goroutines
// read back (where to publish, the listeners) sits behind locks.

// sessionHooks is where a session's facts go: its transitions, the samples
// its listeners land, the faults they meet, and the arm conversation.
type sessionHooks struct {
	transition func(taskID, endpoint string, tr collection.SessionTransition)
	samples    func(taskID string, dps []collection.Sample)
	fault      func(taskID string, err error)
	arm        func(taskID, endpoint, line, reply string, err error)
}

// sessionConn is what a held connection depends on: the transport, the
// address, the wait, the credentials, and the arm conversation. A change to
// any of them recycles the session; anything else (a listener's match or
// emits, re-hashing its task id) keeps the healthy connection.
type sessionConn struct {
	transport   string
	target      string
	timeout     time.Duration
	credentials string // canonical JSON of the listen task's delivered secrets
	arm         string // the instantiation commands, in order, newline-joined
}

// routedListener is one listener as the node runs it: its listen task and
// its baked function.
type routedListener struct {
	taskID string
	fn     *driver.BakedFunction
}

// sessionWant is one session the worklist asks for: its connection, the
// listen task its transitions publish under (owner and instance bind
// server-side from that task's endpoint, like every other sample), its
// listeners, and the spec's unmatched-line policy.
type sessionWant struct {
	conn      sessionConn
	taskID    string
	endpoint  string
	listeners []routedListener
	policy    string
}

// desiredSessions derives the held set from a worklist. Pure. A listen task
// over a transport with no session dialer, or carrying no endpoint id (an
// older server), asks for nothing; an endpoint whose params or listen spec
// are unusable is refused by endpoint id with the reason. Every listen task
// on an endpoint shares its one session: transitions publish under the
// lowest task id, the listeners are ordered by function name (the order the
// first claim is offered in, and the arm commands are sent in), and the
// policy is collection-failed when any listener's spec says so.
func desiredSessions(tasks []collection.TaskSpec, dialers map[string]collection.SessionDialer) (map[string]sessionWant, map[string]string) {
	want := map[string]sessionWant{}
	refused := map[string]string{}
	sorted := append([]collection.TaskSpec(nil), tasks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, task := range sorted {
		if task.Mode != "listen" || task.EndpointID == "" || dialers[task.Transport] == nil {
			continue
		}
		if _, bad := refused[task.EndpointID]; bad {
			continue
		}
		fn, err := driver.ParseBaked(task.Spec)
		if err != nil {
			refused[task.EndpointID] = fmt.Sprintf("listen task %s: %v", task.ID, err)
			delete(want, task.EndpointID)
			continue
		}
		if w, held := want[task.EndpointID]; held {
			w.listeners = append(w.listeners, routedListener{taskID: task.ID, fn: fn})
			want[task.EndpointID] = w
			continue
		}
		var p endpointParams
		if len(task.EndpointParams) > 0 {
			if err := json.Unmarshal(task.EndpointParams, &p); err != nil {
				refused[task.EndpointID] = fmt.Sprintf("bad endpoint params: %v", err)
				continue
			}
		}
		if p.Target == "" {
			refused[task.EndpointID] = "empty session target"
			continue
		}
		var timeout time.Duration
		if p.Timeout != "" {
			d, err := time.ParseDuration(p.Timeout)
			if err != nil {
				refused[task.EndpointID] = fmt.Sprintf("bad timeout %q: %v", p.Timeout, err)
				continue
			}
			timeout = d
		}
		creds := ""
		if len(task.Secrets) > 0 {
			b, _ := json.Marshal(task.Secrets) // map keys marshal sorted: canonical
			creds = string(b)
		}
		want[task.EndpointID] = sessionWant{
			conn:      sessionConn{transport: task.Transport, target: p.Target, timeout: timeout, credentials: creds},
			taskID:    task.ID,
			endpoint:  task.EndpointName,
			listeners: []routedListener{{taskID: task.ID, fn: fn}},
		}
	}
	for id, w := range want {
		sort.SliceStable(w.listeners, func(i, j int) bool { return w.listeners[i].fn.Function < w.listeners[j].fn.Function })
		var arm []string
		w.policy = driver.UnmatchedDiscard
		for _, l := range w.listeners {
			arm = append(arm, l.fn.Arm...)
			if l.fn.Unmatched == driver.UnmatchedCollectionFailed {
				w.policy = driver.UnmatchedCollectionFailed
			}
		}
		w.conn.arm = strings.Join(arm, "\n")
		want[id] = w
	}
	return want, refused
}

// listenerRouter is a held connection's Inbound: it offers each inbound line
// to the listeners in order, lands a claimed line's emits as push samples
// under the claiming listen task, and counts what nobody claims.
type listenerRouter struct {
	hooks sessionHooks

	mu        sync.Mutex
	listeners []routedListener
	policy    string
	faultseen map[string]string // last fault per listen task: one event per change
	unclaimed int
	last      string
}

func newListenerRouter(hooks sessionHooks) *listenerRouter {
	return &listenerRouter{hooks: hooks, faultseen: map[string]string{}, policy: driver.UnmatchedDiscard}
}

// set swaps the listeners and the policy in place (a listener re-hash keeps
// its connection).
func (r *listenerRouter) set(ls []routedListener, policy string) {
	r.mu.Lock()
	r.listeners, r.policy = ls, policy
	r.mu.Unlock()
}

// Claim offers a line to the listeners: the first whose match rule picks it
// claims it, and its emits are interpreted at once and published (push, not
// poll). A claimed line whose emits do not extract is a collection-failed
// fault, landed once per change so a repeating push cannot flood the lane.
func (r *listenerRouter) Claim(line string) bool {
	r.mu.Lock()
	var claimer *routedListener
	for i := range r.listeners {
		if m := r.listeners[i].fn.Match; m != nil && m.Matches(line) {
			claimer = &r.listeners[i]
			break
		}
	}
	r.mu.Unlock()
	if claimer == nil {
		return false
	}
	emitted, faults := claimer.fn.Interpret(driver.Payload{Text: line}, time.Now().UTC())
	if len(emitted) > 0 && r.hooks.samples != nil {
		r.hooks.samples(claimer.taskID, samplesOf(emitted))
	}
	current := ""
	for _, f := range faults {
		current += f.Error() + "\n"
	}
	r.mu.Lock()
	changed := current != r.faultseen[claimer.taskID]
	r.faultseen[claimer.taskID] = current
	r.mu.Unlock()
	if changed && r.hooks.fault != nil {
		for _, f := range faults {
			r.hooks.fault(claimer.taskID, f)
		}
	}
	return true
}

// Unclaimed counts a line no listener claimed and no exchange was owed.
func (r *listenerRouter) Unclaimed(line string) {
	r.mu.Lock()
	r.unclaimed++
	r.last = line
	r.mu.Unlock()
}

// drain returns and resets the unclaimed count since the last drain, with
// the last such line and the policy that decides what becomes of them.
func (r *listenerRouter) drain() (n int, last, policy string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, last, policy = r.unclaimed, r.last, r.policy
	r.unclaimed, r.last = 0, ""
	return n, last, policy
}

// samplesOf maps interpreted emits to the samples a batch carries.
func samplesOf(emitted []driver.Emitted) []collection.Sample {
	out := make([]collection.Sample, 0, len(emitted))
	for _, e := range emitted {
		out = append(out, collection.Sample{Name: e.Name, Value: e.Value, Text: e.Text, IsText: e.IsText, TS: e.TS})
	}
	return out
}

// heldSession is one running session, the connection it was started for,
// its listeners, and where its transitions publish now (updated in place by
// a re-hash).
type heldSession struct {
	conn   sessionConn
	sess   *collection.Session
	router *listenerRouter

	mu       sync.Mutex
	taskID   string
	endpoint string
}

func (h *heldSession) publishTo() (string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.taskID, h.endpoint
}

// sessionManager owns the node's held sessions, keyed by endpoint id.
type sessionManager struct {
	dialers map[string]collection.SessionDialer
	hooks   sessionHooks
	held    map[string]*heldSession
	refused map[string]string // last refusal per endpoint, so it logs once
}

func newSessionManager(dialers map[string]collection.SessionDialer, hooks sessionHooks) *sessionManager {
	return &sessionManager{dialers: dialers, hooks: hooks, held: map[string]*heldSession{}, refused: map[string]string{}}
}

// reconcile brings the held set to what the worklist asks for.
func (m *sessionManager) reconcile(tasks []collection.TaskSpec) {
	want, refused := desiredSessions(tasks, m.dialers)
	for id, h := range m.held {
		w, ok := want[id]
		switch {
		case !ok:
			m.teardown(id, h, "its listen task left the worklist")
		case w.conn != h.conn:
			m.teardown(id, h, "its endpoint changed")
		default:
			h.mu.Lock()
			h.taskID, h.endpoint = w.taskID, w.endpoint
			h.mu.Unlock()
			h.router.set(w.listeners, w.policy)
		}
	}
	for id, w := range want {
		if _, ok := m.held[id]; ok {
			continue
		}
		m.start(id, w)
	}
	for id, reason := range refused {
		if m.refused[id] != reason {
			slog.Warn("session refused", "facility", "session", "endpoint_id", id, "reason", reason)
		}
	}
	m.refused = refused
}

func (m *sessionManager) start(id string, w sessionWant) {
	hooks := m.hooks
	h := &heldSession{conn: w.conn, taskID: w.taskID, endpoint: w.endpoint, router: newListenerRouter(hooks)}
	h.router.set(w.listeners, w.policy)
	var arm []string
	if w.conn.arm != "" {
		arm = strings.Split(w.conn.arm, "\n")
	}
	h.sess = collection.StartSession(collection.SessionConfig{
		Target:  w.conn.target,
		Timeout: w.conn.timeout,
		Dialer:  m.dialers[w.conn.transport],
		Inbound: h.router,
		Arm:     arm,
		OnArm: func(line, reply string, err error) {
			if hooks.arm != nil {
				taskID, endpoint := h.publishTo()
				hooks.arm(taskID, endpoint, line, reply, err)
			}
		},
		OnTransition: func(tr collection.SessionTransition) {
			if hooks.transition != nil {
				taskID, endpoint := h.publishTo()
				hooks.transition(taskID, endpoint, tr)
			}
		},
	})
	m.held[id] = h
}

func (m *sessionManager) teardown(id string, h *heldSession, why string) {
	h.sess.Close()
	delete(m.held, id)
	taskID, endpoint := h.publishTo()
	slog.Info("session torn down", "facility", "session", "task", taskID, "endpoint", endpoint, "target", h.conn.target, "reason", why)
}

// flushUnclaimed reports, once per tick, the inbound lines no listener
// claimed: a self-log with the count and the last, and, when the spec's
// policy is collection-failed, one collection-failed under the session's
// listen task.
func (m *sessionManager) flushUnclaimed() {
	if m == nil {
		return
	}
	for _, h := range m.held {
		n, last, policy := h.router.drain()
		if n == 0 {
			continue
		}
		taskID, endpoint := h.publishTo()
		slog.Info("unclaimed lines", "facility", "session", "task", taskID, "endpoint", endpoint, "count", n, "last", last, "policy", policy)
		if policy == driver.UnmatchedCollectionFailed && m.hooks.fault != nil {
			m.hooks.fault(taskID, fmt.Errorf("%d inbound lines no listener claimed since the last tick, last: %q", n, last))
		}
	}
}

// lookup is the session a poll or a command on endpointID borrows, or nil (a
// nil interface, never a typed nil) when the node holds none for it.
func (m *sessionManager) lookup(endpointID string) collection.HeldExchanger {
	if m == nil || endpointID == "" {
		return nil
	}
	if h, ok := m.held[endpointID]; ok {
		return h.sess
	}
	return nil
}

// closeAll tears down every held session: node shutdown.
func (m *sessionManager) closeAll() {
	for id, h := range m.held {
		m.teardown(id, h, "node shutdown")
	}
}

// sessionSample renders a transition as the session-state property sample:
// the state as the value, the reason as a label, stamped at the transition.
func sessionSample(tr collection.SessionTransition) collection.Sample {
	return collection.Sample{
		Name:   collection.SignalSessionState,
		Text:   string(tr.State),
		IsText: true,
		TS:     tr.At,
		Labels: map[string]string{collection.ReasonLabel: tr.Reason},
	}
}
