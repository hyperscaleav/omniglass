package collection_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The session state machine (#816) over a faked held connection. The fake is
// the necessary half of the capability carve-out; the real line session
// against the simulator's listener (internal/sim) closes the increment.

// fakeHeldConn answers every exchange by echoing the line, counts the
// exchanges it served, and dies when the test kills it.
type fakeHeldConn struct {
	mu        sync.Mutex
	exchanges int
	done      chan struct{}
	err       error
	closed    bool
	once      sync.Once
}

func newFakeHeldConn() *fakeHeldConn { return &fakeHeldConn{done: make(chan struct{})} }

func (c *fakeHeldConn) Exchange(ctx context.Context, line string, _ time.Duration) (string, error) {
	select {
	case <-c.done:
		return "", errors.New("fake: connection is dead")
	default:
	}
	c.mu.Lock()
	c.exchanges++
	c.mu.Unlock()
	return "echo " + line, nil
}

func (c *fakeHeldConn) Done() <-chan struct{} { return c.done }

func (c *fakeHeldConn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *fakeHeldConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.kill(errors.New("closed"))
	return nil
}

func (c *fakeHeldConn) kill(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *fakeHeldConn) served() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exchanges
}

func (c *fakeHeldConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// fakeSessionDialer hands out fresh fake connections, or refuses while the
// test says the device is gone. dieAtOnce makes every connection die the
// moment it is made (a device that accepts and drops).
type fakeSessionDialer struct {
	mu        sync.Mutex
	refuse    bool
	dieAtOnce bool
	conns     []*fakeHeldConn
	dials     int
	gate      chan struct{} // non-nil: the first dial waits for it to close
}

func (d *fakeSessionDialer) Dial(ctx context.Context, target string, _ time.Duration) (collection.HeldConn, error) {
	d.mu.Lock()
	gate := d.gate
	d.gate = nil
	d.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dials++
	if d.refuse {
		return nil, errors.New("connection refused")
	}
	c := newFakeHeldConn()
	if d.dieAtOnce {
		c.kill(errors.New("EOF"))
	}
	d.conns = append(d.conns, c)
	return c, nil
}

func (d *fakeSessionDialer) setRefuse(v bool) {
	d.mu.Lock()
	d.refuse = v
	d.mu.Unlock()
}

func (d *fakeSessionDialer) last() *fakeHeldConn {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.conns) == 0 {
		return nil
	}
	return d.conns[len(d.conns)-1]
}

func (d *fakeSessionDialer) dialCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dials
}

// recorder captures the transitions in order and the backoff attempts asked
// for, so a test reads the machine's story rather than sleeping on it.
type recorder struct {
	mu       sync.Mutex
	seen     []collection.SessionTransition
	attempts []int
	notify   chan struct{}
}

func newRecorder() *recorder { return &recorder{notify: make(chan struct{}, 1024)} }

func (r *recorder) onTransition(tr collection.SessionTransition) {
	r.mu.Lock()
	r.seen = append(r.seen, tr)
	r.mu.Unlock()
	r.notify <- struct{}{}
}

func (r *recorder) backoff(attempt int) time.Duration {
	r.mu.Lock()
	r.attempts = append(r.attempts, attempt)
	r.mu.Unlock()
	return time.Millisecond
}

func (r *recorder) states() []collection.SessionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]collection.SessionState, 0, len(r.seen))
	for _, tr := range r.seen {
		out = append(out, tr.State)
	}
	return out
}

func (r *recorder) lastTransition() collection.SessionTransition {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[len(r.seen)-1]
}

// waitStates blocks until the recorded state sequence equals want.
func (r *recorder) waitStates(t *testing.T, want ...collection.SessionState) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		got := r.states()
		if equalStates(got, want) {
			return
		}
		if len(got) > len(want) {
			t.Fatalf("transitions = %v, want %v", got, want)
		}
		select {
		case <-r.notify:
		case <-deadline:
			t.Fatalf("transitions = %v, want %v", got, want)
		}
	}
}

