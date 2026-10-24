package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
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
	auth       func(taskID, endpoint string, v collection.AuthVerdict)
	samples    func(taskID string, dps []collection.Sample)
	fault      func(taskID string, err error)
	arm        func(taskID, endpoint, line, reply string, err error)
	// rejection hears an endpoint's endpoint-rejection change (#1056), with
	// the reason the gate holds for (#1110), empty when it knows none.
	rejection func(taskID, endpoint, value, reason string)
	// hostKey hears an ssh host key the session trusted (#1032).
	hostKey func(taskID, endpoint, fingerprint string)
	// certificate hears a tls public key the endpoint trusted on first use
	// (#1035), and republishes it while the server lacks it.
	certificate func(taskID, endpoint, fingerprint string)
}

// sessionConn is what a held connection depends on: the transport, the
// address, the wait, the credentials, the session block (its login and
// terminal replies, #1049), and the arm conversation. A change to any of
// them recycles the session, which is also how a rejected session leaves
// rejected when its credential or address is fixed; anything else (a
// listener's match or emits, re-hashing its task id) keeps the healthy
// connection.
type sessionConn struct {
	transport string
	target    string
	timeout   time.Duration
	arm       string // the instantiation commands, in order, newline-joined
	// identity is a SHA-256 digest over everything the session connects
	// with (#1056 ruling): the transport, address, timeout, the delivered
	// secret values, the driver's session block (its login and terminal
	// declarations) and the arm conversation. A secret rotated in the secret
	// store changes it, so a rejected session leaves rejected on its own; it
	// holds no credential beyond the digest, and is never logged.
	identity string
}

// sessionIdentity digests a session's connection inputs. Map keys marshal
// sorted, so the JSON is canonical.
func sessionIdentity(transport, target string, timeout time.Duration, secrets map[string]map[string]string, ss *driver.SessionSpec, arm, fingerprint string, retry int64) string {
	b, _ := json.Marshal(struct {
		Transport   string                       `json:"transport"`
		Target      string                       `json:"target"`
		Timeout     time.Duration                `json:"timeout"`
		Secrets     map[string]map[string]string `json:"secrets"`
		Session     *driver.SessionSpec          `json:"session"`
		Arm         string                       `json:"arm"`
		Fingerprint string                       `json:"fingerprint"`
		Retry       int64                        `json:"retry"`
	}{transport, target, timeout, secrets, ss, arm, fingerprint, retry})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
	// The auth step and the terminal handling (#1049), from the driver's
	// session block with the login rendered against the delivered secrets.
	auth     *collection.SessionAuth
	terminal collection.Classifier
	reprobe  time.Duration
	// retry is the endpoint's :retry counter and fingerprint the server's
	// connection fingerprint (which covers the counter): either moving is
	// a new session identity, so a rejected session dials once more.
	retry       int64
	fingerprint string
	// secrets and session feed the identity digest.
	secrets map[string]map[string]string
	session *driver.SessionSpec
	// hostKey is the ssh pin the worklist delivers; channel shapes the ssh
	// channel (#1032).
	hostKey string
	channel collection.ChannelOptions
	// tls is the handshake a tls endpoint's connections run (#1035): its
	// mode and server name; the pin is the endpoint's own, shared with its
	// probe, and joins at start.
	tls *collection.TLSTrust
}

// holdsSession reports whether a task asks its endpoint's session to be held:
// a standing listen task always does; a driver poll does when its driver
// declares a session (its login cannot run on a throwaway connection) or its
// transport has no stateless line (#1049).
func holdsSession(task collection.TaskSpec) bool {
	if task.Mode == "listen" {
		return true
	}
	return driver.IsBaked(task.Spec) && (task.Session != nil || task.Transport != "tcp")
}

// channelOf reads the ssh channel the driver declares.
func channelOf(ss *driver.SessionSpec) collection.ChannelOptions {
	if ss == nil {
		return collection.ChannelOptions{}
	}
	return collection.ChannelOptions{PTY: ss.PTY, Exec: ss.Exec}
}

