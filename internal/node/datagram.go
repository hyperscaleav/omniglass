package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The node's udp endpoints (#1037). Every endpoint over udp gets its own
// bound socket (decision 5 of #1076): the driver's listen-port input, else a
// port the kernel picks. Its request rung, polls, commands and arm
// conversation all leave from it, so a reply and a push arrive on the one
// socket, and a datagram from any other source is counted and dropped. It is
// not a held session: there is no connection to hold and no session-state;
// reachability is the request rung, every tick. The session manager owns the
// sockets beside the sessions, since they share its gates, its publishing
// task per endpoint and its once-per-tick flush.

// datagramConn is what an endpoint's socket depends on: the component's
// address, the local port, and the driver's terminal rules. A change to any
// rebinds it; a listener's change keeps it.
type datagramConn struct {
	target     string
	listenPort int
	terminal   string // the session block's terminal rules, as JSON
}

// datagramWant is one socket the worklist asks for: its connection, the
// task it logs under (the endpoint's lowest), its listeners, the
// unmatched-line policy, the arm conversation and the timeout.
type datagramWant struct {
	conn      datagramConn
	taskID    string
	endpoint  string
	listeners []routedListener
	policy    string
	arm       []string
	timeout   time.Duration
	session   *driver.SessionSpec
}