func equalStates(a, b []collection.SessionState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func startTestSession(t *testing.T, d *fakeSessionDialer, r *recorder) *collection.Session {
	return startTestSessionAt(t, d, r, nil)
}

// steppingClock returns a clock that moves step forward on every read, so
// every held connection outlives (or not) the stable window by design.
func steppingClock(step time.Duration) func() time.Time {
	var mu sync.Mutex
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		at = at.Add(step)
		return at
	}
}

func startTestSessionAt(t *testing.T, d *fakeSessionDialer, r *recorder, now func() time.Time) *collection.Session {
	t.Helper()
	s := collection.StartSession(collection.SessionConfig{
		Target:       "10.0.0.9:51325",
		Dialer:       d,
		OnTransition: r.onTransition,
		Backoff:      r.backoff,
		Now:          now,
	})
	t.Cleanup(s.Close)
	return s
}

const (
	establishing = collection.SessionEstablishing
	up           = collection.SessionUp
	recovering   = collection.SessionRecovering
	down         = collection.SessionDown
)

func TestSessionEstablishesAndReportsUp(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	s := startTestSession(t, d, r)
	r.waitStates(t, establishing, up)
	if st, _ := s.State(); st != up {
		t.Fatalf("State() = %s, want up", st)
	}
	if r.lastTransition().Reason == "" {
		t.Error("the up transition carries no reason")
	}
}

// A poll over a held session borrows the one connection: three exchanges,
// one dial.
func TestSessionExchangesReuseTheHeldConnection(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	s := startTestSession(t, d, r)
	r.waitStates(t, establishing, up)
	for i := 0; i < 3; i++ {
		got, err := s.Exchange(context.Background(), "GET INPUT", time.Second)
		if err != nil {
			t.Fatalf("exchange %d: %v", i, err)
		}
		if got != "echo GET INPUT" {
			t.Fatalf("exchange %d = %q", i, got)
		}
	}
	if n := d.dialCount(); n != 1 {
		t.Fatalf("dialed %d times for three polls, want 1 (the held connection)", n)
	}
	if n := d.last().served(); n != 3 {
		t.Fatalf("the held connection served %d exchanges, want 3", n)
	}
}

// A held connection that dies reads recovering, with the cause as its reason,
// and then up again on a fresh connection.
func TestSessionRecoversWhenTheConnectionDies(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	s := startTestSession(t, d, r)
	r.waitStates(t, establishing, up)
	first := d.last()
	first.kill(errors.New("connection reset by peer"))
	r.waitStates(t, establishing, up, recovering, up)
	r.mu.Lock()
	reason := r.seen[2].Reason
	r.mu.Unlock()
	if !strings.Contains(reason, "connection reset by peer") {
		t.Errorf("recovering reason = %q, want the cause", reason)
	}
	if d.last() == first {
		t.Fatal("recovered onto the dead connection")
	}
	if got, err := s.Exchange(context.Background(), "GET INPUT", time.Second); err != nil || got != "echo GET INPUT" {
		t.Fatalf("exchange after recovery = %q, %v", got, err)
	}
}

// Recovery is bounded: consecutive failed attempts back off with growing
// attempt numbers, and past the attempt budget the session reads down with
// the reason; it keeps retrying at the capped pace and reads up again when
// the device returns.
func TestSessionRecoveryIsBoundedThenDownThenUp(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	startTestSession(t, d, r)
	r.waitStates(t, establishing, up)
	d.setRefuse(true)
	d.last().kill(errors.New("EOF"))
	r.waitStates(t, establishing, up, recovering, down)
	down := r.lastTransition()
	if !strings.Contains(down.Reason, "connection refused") {
		t.Errorf("down reason = %q, want the last cause", down.Reason)
	}
	r.mu.Lock()
	attempts := append([]int(nil), r.attempts...)
	r.mu.Unlock()
	if len(attempts) < collection.SessionAttemptBudget-1 {
		t.Fatalf("backoff asked %v, want one per failed attempt", attempts)
	}
	for i := 1; i < len(attempts); i++ {
		if attempts[i] < attempts[i-1] {
			t.Fatalf("backoff attempts %v shrink while failing", attempts)
		}
	}
	d.setRefuse(false)
	r.waitStates(t, establishing, up, recovering, collection.SessionDown, up)
}