// sessionAuthOf renders the driver's session block for one endpoint: the
// login's sends from the delivered secrets. A credential the login needs and
// the worklist did not deliver refuses the session, naming the reference.
func sessionAuthOf(task collection.TaskSpec) (*collection.SessionAuth, error) {
	ss := task.Session
	auth := &collection.SessionAuth{}
	if ss != nil && ss.Login != nil {
		sends, err := ss.Login.Render(task.Secrets)
		if err != nil {
			return nil, err
		}
		d := &collection.LoginDialog{Ready: ss.Login.Ready}
		for i, st := range ss.Login.Steps {
			d.Steps = append(d.Steps, collection.LoginPrompt{Expect: st.Expect, Send: sends[i]})
		}
		auth.Login = d
	}
	if task.Transport == "ssh" {
		// The transport's own credential, from the secret input the driver
		// names: a username with a password or a private key.
		cred := &collection.SSHAuth{}
		if ss != nil && ss.Credential != "" {
			f := task.Secrets[ss.Credential]
			cred.Username, cred.Password, cred.PrivateKey = f["username"], f["password"], f["private_key"]
			if cred.Username == "" || (cred.Password == "" && cred.PrivateKey == "" && auth.Login == nil) {
				return nil, fmt.Errorf("the ssh credential %s needs a username and a password or private_key, and no such credential was delivered", ss.Credential)
			}
		}
		auth.SSH = cred
	}
	if auth.Login == nil && auth.SSH == nil {
		return nil, nil
	}
	return auth, nil
}