// listenPortOf reads the listen port off an endpoint's params through the
// one reading the write path shares (driver.ParseListenPort); zero when none
// is set.
func listenPortOf(raw json.RawMessage) (int, error) {
	var p struct {
		ListenPort json.RawMessage `json:"listen-port"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &p); err != nil {
			return 0, err
		}
	}
	return driver.ParseListenPort(p.ListenPort)
}

// desiredDatagrams derives the socket set from a worklist. Pure. Every task
// over udp carrying an endpoint id asks for its endpoint's socket; an
// endpoint whose params are unusable is refused by endpoint id with the
// reason. The listeners are ordered by function name (the order a claim is
// offered in and the arm lines are sent in), and the policy is
// collection-failed when any listener's spec says so.
func desiredDatagrams(tasks []collection.TaskSpec) (map[string]datagramWant, map[string]string) {
	want := map[string]datagramWant{}
	refused := map[string]string{}
	sorted := append([]collection.TaskSpec(nil), tasks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, task := range sorted {
		if task.Transport != "udp" || task.EndpointID == "" {
			continue
		}
		if _, bad := refused[task.EndpointID]; bad {
			continue
		}
		var listener []routedListener
		if task.Mode == "listen" {
			fn, err := driver.ParseBaked(task.Spec)
			if err != nil {
				refused[task.EndpointID] = fmt.Sprintf("listen task %s: %v", task.ID, err)
				delete(want, task.EndpointID)
				continue
			}
			listener = []routedListener{{taskID: task.ID, fn: fn}}
		}
		if w, held := want[task.EndpointID]; held {
			w.listeners = append(w.listeners, listener...)
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
			refused[task.EndpointID] = "empty udp target"
			continue
		}
		port, err := listenPortOf(task.EndpointParams)
		if err != nil {
			refused[task.EndpointID] = err.Error()
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
		terminal := ""
		if task.Session != nil && len(task.Session.Terminal) > 0 {
			b, _ := json.Marshal(task.Session.Terminal)
			terminal = string(b)
		}
		want[task.EndpointID] = datagramWant{
			conn:      datagramConn{target: p.Target, listenPort: port, terminal: terminal},
			taskID:    task.ID,
			endpoint:  task.EndpointName,
			listeners: listener,
			timeout:   timeout,
			session:   task.Session,
		}
	}
	for id, w := range want {
		sort.SliceStable(w.listeners, func(i, j int) bool { return w.listeners[i].fn.Function < w.listeners[j].fn.Function })
		w.policy = driver.UnmatchedDiscard
		w.arm = nil
		for _, l := range w.listeners {
			w.arm = append(w.arm, l.fn.Arm...)
			if l.fn.Unmatched == driver.UnmatchedCollectionFailed {
				w.policy = driver.UnmatchedCollectionFailed
			}
		}
		want[id] = w
	}
	return want, refused
}

// heldDatagram is one endpoint's bound socket, its listeners, and what its
// arm conversation and logs go under (updated in place by a re-hash).
type heldDatagram struct {
	conn   datagramConn
	sock   *collection.DatagramConn
	router *listenerRouter

	mu       sync.Mutex
	taskID   string
	endpoint string
	arm      []string
	timeout  time.Duration
	arming   bool
	// up is the request rung's last verdict, nil before the first: a rung
	// that recovers (down, then up) re-arms, since a component that
	// rebooted forgot its subscriber.
	up *bool
}

func (h *heldDatagram) publishTo() (string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.taskID, h.endpoint
}

// reconcileDatagrams brings the socket set to what the worklist asks for.
func (m *sessionManager) reconcileDatagrams(tasks []collection.TaskSpec) {
	want, refused := desiredDatagrams(tasks)
	for id, h := range m.datagrams {
		w, ok := want[id]
		switch {
		case !ok:
			m.closeDatagram(id, h, "it left the worklist")
		case w.conn != h.conn:
			m.closeDatagram(id, h, "its endpoint changed")
		default:
			h.mu.Lock()
			h.taskID, h.endpoint, h.arm, h.timeout = w.taskID, w.endpoint, w.arm, w.timeout
			h.mu.Unlock()
			h.router.set(w.listeners, w.policy)
		}
	}
	for id, w := range want {
		if _, ok := m.datagrams[id]; ok {
			continue
		}
		if err := m.openDatagram(id, w); err != nil {
			refused[id] = err.Error()
		}
	}
	for id, reason := range refused {
		if m.datagramRefused[id] != reason {
			slog.Warn("udp socket refused", "facility", "session", "endpoint_id", id, "reason", reason)
		}
	}
	m.datagramRefused = refused
}

// openDatagram binds an endpoint's socket and arms its listeners. A bind
// that fails (the listen port in use) is retried on the next reconcile.
func (m *sessionManager) openDatagram(id string, w datagramWant) error {
	router := newListenerRouter(m.hooks)
	router.set(w.listeners, w.policy)
	sock, err := collection.ListenDatagram(collection.DatagramConfig{
		Target:    w.conn.target,
		LocalPort: w.conn.listenPort,
		Inbound:   router,
		Terminal:  collection.TerminalClassifier(w.session),
	})
	if err != nil {
		return err
	}
	h := &heldDatagram{conn: w.conn, sock: sock, router: router, taskID: w.taskID, endpoint: w.endpoint, arm: w.arm, timeout: w.timeout}
	m.datagrams[id] = h
	slog.Info("udp socket bound", "facility", "session", "task", w.taskID, "endpoint", w.endpoint, "target", w.conn.target, "local", sock.LocalAddr().String())
	m.armDatagram(id, h)
	return nil
}

func (m *sessionManager) closeDatagram(id string, h *heldDatagram, why string) {
	_ = h.sock.Close()
	delete(m.datagrams, id)
	taskID, endpoint := h.publishTo()
	slog.Info("udp socket closed", "facility", "session", "task", taskID, "endpoint", endpoint, "target", h.conn.target, "reason", why)
}

// armDatagram sends the listeners' instantiation commands from the
// endpoint's socket, in order, off the run loop, one conversation at a time
// per endpoint. Each is sent once, as a command is: a component whose
// subscribe adds a destination per request must not get one per retry, and
// a lost one is sent again when the rung next recovers. The ack is the first
// line of any kind; a component that subscribes silently is armed,
// unacknowledged. A terminal reply gates the endpoint, as it does a held
// session's. Beneath a gate nothing is sent under its fingerprint, so the arm
// waits: the rung reads down until the gate lifts, and its first answer then
// arms, as a recovery does.
func (m *sessionManager) armDatagram(id string, h *heldDatagram) {
	if _, gated := m.blocked(id, rungResponsive); gated {
		down := false
		h.mu.Lock()
		h.up = &down
		h.mu.Unlock()
		return
	}
	h.mu.Lock()
	if len(h.arm) == 0 || h.arming {
		h.mu.Unlock()
		return
	}
	h.arming = true
	arm, timeout := append([]string(nil), h.arm...), h.timeout
	h.mu.Unlock()
	fingerprint := m.currentFingerprint(id)
	go func() {
		defer func() {
			h.mu.Lock()
			h.arming = false
			h.mu.Unlock()
		}()
		for _, line := range arm {
			ans, err := h.sock.Exchange(context.Background(), line, driver.AnyLine(), timeout, 0)
			if errors.Is(err, collection.ErrNoAnswer) {
				err = collection.ErrUnacknowledged
			}
			if m.hooks.arm != nil {
				taskID, endpoint := h.publishTo()
				m.hooks.arm(taskID, endpoint, line, ans.Line, err)
			}
			if terr, ok := collection.AsTerminal(err); ok {
				m.refuseOver("udp", id, fingerprint, terr)
				return
			}
			if err != nil && !errors.Is(err, collection.ErrUnacknowledged) {
				return
			}
		}
	}()
}

// datagramOf is the socket a poll or command on endpointID borrows, or nil
// (a nil interface, never a typed nil) when none is bound.
func (m *sessionManager) datagramOf(endpointID string) collection.DatagramExchanger {
	if m == nil || endpointID == "" {
		return nil
	}
	if h, ok := m.datagrams[endpointID]; ok {
		return h.sock
	}
	return nil
}

// datagramTimeout is the endpoint's timeout (zero: the default), which a
// command's exchange shares out among its attempts.
func (m *sessionManager) datagramTimeout(endpointID string) time.Duration {
	if m == nil {
		return 0
	}
	h, ok := m.datagrams[endpointID]
	if !ok {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.timeout
}

// datagramRefusal says why an endpoint has no socket, for a fault.
func (m *sessionManager) datagramRefusal(endpointID string) string {
	if m == nil {
		return "no session manager"
	}
	if r := m.datagramRefused[endpointID]; r != "" {
		return r
	}
	return "the endpoint is not in the worklist"
}

// datagramVerdict records the request rung's verdict for an endpoint; a
// rung that recovers re-arms its listeners.
func (m *sessionManager) datagramVerdict(endpointID string, up bool) {
	if m == nil {
		return
	}
	h, ok := m.datagrams[endpointID]
	if !ok {
		return
	}
	h.mu.Lock()
	recovered := up && h.up != nil && !*h.up
	h.up = &up
	h.mu.Unlock()
	if recovered {
		m.armDatagram(endpointID, h)
	}
}

// flushDatagrams reports, once per tick, each socket's lines no listener
// claimed (by the spec's policy, as a held session's) and the datagrams it
// dropped from another source: counted, self-logged, never published.
func (m *sessionManager) flushDatagrams() {
	for _, h := range m.datagrams {
		taskID, endpoint := h.publishTo()
		if n, last := h.sock.DrainForeign(); n > 0 {
			slog.Warn("foreign datagrams dropped", "facility", "session", "task", taskID, "endpoint", endpoint, "count", n, "last_source", last, "component", h.conn.target)
		}
		n, last, policy := h.router.drain()
		if n == 0 {
			continue
		}
		slog.Info("unclaimed lines", "facility", "session", "task", taskID, "endpoint", endpoint, "count", n, "last", last, "policy", policy)
		if policy == driver.UnmatchedCollectionFailed && m.hooks.fault != nil {
			m.hooks.fault(taskID, fmt.Errorf("%d inbound lines no listener claimed since the last tick, last: %q", n, last))
		}
	}
}

// collectDatagramRung runs a udp endpoint's request rung (ADR-0158): the
// driver's named request, sent from the endpoint's socket and answered, is
// the transport proven, since an open udp port cannot be detected. It lands
// endpoint-responsive with its reason (responded, timeout, refused when the
// host reports the port unreachable) and, when answered, one
// udp-response-time; never tcp-open. A reply matching the request's error
// shape is the component answering, so the rung passes; a reply the driver
// declares terminal gates the endpoint, and beneath a gate no request is
// sent under that fingerprint.
func collectDatagramRung(ctx context.Context, task collection.TaskSpec, req *driver.Request, sessions *sessionManager) ([]collection.Sample, error) {
	sock := sessions.datagramOf(task.EndpointID)
	if sock == nil {
		return nil, fmt.Errorf("node: task %s: no udp socket is bound for the endpoint: %s", task.ID, sessions.datagramRefusal(task.EndpointID))
	}
	now := time.Now().UTC()
	responsive := func(verdict, reason string) collection.Sample {
		return collection.Sample{Name: collection.SignalEndpointResponsive, Text: verdict, IsText: true, TS: now,
			Labels: map[string]string{collection.ReasonLabel: reason}}
	}
	if gt, ok := sessions.blocked(task.EndpointID, rungResponsive); ok {
		return []collection.Sample{responsive(collection.VerdictDown, "rejected: "+gt.class)}, nil
	}
	var timeout time.Duration
	var p endpointParams
	_ = json.Unmarshal(task.EndpointParams, &p) // desiredDatagrams has read it already
	if p.Timeout != "" {
		timeout, _ = time.ParseDuration(p.Timeout)
	}
	ans, err := sock.Exchange(ctx, req.Line, req.Reply, timeout, req.RetriesOr(driver.DefaultPollRetries))
	var refused *collection.RefusedError
	switch {
	case err == nil:
		sessions.datagramVerdict(task.EndpointID, true)
		return []collection.Sample{
			responsive(collection.VerdictUp, string(collection.Responded)),
			{Name: collection.SignalUDPResponseTime, Value: float64(ans.RTT.Microseconds()) / 1000, TS: now},
		}, nil
	case errors.As(err, &refused):
		sessions.datagramVerdict(task.EndpointID, true)
		return []collection.Sample{responsive(collection.VerdictUp, string(collection.Responded))}, nil
	}
	if terr, ok := collection.AsTerminal(err); ok {
		sessions.refuseOver(task.Transport, task.EndpointID, task.Fingerprint, terr)
		sessions.datagramVerdict(task.EndpointID, false)
		return []collection.Sample{responsive(collection.VerdictDown, "rejected: "+terr.Class)}, nil
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("node: task %s: %w", task.ID, ctx.Err())
	}
	reason, ok := collection.Classify(err)
	if !ok {
		return nil, fmt.Errorf("node: task %s: %w", task.ID, err)
	}
	sessions.datagramVerdict(task.EndpointID, false)
	return []collection.Sample{responsive(collection.VerdictDown, string(reason))}, nil
}