// A device never reached reads establishing then down; it never claims to
// have been up, so it never reads recovering.
func TestSessionThatNeverConnectsReadsDownNotRecovering(t *testing.T) {
	d, r := &fakeSessionDialer{refuse: true}, newRecorder()
	startTestSession(t, d, r)
	r.waitStates(t, establishing, down)
	if n := d.dialCount(); n < collection.SessionAttemptBudget {
		t.Fatalf("down after %d attempts, want the budget of %d", n, collection.SessionAttemptBudget)
	}
}

// A device that accepts and drops at once does not hot-loop: each short-lived
// connection grows the backoff.
func TestSessionFlappingConnectionBacksOff(t *testing.T) {
	d, r := &fakeSessionDialer{dieAtOnce: true}, newRecorder()
	startTestSession(t, d, r)
	deadline := time.After(3 * time.Second)
	for {
		r.mu.Lock()
		attempts := append([]int(nil), r.attempts...)
		r.mu.Unlock()
		if len(attempts) >= 3 {
			if attempts[0] < 1 || attempts[2] <= attempts[0] {
				t.Fatalf("flapping backoff attempts = %v, want growing", attempts)
			}
			return
		}
		select {
		case <-r.notify:
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("flapping connection asked backoff %v; it is hot-looping or stalled", attempts)
		}
	}
}

// Review finding 3: a device that accepts and drops at once reaches down
// within the attempt budget (each churned connection is a failed attempt),
// and once down a churned connection reads down again, never recovering.
func TestSessionAcceptThenDropReadsDownWithinTheBudget(t *testing.T) {
	d, r := &fakeSessionDialer{dieAtOnce: true}, newRecorder()
	startTestSession(t, d, r)
	deadline := time.After(3 * time.Second)
	for {
		st := r.states()
		if i := indexOf(st, down); i >= 0 {
			if i+1 > 2*collection.SessionAttemptBudget+1 {
				t.Fatalf("down only after %d transitions %v, want within the budget", i+1, st)
			}
			break
		}
		select {
		case <-r.notify:
		case <-deadline:
			t.Fatalf("an accept-then-drop device never read down: %v", r.states())
		}
	}
	// A few more churned connections after down.
	for len(r.states()) < 2*collection.SessionAttemptBudget+6 {
		select {
		case <-r.notify:
		case <-time.After(3 * time.Second):
			t.Fatal("the machine stalled after down")
		}
	}
	st := r.states()
	for _, s := range st[indexOf(st, down):] {
		if s == recovering {
			t.Fatalf("a churned connection after down read recovering: %v", st)
		}
	}
	r.mu.Lock()
	last := r.attempts[len(r.attempts)-1]
	r.mu.Unlock()
	if last < collection.SessionAttemptBudget {
		t.Fatalf("backoff attempt after down = %d, want at least the budget (the waits stay at the curve)", last)
	}
}

// A stable connection's drop is not a failure: it redials at once and the
// session never reads down however often it happens.
func TestSessionStableDropsNeverReadDown(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	startTestSessionAt(t, d, r, steppingClock(collection.SessionStableAfter+time.Second))
	r.waitStates(t, establishing, up)
	for i := 0; i < 2*collection.SessionAttemptBudget; i++ {
		d.last().kill(errors.New("EOF"))
		want := len(r.states()) + 2
		for len(r.states()) < want {
			select {
			case <-r.notify:
			case <-time.After(3 * time.Second):
				t.Fatalf("no recovery after drop %d: %v", i, r.states())
			}
		}
	}
	if indexOf(r.states(), down) >= 0 {
		t.Fatalf("stable drops read down: %v", r.states())
	}
}

