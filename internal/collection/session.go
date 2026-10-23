package collection

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A session holds its connection (#816): a stateful transport keeps one
// connection to an endpoint across ticks instead of dialing per poll. The
// machine establishes it, lends it to polls while it is up, recovers it with a
// jittered, bounded backoff when it drops, and tears it down when the node
// stops wanting it. There is no session table: the machine's state is
// surfaced as the session-state property per endpoint (the transitions only)
// plus the node's own logs, which carry the reasons.

// SignalSessionState is the property the session's transitions land under,
// and SignalSessionReason the property their reasons land under (#1049).
const (
	SignalSessionState  = probekeys.SessionState
	SignalSessionReason = probekeys.SessionReason
	SignalSSHHostKey    = probekeys.SSHHostKey
	// SignalEndpointRejection is the endpoint's gate (ADR-0157).
	SignalEndpointRejection = probekeys.EndpointRejection
)

// SessionState is the session's honest state, the session-state property's
// value domain.
type SessionState string

const (
	// SessionEstablishing: the first connection is being made; the session has
	// never been up.
	SessionEstablishing SessionState = "establishing"
	// SessionUp: the connection is held and polls borrow it.
	SessionUp SessionState = "up"
	// SessionRecovering: a held connection dropped and the session is
	// reconnecting inside its attempt budget.
	SessionRecovering SessionState = "recovering"
	// SessionDown: the attempt budget is spent. The session keeps retrying at
	// the capped pace and holds down until a returning connection proves
	// itself (an answered exchange, or, unless answers were what failed,
	// outliving the stable window), so a device that returns reads up again
	// unattended and one that keeps failing publishes nothing more.
	SessionDown SessionState = "down"
	// SessionRejected: a refusal retrying cannot fix (#1049): the auth step
	// was refused, a reply the driver declares terminal arrived, or the
	// transport refused on its own account (a changed host key). The
	// session holds no connection and dials no more until its endpoint
	// changes, an operator retries, or the driver's slow re-probe comes due.
	SessionRejected SessionState = "rejected"
)

// The recovery bounds. A failed attempt waits SessionBackoff(attempt), an
// exponential curve from SessionBackoffBase capped at SessionBackoffMax and
// jittered by SessionBackoffJitter either way, so a fleet of sessions that
// lost one switch does not redial in lockstep. SessionAttemptBudget
// consecutive failed attempts move a session to down. An attempt fails when
// its dial fails, when its connection drops unanswered before
// SessionStableAfter (so a device that accepts and drops cannot hot-loop or
// read up forever), or when an exchange over it timed out (a hung device,
// however long the connection lived). An answered exchange forgives the
// failures before it, so a healthy device's rare timeout never adds up to
// down; a connection that outlives SessionStableAfter and drops for another
// reason forgives them too, and redials at once.
// SessionKeepAlive is the TCP keepalive on a held connection, which is what
// notices a peer that vanished without closing.
const (
	SessionBackoffBase   = 250 * time.Millisecond
	SessionBackoffMax    = 30 * time.Second
	SessionBackoffJitter = 0.2
	SessionAttemptBudget = 5
	SessionStableAfter   = 10 * time.Second
	SessionKeepAlive     = 15 * time.Second
)

// defaultSessionTimeout bounds a dial or an exchange when the endpoint sets
// no timeout, matching the stateless line client.
const defaultSessionTimeout = 5 * time.Second

// ErrSessionClosed is the answer to an exchange over a torn-down session.
var ErrSessionClosed = errors.New("collection: session torn down")

// SessionBackoff is the wait before the given attempt: zero for attempt 0
// (an immediate redial after a stable connection dropped), else
// SessionBackoffBase doubled per attempt, capped at SessionBackoffMax, scaled
// by a jitter factor drawn from unit (uniform in [0,1): 0.5 is no jitter).
// Pure: the caller supplies the randomness.
func SessionBackoff(attempt int, unit float64) time.Duration {
	if attempt <= 0 {
		return 0
	}
	d := SessionBackoffMax
	if attempt <= 30 { // past 2^30 the doubling only overflows; the cap governs
		if exp := SessionBackoffBase << (attempt - 1); exp < SessionBackoffMax {
			d = exp
		}
	}
	jittered := time.Duration(float64(d) * (1 - SessionBackoffJitter + 2*SessionBackoffJitter*unit))
	if jittered > SessionBackoffMax {
		return SessionBackoffMax
	}
	return jittered
}

// SessionTransition is one change of a session's state, with why and when.
type SessionTransition struct {
	State  SessionState
	Reason string
	At     time.Time
	// Refusal is a rejected transition's terminal error (#1056 ruling), the
	// class and text the node's gate holds.
	Refusal *TerminalError
}

// HeldConn is one established connection a session holds: it serves
// request-answer exchanges, one at a time, and closes Done when it dies
// (Err then says why). Faked in unit tests; the real line connection is
// NewLineSessionDialer's.
type HeldConn interface {
	// Exchange sends one line and returns its answer: the next line no
	// listener claims, or, when reply declares the protocol's answer shape,
	// the next line that shape matches.
	Exchange(ctx context.Context, line string, reply *driver.Reply, timeout time.Duration) (string, error)
	Done() <-chan struct{}
	Err() error
	Close() error
}

// Inbound is how a held connection disposes of an inbound line (#817): Claim
// offers it to the listeners (true when one took it, which also delivers
// it), and Unclaimed reports a line nothing took, neither a listener nor an
// exchange. Both are called on the connection's reader goroutine, in order.
type Inbound interface {
	Claim(line string) bool
	Unclaimed(line string)
}

// SessionDialer establishes a held connection to a target, routing its
// inbound lines through in (nil drops every line no exchange takes).
type SessionDialer interface {
	Dial(ctx context.Context, target string, timeout time.Duration, in Inbound) (HeldConn, error)
}

// StagedDialer is a dialer whose connections run the session's auth step
// between connecting and routing (#1049): Connect makes the connection,
// PendingConn.Authenticate runs the auth step over it, and only then does
// Open start routing lines, so nothing (a poll, a command, a listener's
// arm) can ride a connection that has not authenticated.
type StagedDialer interface {
	SessionDialer
	Connect(ctx context.Context, target string, timeout time.Duration) (PendingConn, error)
}

// PendingConn is a connection made and not yet routing.
type PendingConn interface {
	// Authenticate runs the auth step: the transport's own (ssh userauth)
	// and the driver's login dialog. A refusal is a TerminalError; anything
	// else that fails it is retryable.
	Authenticate(ctx context.Context, auth *SessionAuth, terminal Classifier, timeout time.Duration) error
	// Open starts routing: the connection becomes the session's HeldConn.
	Open(in Inbound, ch ChannelOptions) (HeldConn, error)
	Close() error
}

// SessionAuth is the session's auth step, declared by the driver or the
// transport. Its fields carry credentials: it is never printed.
type SessionAuth struct {
	// Login is the driver's login dialog, its sends rendered. Over ssh it
	// answers keyboard-interactive questions.
	Login *LoginDialog
	// SSH is the transport's own credential and host key pin (#1032).
	SSH *SSHAuth
}

// SSHAuth is an ssh session's credential and its host key pin.
type SSHAuth struct {
	Username   string
	Password   string
	PrivateKey string // PEM
	// Pin is the host key the session trusts; empty on first use, when the
	// first key offered is trusted and recorded.
	Pin *HostKeyPin
}

// HostKeyPin is a host key fingerprint (ssh.FingerprintSHA256) a session
// trusts, shared across its reconnects: trusted on first use, then held.
type HostKeyPin struct {
	mu sync.Mutex
	fp string
	// OnTrust hears a key the session trusted on first use, or the pinned
	// key matching on a connection that authenticated.
	OnTrust func(fingerprint string)
}

// NewHostKeyPin pins fp ("" trusts the first key offered).
func NewHostKeyPin(fp string) *HostKeyPin { return &HostKeyPin{fp: fp} }

// Get is the pinned fingerprint, "" before first use.
func (p *HostKeyPin) Get() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fp
}

