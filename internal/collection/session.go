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

// SignalSessionState is the property the session's transitions land under.
const SignalSessionState = probekeys.SessionState

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
	changed chan struct{} // closed and replaced on every transition
	closed  bool
	// dialing is true while the first dial is in flight: the one window an
	// exchange waits in, so the first poll after a start rides it.
	dialing bool
}

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
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{cfg: cfg, cancel: cancel, done: make(chan struct{}), changed: make(chan struct{}), dialing: true}
	go s.run(ctx)
	return s
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
		conn, heard, state, reason, changed, dialing := s.conn, s.heard, s.state, s.reason, s.changed, s.dialing
		s.mu.Unlock()
		if conn != nil {
			answer, err := conn.Exchange(ctx, line, reply, timeout)
			if err == nil {
				select {
				case heard <- struct{}{}:
				default:
				}
			}
			return answer, err
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
	if conn == nil {
		s.conn, s.heard = nil, nil
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
		s.cfg.OnTransition(SessionTransition{State: state, Reason: reason, At: s.cfg.Now().UTC()})
	}
}

// lend makes conn the connection exchanges borrow, with heard as its
// answered-exchange signal, without publishing anything.
func (s *Session) lend(conn HeldConn, heard chan struct{}) {
	s.mu.Lock()
	s.conn, s.heard = conn, heard
	s.mu.Unlock()
}

// run is the machine. failures counts the consecutive failed attempts
// (failed dials, unanswered churned connections, answer timeouts); it is
// also the backoff attempt before the next dial, zero (an immediate redial)
// once forgiven. needAnswer holds while answer timeouts are what failed: a
// down session then leaves down only on an answer, never on age alone.
func (s *Session) run(ctx context.Context) {
	defer close(s.done)
	s.transition(SessionEstablishing, "connecting to "+s.cfg.Target, nil)
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
		case state == SessionUp:
			state = SessionRecovering
			s.transition(state, cause, nil)
		}
	}
	for {
		if failures > 0 && !sleepCtx(ctx, s.cfg.Backoff(failures)) {
			return
		}
		conn, err := s.cfg.Dialer.Dial(ctx, s.cfg.Target, s.cfg.Timeout, s.cfg.Inbound)
		if ctx.Err() != nil {
			if conn != nil {
				_ = conn.Close()
			}
			return
		}
		if err != nil {
			s.firstDialDone()
			fail(err.Error())
			continue
		}
		// Arm before anything borrows the connection or it reads up, so the
		// listeners' subscriptions are in place whenever the session is up.
		// An arm the device does not answer is a failed attempt.
		if err := s.armConn(ctx, conn); err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, ErrExchangeTimeout) {
				needAnswer = true
			}
			s.firstDialDone()
			fail("arming failed: " + err.Error())
			continue
		}
		heard := make(chan struct{}, 1)
		s.lend(conn, heard)
		since := s.cfg.Now()
		// A down session proves the connection before publishing up: an
		// answer always proves it, outliving the stable window does unless
		// answers were what failed.
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
	held:
		for {
			select {
			case <-ctx.Done():
				stopTimer(stableTimer)
				_ = conn.Close()
				return
			case <-heard:
				answered = true
				failures, needAnswer = 0, false
				if proving {
					promote("; it answered")
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
		s.lend(nil, nil)
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

// sleepCtx waits d or until ctx ends; false means ctx ended.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