// tlsTrustOf reads a tls endpoint's handshake off its params (#1035): the
// mode (empty verifies) and the server name, the target's host. The pin is
// the endpoint's own, joined by the session manager.
func tlsTrustOf(p endpointParams) (collection.TLSTrust, error) {
	mode, err := driver.ParseTLSMode(p.TLSMode)
	if err != nil {
		return collection.TLSTrust{}, fmt.Errorf("tls: %v", err)
	}
	host, _, err := net.SplitHostPort(p.Target)
	if err != nil {
		host = p.Target
	}
	return collection.TLSTrust{Mode: mode, ServerName: host}, nil
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
		if !holdsSession(task) || task.EndpointID == "" || dialers[task.Transport] == nil {
			continue
		}
		if _, bad := refused[task.EndpointID]; bad {
			continue
		}
		fn, err := driver.ParseBaked(task.Spec)
		if err != nil {
			refused[task.EndpointID] = fmt.Sprintf("%s task %s: %v", task.Mode, task.ID, err)
			delete(want, task.EndpointID)
			continue
		}
		var listener []routedListener
		if task.Mode == "listen" {
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
		auth, err := sessionAuthOf(task)
		if err != nil {
			refused[task.EndpointID] = fmt.Sprintf("session auth: %v", err)
			continue
		}
		var trust *collection.TLSTrust
		if task.Transport == "tls" {
			t, err := tlsTrustOf(p)
			if err != nil {
				refused[task.EndpointID] = err.Error()
				continue
			}
			trust = &t
		}
		want[task.EndpointID] = sessionWant{
			conn:        sessionConn{transport: task.Transport, target: p.Target, timeout: timeout},
			secrets:     task.Secrets,
			session:     task.Session,
			taskID:      task.ID,
			endpoint:    task.EndpointName,
			listeners:   listener,
			auth:        auth,
			terminal:    collection.TerminalClassifier(task.Session),
			reprobe:     task.Session.ReprobeInterval(),
			retry:       task.SessionRetry,
			fingerprint: task.Fingerprint,
			hostKey:     task.HostKey,
			channel:     channelOf(task.Session),
			tls:         trust,
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
		w.conn.identity = sessionIdentity(w.conn.transport, w.conn.target, w.conn.timeout, w.secrets, w.session, w.conn.arm, w.fingerprint, w.retry)
		w.secrets = nil // the digest is all the identity keeps
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
	pin    *collection.HostKeyPin
	// certPin is the tls pin the session was started with (#1035): the
	// endpoint's own, which the probe shares.
	certPin *collection.HostKeyPin

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
	// pins carries each ssh endpoint's trusted host key across a session's
	// recycle (a credential edit, a revision), so a key trusted on first use
	// stays the pin while the server does not yet hold it (#1032).
	pins map[string]string
	// gates is the layered gate per endpoint (#1056 ruling); published is
	// the endpoint-rejection each endpoint last published, reasonSent the
	// endpoint-rejection-reason (#1110), and pubTask the task (and endpoint
	// name) it publishes under. The worklist reflects the gate, never its
	// reason, so reasonSent outlives the reflection.
	gates      *gates
	pubMu      sync.Mutex
	published  map[string]string
	reasonSent map[string]string
	pubTask    map[string][2]string
	// certPins is each tls endpoint's public key pin (#1035), one per
	// endpoint and shared by its held session and its probe, so neither
	// trusts a key the other refused. It outlives a session's recycle (a key
	// trusted on first use stays the pin while the server does not yet hold
	// it) and is replaced only when the worklist delivers another pin (an
	// operator's :retry with certificate). Touched on the run loop only; the
	// pin itself is safe across goroutines.
	certPins map[string]*collection.HostKeyPin
	// datagrams is each udp endpoint's bound socket (#1037), and
	// datagramRefused the last reason each endpoint has none, so it logs
	// once. Touched on the run loop only.
	datagrams       map[string]*heldDatagram
	datagramRefused map[string]string
}

func newSessionManager(dialers map[string]collection.SessionDialer, hooks sessionHooks) *sessionManager {
	return &sessionManager{dialers: dialers, hooks: hooks, held: map[string]*heldSession{}, refused: map[string]string{}, pins: map[string]string{},
		gates: newGates(), published: map[string]string{}, reasonSent: map[string]string{}, pubTask: map[string][2]string{}, certPins: map[string]*collection.HostKeyPin{},
		datagrams: map[string]*heldDatagram{}, datagramRefused: map[string]string{}}
}

// syncCertPins brings each tls endpoint's pin to what the worklist
// delivers: a new endpoint takes the delivered pin (empty before first use),
// a different delivered pin replaces it, a key the node learned on first use
// that the worklist does not deliver (a lost publish) is published again, and
// a delivered pin the worklist stops delivering (withdrawn) is forgotten. An
// endpoint that left the worklist forgets its pin.
func (m *sessionManager) syncCertPins(tasks []collection.TaskSpec) {
	delivered := map[string]string{}
	for _, t := range tasks {
		if t.Transport != "tls" || t.EndpointID == "" {
			continue
		}
		if _, seen := delivered[t.EndpointID]; !seen || t.CertificatePin != "" {
			delivered[t.EndpointID] = t.CertificatePin
		}
	}
	for id := range m.certPins {
		if _, ok := delivered[id]; !ok {
			delete(m.certPins, id)
		}
	}
	for id, pin := range delivered {
		held := m.certPins[id]
		switch {
		case held == nil, pin != "" && pin != held.Get():
			m.certPins[id] = m.newCertPin(id, pin)
		case pin == "" && held.Get() != "" && !held.Learned():
			// A pin the server delivered and now withdraws (a declared pin
			// the writer ruling retired, ADR-0161): forgotten, so the next
			// connection trusts on first use; a session held under it
			// redials, its pin no longer the endpoint's.
			m.certPins[id] = m.newCertPin(id, "")
		case pin == "" && held.Get() != "":
			m.publishCertificate(id, held.Get())
		}
	}
}

// newCertPin makes an endpoint's pin; a key it trusts on first use is
// published as tls-certificate under the endpoint's publishing task.
func (m *sessionManager) newCertPin(endpointID, fp string) *collection.HostKeyPin {
	pin := collection.NewHostKeyPin(fp)
	pin.OnTrust = func(fp string) { m.publishCertificate(endpointID, fp) }
	return pin
}

func (m *sessionManager) publishCertificate(endpointID, fp string) {
	if m.hooks.certificate == nil {
		return
	}
	m.pubMu.Lock()
	task := m.pubTask[endpointID]
	m.pubMu.Unlock()
	if task[0] != "" {
		m.hooks.certificate(task[0], task[1], fp)
	}
}

// certPinOf is the tls endpoint's pin the probe shares, or a pin of the
// delivered key alone when the node holds none (no session manager).
func (m *sessionManager) certPinOf(task collection.TaskSpec) *collection.HostKeyPin {
	if m != nil {
		if pin, ok := m.certPins[task.EndpointID]; ok {
			return pin
		}
	}
	return collection.NewHostKeyPin(task.CertificatePin)
}

// syncRejection publishes the endpoint's endpoint-rejection when it differs
// from what it last published (none once its gate lifts), with the reason the
// gate holds for (#1110); and the gate again when only its reason moved (a
// re-probe meeting other words under the same fingerprint, or an adopted
// gate's first refusal of its own), since the worklist never reflects it.
func (m *sessionManager) syncRejection(endpointID string) {
	if m == nil {
		return
	}
	want, reason := m.gates.rejectionOf(endpointID)
	m.pubMu.Lock()
	last, seen := m.published[endpointID]
	moved := !(seen && last == want) && !(!seen && want == rejectionNone)
	if !moved && (reason == "" || reason == m.reasonSent[endpointID]) {
		m.pubMu.Unlock()
		return
	}
	m.published[endpointID] = want
	task := m.pubTask[endpointID]
	if m.hooks.rejection == nil || task[0] == "" {
		m.pubMu.Unlock()
		return
	}
	if want == rejectionNone {
		delete(m.reasonSent, endpointID)
	} else if reason != "" {
		m.reasonSent[endpointID] = reason
	}
	m.pubMu.Unlock()
	m.hooks.rejection(task[0], task[1], want, reason)
}

// refuse gates an endpoint for a terminal refusal and publishes it.
// fingerprint is the one the refused attempt was made under; a refusal
// from an older one gates nothing.
func (m *sessionManager) refuse(endpointID, fingerprint string, terr *collection.TerminalError) {
	if m == nil {
		return
	}
	if m.gates.record(endpointID, fingerprint, terr) {
		m.syncRejection(endpointID)
	}
}

// refuseOver gates an endpoint for a terminal refusal met over a transport,
// at the rung the refusal holds there: over a transport whose rung is a
// request (udp), nothing logs in, so every refusal holds the transport rung.
func (m *sessionManager) refuseOver(transportName, endpointID, fingerprint string, terr *collection.TerminalError) {
	if m == nil {
		return
	}
	if m.gates.recordAt(endpointID, fingerprint, terr, rungIn(transportName, terr.Class)) {
		m.syncRejection(endpointID)
	}
}

// currentFingerprint is the fingerprint the endpoint is connected with now.
func (m *sessionManager) currentFingerprint(endpointID string) string {
	if m == nil {
		return ""
	}
	return m.gates.fingerprintOf(endpointID)
}

// pass lifts an endpoint's gate (its rung passed) and publishes none.
func (m *sessionManager) pass(endpointID string) {
	if m == nil {
		return
	}
	m.gates.clear(endpointID)
	m.syncRejection(endpointID)
}

// blocked is the gate that blocks rung r for the endpoint, if any.
func (m *sessionManager) blocked(endpointID string, r rung) (gate, bool) {
	if m == nil {
		return gate{}, false
	}
	return m.gates.blocked(endpointID, r)
}

// observe takes each endpoint's fingerprint and published rejection off the
// worklist: a gate under another fingerprint lifts, one carried back from
// before a restart is adopted.
func (m *sessionManager) observe(tasks []collection.TaskSpec) {
	// The publishing task is chosen afresh from this worklist (a re-derived
	// driver deletes old task ids, and a sample under one is an orphan).
	pubTask := map[string][2]string{}
	reflected := map[string]string{}
	for _, t := range tasks {
		if t.EndpointID == "" {
			continue
		}
		if cur, ok := pubTask[t.EndpointID]; !ok || t.ID < cur[0] {
			pubTask[t.EndpointID] = [2]string{t.ID, t.EndpointName}
		}
		if _, ok := reflected[t.EndpointID]; !ok {
			reflected[t.EndpointID] = t.Rejection
			m.gates.observe(t.EndpointID, t.Fingerprint, t.Rejection)
		}
	}
	m.pubMu.Lock()
	m.pubTask = pubTask
	// What the server reflects back is what is published: a gate the node
	// holds that the worklist does not reflect (a lost publish) goes out
	// again on every pull until it does, as the trusted host key does.
	m.published = map[string]string{}
	for id, r := range reflected {
		if r != "" {
			m.published[id] = r
		}
	}
	m.pubMu.Unlock()
	for id := range reflected {
		m.syncRejection(id)
	}
}

// reconcile brings the held set to what the worklist asks for.
func (m *sessionManager) reconcile(tasks []collection.TaskSpec) {
	m.observe(tasks)
	m.syncCertPins(tasks)
	m.reconcileDatagrams(tasks)
	want, refused := desiredSessions(tasks, m.dialers)
	for id := range m.pins {
		if _, ok := want[id]; !ok {
			delete(m.pins, id)
		}
	}
	for id, h := range m.held {
		w, ok := want[id]
		// Only a key this node learned on first use is carried across a
		// recycle; a delivered pin is the server's, delivered again or not.
		if h.pin != nil && h.pin.Learned() {
			if fp := h.pin.Get(); fp != "" {
				m.pins[id] = fp
			}
		}
		switch {
		case !ok:
			m.teardown(id, h, "its listen task left the worklist")
		case w.conn != h.conn:
			m.teardown(id, h, "its endpoint changed")
		case h.pin != nil && w.hostKey != "" && w.hostKey != h.pin.Get():
			// The operator pinned a different key (a replaced component's,
			// accepted with :retry): the session redials onto it.
			m.teardown(id, h, "its host key pin changed")
		case h.pin != nil && w.hostKey == "" && h.pin.Get() != "" && !h.pin.Learned():
			// The server delivered this pin and now delivers none: it was
			// withdrawn (a declared pin the writer ruling retired, ADR-0161).
			// The node forgets it, and the session redials trusting on first
			// use rather than holding a key nobody vouches for.
			delete(m.pins, id)
			m.teardown(id, h, "its host key pin was withdrawn")
		case h.certPin != nil && h.certPin != m.certPins[id]:
			// The endpoint's tls pin was replaced (an operator's :retry with
			// certificate): the session redials onto the new one.
			m.teardown(id, h, "its certificate pin changed")
		default:
			h.mu.Lock()
			h.taskID, h.endpoint = w.taskID, w.endpoint
			h.mu.Unlock()
			h.router.set(w.listeners, w.policy)
			if h.pin != nil && w.hostKey == "" && h.pin.Get() != "" && h.pin.Learned() && m.hooks.hostKey != nil {
				// The worklist delivers no pin: the server never got the key
				// this session trusted on first use (a lost publish). Publish
				// it again; the property lane's transition guard collapses
				// repeats. A delivered pin is never republished as learned.
				m.hooks.hostKey(w.taskID, w.endpoint, h.pin.Get())
			}
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
	if w.auth != nil && w.auth.SSH != nil {
		h.pin = collection.NewHostKeyPin(w.hostKey)
		if w.hostKey == "" {
			// The key this node trusted on first use, carried across a recycle.
			h.pin = collection.NewLearnedHostKeyPin(m.pins[id])
		}
		// Every authenticated connection publishes its key: one lost publish
		// must not leave the server without the pin.
		h.pin.OnTrust = func(fp string) {
			if hooks.hostKey == nil {
				return
			}
			taskID, endpoint := h.publishTo()
			hooks.hostKey(taskID, endpoint, fp)
		}
		w.auth.SSH.Pin = h.pin
	}
	var trust *collection.TLSTrust
	if w.tls != nil {
		t := *w.tls
		if t.Mode == driver.TLSModePin {
			t.Pin = m.certPins[id]
			if t.Pin == nil {
				t.Pin = m.newCertPin(id, "")
				m.certPins[id] = t.Pin
			}
			h.certPin = t.Pin
		}
		trust = &t
	}
	var rejected *collection.TerminalError
	if gt, ok := m.gates.blocked(id, rungAuthenticated); ok {
		// A gate this node holds for this fingerprint, or carried back from
		// before a restart: the session starts rejected and dials nothing.
		rejected = gt.refusal()
	}
	h.sess = collection.StartSession(collection.SessionConfig{
		Rejected: rejected,
		Target:   w.conn.target,
		Timeout:  w.conn.timeout,
		Dialer:   m.dialers[w.conn.transport],
		Inbound:  h.router,
		Arm:      arm,
		Auth:     w.auth,
		Terminal: w.terminal,
		Reprobe:  w.reprobe,
		Channel:  w.channel,
		TLS:      trust,
		OnAuth: func(v collection.AuthVerdict) {
			if hooks.auth != nil {
				taskID, endpoint := h.publishTo()
				hooks.auth(taskID, endpoint, v)
			}
		},
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
			switch {
			case tr.State == collection.SessionRejected && tr.Refusal != nil && tr.Refusal != rejected:
				// Not the replay of the refusal the session was started
				// with (that gate is already held, and an adopted one's
				// refusal is the node's placeholder, #1110): a refusal met,
				// a re-probe's under the same fingerprint included.
				m.refuse(id, w.fingerprint, tr.Refusal)
			case tr.State == collection.SessionUp:
				m.pass(id)
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
	m.flushDatagrams()
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

// isUp reports whether the node holds a session for endpointID that reads up.
func (m *sessionManager) isUp(endpointID string) bool {
	if m == nil || endpointID == "" {
		return false
	}
	h, ok := m.held[endpointID]
	if !ok {
		return false
	}
	st, _ := h.sess.State()
	return st == collection.SessionUp
}

// closeAll tears down every held session: node shutdown.
func (m *sessionManager) closeAll() {
	for id, h := range m.held {
		m.teardown(id, h, "node shutdown")
	}
	for id, h := range m.datagrams {
		m.closeDatagram(id, h, "node shutdown")
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

// sessionSamples renders a transition as what it publishes: session-state,
// and session-reason (#1049), the reason as a property an event rule can
// read (`auth: <device text>` when rejected), since labels do not persist.
func sessionSamples(tr collection.SessionTransition) []collection.Sample {
	return []collection.Sample{sessionSample(tr), {
		Name:   collection.SignalSessionReason,
		Text:   tr.Reason,
		IsText: true,
		TS:     tr.At,
	}}
}

// rejectionSamples renders a gate as what it publishes: endpoint-rejection,
// and endpoint-rejection-reason beside it in the same batch and at the same
// time when the gate knows why it holds (#1110), so an endpoint with no held
// session still says why it was refused. A gate that knows no reason (none,
// or one adopted after a restart) lands alone, leaving the reason on record.
func rejectionSamples(value, reason string, at time.Time) []collection.Sample {
	out := []collection.Sample{{Name: collection.SignalEndpointRejection, Text: value, IsText: true, TS: at}}
	if reason != "" {
		out = append(out, collection.Sample{Name: collection.SignalEndpointRejectionReason, Text: reason, IsText: true, TS: at})
	}
	return out
}

// authSample renders the session's auth verdict as the endpoint-authenticated
// rung (#1049), the ladder's own vocabulary: yes or no, the reason a label.
func authSample(v collection.AuthVerdict) collection.Sample {
	verdict := "no"
	if v.Authenticated {
		verdict = "yes"
	}
	return collection.Sample{
		Name:   collection.SignalEndpointAuthenticated,
		Text:   verdict,
		IsText: true,
		TS:     v.At,
		Labels: map[string]string{collection.ReasonLabel: v.Reason},
	}
}