// Trust records the key a connection authenticated with: on first use it
// becomes the pin (it never replaces one), and OnTrust hears it.
func (p *HostKeyPin) Trust(fp string) {
	p.mu.Lock()
	if p.fp == "" {
		p.fp = fp
	}
	on := p.OnTrust
	p.mu.Unlock()
	if on != nil {
		on(fp)
	}
}

// String never prints a credential.
func (a *SessionAuth) String() string { return "session auth" }

// ChannelOptions shape the line channel a connection opens: over ssh, a
// shell (with a PTY when asked) or an exec channel running Exec.
type ChannelOptions struct {
	PTY  bool
	Exec string
}

// AuthVerdict is the auth step's outcome, the endpoint-authenticated rung:
// Authenticated with the reason it passed, or refused with the device's
// text. Reported only when the step decided (a dropped or silent login
// decides nothing) and only when the verdict changes.
type AuthVerdict struct {
	Authenticated bool
	Reason        string
	At            time.Time
}

// SessionConfig is what a session needs: where to connect, how, and who hears
// its transitions. Backoff and Now default to the jittered curve and the wall
// clock; tests inject their own.
type SessionConfig struct {
	Target       string
	Timeout      time.Duration
	Dialer       SessionDialer
	OnTransition func(SessionTransition)
	Backoff      func(attempt int) time.Duration
	Now          func() time.Time
	// StableAfter is how long a connection must live to count as stable;
	// zero is SessionStableAfter. Tests shorten it.
	StableAfter time.Duration
	// Inbound routes the connection's inbound lines to the listeners (#817).
	Inbound Inbound
	// Arm holds the instantiation commands (a listener's subscribe
	// conversation), sent in order on every connection before the session
	// reads up: on establish and again on each recover. Plumbing, per the
	// #603 ruling: OnArm reports each with its reply for the node's logs,
	// and nothing records a command row.
	Arm   []string
	OnArm func(line, reply string, err error)
	// Auth is the session's auth step (#1049); nil runs none. Every attempt
	// runs it after connecting and before anything borrows the connection,
	// over a StagedDialer (a dialer that cannot run one rejects the session
	// as a protocol refusal).
	Auth *SessionAuth
	// OnAuth hears the auth step's verdict when it changes.
	OnAuth func(AuthVerdict)
	// Terminal classifies a device's line as a refusal retrying cannot fix
	// (the driver's terminal rules): during the login, and as the answer or
	// refusal of any exchange or arm. A match rejects the session.
	Terminal Classifier
	// Reprobe is how long a rejected session waits before trying once more
	// on its own; zero is never (the driver declares it, slow by design).
	Reprobe time.Duration
	// After is the clock's timer for the backoff and the re-probe; nil is
	// time.After. Tests inject a fake clock.
	After func(time.Duration) <-chan time.Time
	// Channel shapes the line channel each connection opens (ssh).
	Channel ChannelOptions
	// Rejected starts the session already rejected (a gate the node holds
	// for this connection fingerprint, from before a restart): it dials
	// nothing until its re-probe, if any.
	Rejected *TerminalError
}