// Review finding 1 at the machine: an exchange timeout is a failure however
// long the connection lived, so a hung device reaches down.
func TestSessionExchangeTimeoutsReadDown(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	startTestSessionAt(t, d, r, steppingClock(collection.SessionStableAfter+time.Second))
	r.waitStates(t, establishing, up)
	deadline := time.After(3 * time.Second)
	for indexOf(r.states(), down) < 0 {
		if c := d.last(); c != nil {
			c.kill(collection.ErrExchangeTimeout)
		}
		select {
		case <-r.notify:
		case <-time.After(5 * time.Millisecond):
		case <-deadline:
			t.Fatalf("repeated exchange timeouts never read down: %v", r.states())
		}
	}
}

func indexOf(st []collection.SessionState, want collection.SessionState) int {
	for i, s := range st {
		if s == want {
			return i
		}
	}
	return -1
}

// Teardown closes the held connection, stops the machine (no transition after
// Close), and refuses later exchanges.
func TestSessionCloseTearsDown(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	s := startTestSession(t, d, r)
	r.waitStates(t, establishing, up)
	conn := d.last()
	s.Close()
	if !conn.isClosed() {
		t.Fatal("teardown left the held connection open")
	}
	before := len(r.states())
	time.Sleep(20 * time.Millisecond)
	if after := len(r.states()); after != before {
		t.Fatalf("transitions after teardown: %v", r.states())
	}
	if n := d.dialCount(); n != 1 {
		t.Fatalf("redialed after teardown (%d dials)", n)
	}
	if _, err := s.Exchange(context.Background(), "GET INPUT", 50*time.Millisecond); err == nil {
		t.Fatal("an exchange over a torn-down session succeeded")
	}
	s.Close() // idempotent
}

// A poll over a session that is not up fails at once naming the state: a
// down device must not stall the node's tick for the poll's timeout.
func TestSessionExchangeFailsFastNamingTheStateWhenNotUp(t *testing.T) {
	d, r := &fakeSessionDialer{refuse: true}, newRecorder()
	s := startTestSession(t, d, r)
	deadline := time.Now().Add(3 * time.Second)
	for d.dialCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	_, err := s.Exchange(context.Background(), "GET INPUT", 2*time.Second)
	if err == nil {
		t.Fatal("an exchange over an unconnected session succeeded")
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("a poll over a session that is not up waited %v; it must fail fast", took)
	}
	if !strings.Contains(err.Error(), string(establishing)) && !strings.Contains(err.Error(), string(down)) {
		t.Fatalf("error = %v, want the session state named", err)
	}
}

// An exchange issued while the first dial is still in flight waits for it,
// so the first poll after a start rides the connection being established.
func TestSessionExchangeWaitsForTheFirstDial(t *testing.T) {
	gate := make(chan struct{})
	d, r := &fakeSessionDialer{gate: gate}, newRecorder()
	s := startTestSession(t, d, r)
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(gate)
	}()
	got, err := s.Exchange(context.Background(), "GET INPUT", 2*time.Second)
	if err != nil || got != "echo GET INPUT" {
		t.Fatalf("exchange during the first dial = %q, %v", got, err)
	}
}

// The backoff is jittered around an exponential curve and bounded by the cap.
func TestSessionBackoffIsJitteredAndCapped(t *testing.T) {
	base := collection.SessionBackoffBase
	if got := collection.SessionBackoff(1, 0.5); got != base {
		t.Errorf("SessionBackoff(1, mid) = %v, want the base %v", got, base)
	}
	if got := collection.SessionBackoff(2, 0.5); got != 2*base {
		t.Errorf("SessionBackoff(2, mid) = %v, want %v", got, 2*base)
	}
	lo, hi := collection.SessionBackoff(3, 0), collection.SessionBackoff(3, 0.999)
	if lo >= 4*base || hi <= 4*base {
		t.Errorf("jitter at attempt 3 spans [%v, %v], want around %v", lo, hi, 4*base)
	}
	for _, unit := range []float64{0, 0.5, 0.999} {
		if got := collection.SessionBackoff(64, unit); got > collection.SessionBackoffMax {
			t.Errorf("SessionBackoff(64, %v) = %v, past the cap %v", unit, got, collection.SessionBackoffMax)
		}
	}
	if got := collection.SessionBackoff(0, 0.5); got != 0 {
		t.Errorf("SessionBackoff(0) = %v, want no wait", got)
	}
}
