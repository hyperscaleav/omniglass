package node

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The node's held sessions (#816). An endpoint whose worklist carries a
// standing listen task over a transport with a session dialer gets one held
// session: the listen task is what needs the connection held (its listeners
// arm over it, #817), and the endpoint's polls borrow it rather than dialing
// per tick. The manager reconciles the held set against every worklist pull:
// a new listen task starts a session, a removed one (or a re-addressed
// endpoint) tears its session down, and node shutdown tears down them all.
// It runs on the run loop's goroutine only; the one field a session's
// goroutine reads back (where to publish) sits behind its own lock.

// sessionConn is what a held connection depends on: the transport, the
// address, the wait, and the credentials. A change to any of them recycles
// the session; anything else (a listen task's spec re-hashing its id) keeps
// the healthy connection.
type sessionConn struct {
	transport   string
	target      string
	timeout     time.Duration
	credentials string // canonical JSON of the listen task's delivered secrets
}

// sessionWant is one session the worklist asks for: its connection, and the
// listen task its transitions publish under (owner and instance bind
// server-side from that task's endpoint, like every other sample).
type sessionWant struct {
	conn     sessionConn
	taskID   string
	endpoint string
}

// desiredSessions derives the held set from a worklist. Pure. A listen task
// over a transport with no session dialer, or carrying no endpoint id (an
// older server), asks for nothing; one whose params are unusable is
// refused by endpoint id with the reason. Two listen tasks on one endpoint
// share its one session, published under the lowest task id so the choice
// is stable across pulls.
func desiredSessions(tasks []collection.TaskSpec, dialers map[string]collection.SessionDialer) (map[string]sessionWant, map[string]string) {
	want := map[string]sessionWant{}
	refused := map[string]string{}
	sorted := append([]collection.TaskSpec(nil), tasks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for _, task := range sorted {
		if task.Mode != "listen" || task.EndpointID == "" || dialers[task.Transport] == nil {
			continue
		}
		if _, held := want[task.EndpointID]; held {
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
			conn:   sessionConn{transport: task.Transport, target: p.Target, timeout: timeout, credentials: creds},
			taskID: task.ID, endpoint: task.EndpointName,
		}
	}
	return want, refused
}

// heldSession is one running session, the connection it was started for,
// and where its transitions publish now (updated in place by a re-hash).
type heldSession struct {
	conn sessionConn
	sess *collection.Session

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
	dialers      map[string]collection.SessionDialer
	onTransition func(taskID, endpoint string, tr collection.SessionTransition)
	held         map[string]*heldSession
	refused      map[string]string // last refusal per endpoint, so it logs once
}

func newSessionManager(dialers map[string]collection.SessionDialer, onTransition func(taskID, endpoint string, tr collection.SessionTransition)) *sessionManager {
	return &sessionManager{dialers: dialers, onTransition: onTransition, held: map[string]*heldSession{}, refused: map[string]string{}}
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
	notify := m.onTransition
	h := &heldSession{conn: w.conn, taskID: w.taskID, endpoint: w.endpoint}
	h.sess = collection.StartSession(collection.SessionConfig{
		Target:  w.conn.target,
		Timeout: w.conn.timeout,
		Dialer:  m.dialers[w.conn.transport],
		OnTransition: func(tr collection.SessionTransition) {
			if notify != nil {
				taskID, endpoint := h.publishTo()
				notify(taskID, endpoint, tr)
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

// lookup is the session a poll on endpointID borrows, or nil (a nil
// interface, never a typed nil) when the node holds none for it.
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