// Session holds one endpoint's connection. Start it with StartSession, borrow
// it with Exchange, stop it with Close.
type Session struct {
	cfg    SessionConfig
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	state   SessionState
	reason  string
	conn    HeldConn      // the connection exchanges borrow, nil when none is held
	heard   chan struct{} // signalled by each answered exchange on conn
	pushed  chan struct{} // signalled by each line a listener claims on conn
	changed chan struct{} // closed and replaced on every transition
	closed  bool
	// dialing is true while the first dial is in flight: the one window an
	// exchange waits in, so the first poll after a start rides it.
	dialing bool
	// refusal is why the session is rejected, nil otherwise.
	refusal *TerminalError
	// retry wakes a rejected session for one more attempt (Retry);
	// retrying is set while that attempt is in flight, so a second retry
	// buys no second dial.
	retry    chan struct{}
	retrying bool
	// refused carries a terminal reply an exchange met on conn to the
	// machine, and inflight counts the exchanges on conn: both belong to
	// the connection, so a reply that arrives as the component hangs up is
	// still judged against the connection it ended.
	refused  chan *TerminalError
	inflight *sync.WaitGroup
	lastAuth string // the last auth verdict reported, so it reports changes only
	// loginDrops counts consecutive logins the component hung up on after
	// the final secret (machine goroutine only).
	loginDrops int
}

// LoginDropBudget is how many consecutive logins a component may hang up on
// after the final secret, with no refusal text, before the session reads
// rejected as an auth refusal (#1056 ruling): a component that drops a wrong
// password silently would otherwise be retried forever.
const LoginDropBudget = 3

// StartSession starts the machine: it reports establishing at once and
// dials in the background.
func StartSession(cfg SessionConfig) *Session {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultSessionTimeout
	}
	if cfg.Backoff == nil {
		cfg.Backoff = func(attempt int) time.Duration { return SessionBackoff(attempt, rand.Float64()) }
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.StableAfter <= 0 {
		cfg.StableAfter = SessionStableAfter
	}
	if cfg.After == nil {
		cfg.After = time.After
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		cfg: cfg, cancel: cancel, done: make(chan struct{}), changed: make(chan struct{}), dialing: true,
		retry: make(chan struct{}, 1),
	}
	go s.run(ctx)
	return s
}

// Retry wakes a rejected session for one more attempt (an operator's retry,
// or the endpoint's revision moving): it dials once, and reads up, rejected
// again, or, on a transient failure, follows the ordinary backoff and
// budget. A session that is not rejected is left alone, and Retry reports
// false.
func (s *Session) Retry() bool {
	s.mu.Lock()
	rejected := s.state == SessionRejected && !s.closed && !s.retrying
	s.mu.Unlock()
	if !rejected {
		return false
	}
	select {
	case s.retry <- struct{}{}:
	default:
	}
	return true
}

// State is the session's current state and the reason for it.
func (s *Session) State() (SessionState, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.reason
}

// Exchange borrows the held connection for one request and its answer.
// While the first dial is in flight it waits, on a budget of its own (the
// timeout), so the first poll after a start rides the connection being
// established; the exchange itself then gets its whole timeout and the
// caller's context, so a slow dial is never blamed on the device. With no
// connection held otherwise, the exchange fails at once naming the state and
// its reason: a down device must not stall the node's tick, and a poll never
// reconnects on its own (the session owns the connection). A down session
// lends the connection it is proving, so an answer can prove it.
func (s *Session) Exchange(ctx context.Context, line string, reply *driver.Reply, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = s.cfg.Timeout
	}
	var wait <-chan time.Time
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return "", ErrSessionClosed
		}
		conn, heard, state, reason, changed, dialing, refusal := s.conn, s.heard, s.state, s.reason, s.changed, s.dialing, s.refusal
		refusedCh, inflight := s.refused, s.inflight
		if conn != nil {
			inflight.Add(1) // under the lock: the machine waits on it only after taking conn back
		}
		s.mu.Unlock()
		if conn != nil {
			answer, err := conn.Exchange(ctx, line, reply, timeout)
			terr := s.terminalReply(answer, err, refusedCh)
			inflight.Done()
			if terr != nil {
				return "", fmt.Errorf("collection: session to %s: %w", s.cfg.Target, terr)
			}
			if err == nil {
				select {
				case heard <- struct{}{}:
				default:
				}
			}
			return answer, err
		}
		if refusal != nil {
			// Rejected: fail fast as the refusal itself, so a poll waits its
			// interval and a command fails with the reason, never a retry.
			return "", fmt.Errorf("collection: session to %s is %s: %w", s.cfg.Target, state, refusal)
		}
		if !dialing {
			return "", fmt.Errorf("collection: session to %s is %s: %s", s.cfg.Target, state, reason)
		}
		if wait == nil {
			t := time.NewTimer(timeout)
			defer t.Stop()
			wait = t.C
		}
		select {
		case <-changed:
		case <-wait:
			return "", fmt.Errorf("collection: session to %s is %s: %s", s.cfg.Target, state, reason)
		case <-ctx.Done():
			return "", fmt.Errorf("collection: session to %s: %w", s.cfg.Target, ctx.Err())
		}
	}
}

// terminalReply classifies an exchange's answer, or the device's refusal, by
// the driver's terminal rules. A match is handed to the machine, which
// rejects the session, and becomes the exchange's error.
func (s *Session) terminalReply(answer string, err error, refusedCh chan *TerminalError) *TerminalError {
	line := answer
	if err != nil {
		var refused *RefusedError
		if !errors.As(err, &refused) {
			return nil
		}
		line = refused.Line
	}
	terr := s.cfg.Terminal.classify(line)
	if terr == nil {
		return nil
	}
	select {
	case refusedCh <- terr:
	default:
	}
	return terr
}

// Close tears the session down: the machine stops, the held connection
// closes, and later exchanges refuse. It waits for the machine to exit and
// is idempotent. Teardown reports no transition: the node stopped wanting
// the session, which says nothing about the device.
func (s *Session) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.changed)
		s.changed = make(chan struct{})
	}
	s.mu.Unlock()
	s.cancel()
	<-s.done
}

// transition records a state change and reports it. Called only from the
// machine's goroutine, so reports arrive in order.
func (s *Session) transition(state SessionState, reason string, conn HeldConn) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.state, s.reason = state, reason
	s.retrying = false
	if state != SessionRejected {
		s.refusal = nil
	}
	if conn == nil {
		s.conn, s.heard, s.pushed = nil, nil, nil
	}
	if state == SessionUp {
		// The first dial's wait window ends with the connection already
		// lent, so a waiting exchange never sees the window closed first.
		s.dialing = false
	}
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	if s.cfg.OnTransition != nil {
		tr := SessionTransition{State: state, Reason: reason, At: s.cfg.Now().UTC()}
		if state == SessionRejected {
			tr.Refusal = s.refusal
		}
		s.cfg.OnTransition(tr)
	}
}

// provingInbound routes a connection's inbound lines to the configured
// listeners and lets a claimed line prove the connection, as an answered
// exchange does: with polls gated by their schedule (#822), a device that
// pushes must not wait a poll's interval to read up again.
type provingInbound struct{ s *Session }

func (p provingInbound) Claim(line string) bool {
	in := p.s.cfg.Inbound
	if in == nil || !in.Claim(line) {
		return false
	}
	p.s.mu.Lock()
	pushed := p.s.pushed
	p.s.mu.Unlock()
	if pushed != nil {
		select {
		case pushed <- struct{}{}:
		default:
		}
	}
	return true
}

func (p provingInbound) Unclaimed(line string) {
	if in := p.s.cfg.Inbound; in != nil {
		in.Unclaimed(line)
	}
}

// lend makes conn the connection exchanges borrow, with heard as its
// answered-exchange signal, pushed as its claimed-line signal, and refused
// and inflight as its terminal-reply signal and exchange count, without
// publishing anything.
func (s *Session) lend(conn HeldConn, heard, pushed chan struct{}, refused chan *TerminalError, inflight *sync.WaitGroup) {
	s.mu.Lock()
	s.conn, s.heard, s.pushed, s.refused, s.inflight = conn, heard, pushed, refused, inflight
	s.mu.Unlock()
}

// run is the machine. failures counts the consecutive failed attempts
// (failed dials, unanswered churned connections, answer timeouts); it is
// also the backoff attempt before the next dial, zero (an immediate redial)
// once forgiven. needAnswer holds while answer timeouts are what failed: a
// down session then leaves down only on an answer, never on age alone.
func (s *Session) run(ctx context.Context) {
	defer close(s.done)
	if s.cfg.Rejected == nil {
		s.transition(SessionEstablishing, "connecting to "+s.cfg.Target, nil)
	}
	state := SessionEstablishing
	failures, needAnswer := 0, false
	fail := func(cause string) {
		failures++
		switch {
		case failures >= SessionAttemptBudget:
			if state != SessionDown {
				state = SessionDown
				s.transition(state, fmt.Sprintf("%d consecutive attempts failed, last: %s", failures, cause), nil)
			}
		case state == SessionUp, state == SessionRejected:
			// From rejected, a retry's attempt met a transient failure: the
			// old refusal no longer says why, so the session leaves rejected.
			state = SessionRecovering
			s.transition(state, cause, nil)
		}
	}
	// reject moves the session to rejected and waits there: for an
	// operator's retry, the driver's re-probe, or the end. false is the end.
	reject := func(terr *TerminalError) bool {
		failures, needAnswer = 0, false
		state = SessionRejected
		s.mu.Lock()
		s.refusal = terr
		s.mu.Unlock()
		// A retry asked for before this rejection (during the attempt that
		// ended in it) is spent: it does not buy another dial.
		select {
		case <-s.retry:
		default:
		}
		s.transition(state, terr.Error(), nil)
		var reprobe <-chan time.Time
		if s.cfg.Reprobe > 0 {
			reprobe = s.cfg.After(s.cfg.Reprobe)
		}
		select {
		case <-ctx.Done():
			return false
		case <-s.retry:
		case <-reprobe:
		}
		s.mu.Lock()
		s.retrying = true
		s.mu.Unlock()
		return true
	}
	if s.cfg.Rejected != nil {
		// A gate held from before (a node restart): rejected at once, no dial.
		s.firstDialDone()
		if !reject(s.cfg.Rejected) {
			return
		}
	}
	for {
		if failures > 0 && !s.sleep(ctx, s.cfg.Backoff(failures)) {
			return
		}
		conn, err := s.connect(ctx)
		if ctx.Err() != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return
		}
		if err != nil {
			s.firstDialDone()
			if terr, ok := AsTerminal(err); ok {
				if !reject(terr) {
					return
				}
				continue
			}
			fail(err.Error())
			continue
		}
		// Arm before anything borrows the connection or it reads up, so the
		// listeners' subscriptions are in place whenever the session is up.
		// An arm the device does not answer is a failed attempt; one it
		// answers with a terminal reply rejects the session.
		if err := s.armConn(ctx, conn); err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return
			}
			s.firstDialDone()
			if terr, ok := AsTerminal(err); ok {
				if !reject(terr) {
					return
				}
				continue
			}
			if errors.Is(err, ErrExchangeTimeout) {
				needAnswer = true
			}
			fail("arming failed: " + err.Error())
			continue
		}
		heard, pushed := make(chan struct{}, 1), make(chan struct{}, 1)
		refusedCh, inflight := make(chan *TerminalError, 1), &sync.WaitGroup{}
		s.lend(conn, heard, pushed, refusedCh, inflight)
		since := s.cfg.Now()
		// A down session proves the connection before publishing up: an
		// answer or a listener's push always proves it, outliving the stable
		// window does unless answers were what failed.
		proving := state == SessionDown
		var stable <-chan time.Time
		var stableTimer *time.Timer
		if proving && !needAnswer {
			stableTimer = time.NewTimer(s.cfg.StableAfter)
			stable = stableTimer.C
		}
		promote := func(how string) {
			failures, needAnswer, proving = 0, false, false
			state = SessionUp
			s.transition(state, "connected to "+s.cfg.Target+how, conn)
		}
		if !proving {
			state = SessionUp
			s.transition(state, "connected to "+s.cfg.Target, conn)
		}
		answered := false
		var refused *TerminalError
	held:
		for {
			select {
			case <-ctx.Done():
				stopTimer(stableTimer)
				_ = conn.Close()
				return
			case refused = <-refusedCh:
				break held
			case <-heard:
				answered = true
				failures, needAnswer = 0, false
				if proving {
					promote("; it answered")
				}
			case <-pushed:
				// A push proves the device talks, so a down session reads up
				// (polls are gated by their schedule and may be minutes off).
				// It forgives nothing: a device that pushes and drops still
				// counts its churn, so it cannot hot-loop.
				if proving {
					proving = false
					state = SessionUp
					s.transition(state, "connected to "+s.cfg.Target+"; it pushed", conn)
				}
			case <-stable:
				stable = nil
				if proving {
					promote("; held past the stable window")
				}
			case <-conn.Done():
				break held
			}
		}
		stopTimer(stableTimer)
		cause := conn.Err()
		_ = conn.Close()
		s.lend(nil, nil, nil, nil, nil)
		// An exchange in flight when the connection died may yet classify a
		// terminal reply (a component that refuses and hangs up): wait for
		// them, which the closed connection makes prompt, then look.
		inflight.Wait()
		if refused == nil {
			select {
			case refused = <-refusedCh:
			default:
			}
		}
		if refused != nil {
			if !reject(refused) {
				return
			}
			continue
		}
		reason := "connection lost"
		if cause != nil {
			reason += ": " + cause.Error()
		}
		lived := s.cfg.Now().Sub(since)
		switch {
		case errors.Is(cause, ErrExchangeTimeout):
			needAnswer = true
			fail(reason)
		case lived < s.cfg.StableAfter && !answered:
			fail(fmt.Sprintf("%s (unanswered, within %s of connecting)", reason, s.cfg.StableAfter))
		default:
			failures = 0
			if state == SessionUp {
				state = SessionRecovering
				s.transition(state, reason, nil)
			}
		}
	}
}

// connect makes one attempt's connection: dial, then the auth step, then
// routing. Over a StagedDialer the auth step runs between connecting and
// opening, so nothing rides the connection before it passes; its verdict is
// reported as the endpoint-authenticated rung. A session declaring an auth
// step over a dialer that cannot run one is a protocol refusal.
func (s *Session) connect(ctx context.Context) (HeldConn, error) {
	staged, ok := s.cfg.Dialer.(StagedDialer)
	if !ok {
		if s.cfg.Auth != nil {
			return nil, &TerminalError{Class: driver.ClassProtocol, Reason: "the transport cannot run the declared auth step"}
		}
		return s.cfg.Dialer.Dial(ctx, s.cfg.Target, s.cfg.Timeout, provingInbound{s})
	}
	pc, err := staged.Connect(ctx, s.cfg.Target, s.cfg.Timeout)
	if err != nil {
		return nil, err
	}
	if s.cfg.Auth != nil {
		if err := pc.Authenticate(ctx, s.cfg.Auth, s.cfg.Terminal, s.cfg.Timeout); err != nil {
			_ = pc.Close()
			if errors.Is(err, ErrLoginDropped) {
				s.loginDrops++
				if s.loginDrops >= LoginDropBudget {
					// Spent: the driver's re-probe, on the same session, re-
					// rejects at once if it drops again. An operator's :retry
					// moves the fingerprint, so it is a fresh session with a
					// fresh budget.
					s.loginDrops = LoginDropBudget - 1
					err = &TerminalError{Class: driver.ClassAuth, Reason: fmt.Sprintf(
						"the component hung up after the login %d times running, with no refusal text", LoginDropBudget)}
				}
			}
			if terr, ok := AsTerminal(err); ok && terr.Class == driver.ClassAuth {
				s.reportAuth(false, terr.Error())
			}
			return nil, err
		}
		s.loginDrops = 0
		s.reportAuth(true, "the auth step passed on "+s.cfg.Target)
	}
	conn, err := pc.Open(provingInbound{s}, s.cfg.Channel)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	return conn, nil
}

// reportAuth reports the auth verdict when it changes. Called only from the
// machine's goroutine.
func (s *Session) reportAuth(ok bool, reason string) {
	v := "no"
	if ok {
		v = "yes"
	}
	if v == s.lastAuth {
		return
	}
	s.lastAuth = v
	if s.cfg.OnAuth != nil {
		s.cfg.OnAuth(AuthVerdict{Authenticated: ok, Reason: reason, At: s.cfg.Now().UTC()})
	}
}

// sleep waits d on the session's clock, or until ctx ends; false means ctx
// ended.
func (s *Session) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	select {
	case <-ctx.Done():
		return false
	case <-s.cfg.After(d):
		return true
	}
}

// ErrUnacknowledged is reported for an arm command the device sent nothing
// back for within the timeout: armed, unacknowledged. Not a failure, since
// many devices subscribe silently; only the connection dying fails the arm.
var ErrUnacknowledged = errors.New("armed, unacknowledged")

// armConn sends the instantiation commands over a fresh connection, in
// order, reporting each. The ack is the first line of any kind: one a
// listener claims counts (the listener has it too), since a device may
// acknowledge a subscribe with the push it subscribes to.
func (s *Session) armConn(ctx context.Context, conn HeldConn) error {
	for _, line := range s.cfg.Arm {
		reply, err := conn.Exchange(ctx, line, driver.AnyLine(), s.cfg.Timeout)
		if terr := s.cfg.Terminal.classify(reply); err == nil && terr != nil {
			if s.cfg.OnArm != nil {
				s.cfg.OnArm(line, reply, terr)
			}
			return terr
		}
		if err != nil && errors.Is(err, ErrNoAnswer) && !isDone(conn) {
			err = ErrUnacknowledged
		}
		if s.cfg.OnArm != nil {
			s.cfg.OnArm(line, reply, err)
		}
		if err != nil && !errors.Is(err, ErrUnacknowledged) {
			return fmt.Errorf("%s: %w", line, err)
		}
	}
	return nil
}

func isDone(conn HeldConn) bool {
	select {
	case <-conn.Done():
		return true
	default:
		return false
	}
}

// firstDialDone ends the first-dial wait window after a failed first dial
// and wakes its waiters; a successful one ends it inside the up transition.
func (s *Session) firstDialDone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dialing || s.closed {
		s.dialing = false
		return
	}
	s.dialing = false
	close(s.changed)
	s.changed = make(chan struct{})
}

func stopTimer(t *time.Timer) {
	if t != nil {
		t.Stop()
	}
}
