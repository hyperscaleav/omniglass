package collection_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// Auth is a layer that gates the session, and a terminal refusal rejects it
// (#1049). The machine over fakes: a staged dialer whose auth step the test
// scripts, and a fake clock, so "no dial over an hour" is an hour of the
// session's own clock rather than of the test's wall time.

const rejected = collection.SessionRejected

// fakeClock is the session's timer source: After registers a timer, Advance
// moves the clock and fires every timer it passes.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, fakeTimer{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock by d in steps, firing each timer it passes and
// giving the machine a moment to act on it before the next step.
func (c *fakeClock) Advance(d, step time.Duration) {
	for moved := time.Duration(0); moved < d; moved += step {
		c.mu.Lock()
		c.now = c.now.Add(step)
		var keep []fakeTimer
		for _, t := range c.timers {
			if !t.at.After(c.now) {
				t.ch <- c.now
				continue
			}
			keep = append(keep, t)
		}
		c.timers = keep
		c.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
}

// terminalDialer refuses every dial with a TerminalError until told to accept.
type terminalDialer struct {
	fakeSessionDialer
	tmu     sync.Mutex
	refusal *collection.TerminalError
}

func (d *terminalDialer) Dial(ctx context.Context, target string, timeout time.Duration, in collection.Inbound) (collection.HeldConn, error) {
	d.tmu.Lock()
	refusal := d.refusal
	d.tmu.Unlock()
	if refusal != nil {
		d.mu.Lock()
		d.dials++
		d.mu.Unlock()
		return nil, refusal
	}
	return d.fakeSessionDialer.Dial(ctx, target, timeout, in)
}

func (d *terminalDialer) accept() {
	d.tmu.Lock()
	d.refusal = nil
	d.tmu.Unlock()
}

func authRefusal() *collection.TerminalError {
	return &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"}
}

func startRejectingSession(t *testing.T, d collection.SessionDialer, r *recorder, clock *fakeClock, reprobe time.Duration) *collection.Session {
	t.Helper()
	s := collection.StartSession(collection.SessionConfig{
		Target: "10.0.0.9:23", Dialer: d, OnTransition: r.onTransition, Backoff: r.backoff,
		After: clock.After, Now: clock.Now, Reprobe: reprobe,
	})
	t.Cleanup(s.Close)
	return s
}

// A wrong credential makes exactly one attempt, reads rejected with the
// device's text, and dials no more over an hour of the session's clock.
func TestSessionTerminalRefusalRejectsAndStopsDialing(t *testing.T) {
	d := &terminalDialer{refusal: authRefusal()}
	r, clock := newRecorder(), newFakeClock()
	s := startRejectingSession(t, d, r, clock, 0)
	r.waitStates(t, establishing, rejected)
	if got := r.lastTransition().Reason; got != "auth: auth rejected" {
		t.Fatalf("rejected reason = %q, want auth: auth rejected", got)
	}
	clock.Advance(time.Hour, 30*time.Second)
	if n := d.dialCount(); n != 1 {
		t.Fatalf("dialed %d times over an hour of rejection, want exactly 1", n)
	}
	if got := r.states(); len(got) != 2 {
		t.Fatalf("transitions over the hour = %v, want only establishing, rejected", got)
	}
	// Polls and commands fail fast with the reason, as a terminal error.
	_, err := s.Exchange(context.Background(), "GET INPUT", nil, time.Second)
	if terr, ok := collection.AsTerminal(err); !ok || terr.Class != driver.ClassAuth {
		t.Fatalf("an exchange over a rejected session = %v, want the auth refusal", err)
	}
}

// An operator's retry redials exactly once; still refused, the session reads
// rejected again and waits.
func TestSessionRetryRedialsOnce(t *testing.T) {
	d := &terminalDialer{refusal: authRefusal()}
	r, clock := newRecorder(), newFakeClock()
	s := startRejectingSession(t, d, r, clock, 0)
	r.waitStates(t, establishing, rejected)
	if !s.Retry() {
		t.Fatal("Retry on a rejected session reported nothing to retry")
	}
	r.waitStates(t, establishing, rejected, rejected)
	clock.Advance(time.Hour, time.Minute)
	if n := d.dialCount(); n != 2 {
		t.Fatalf("dialed %d times, want 2 (the first and the one retry)", n)
	}
}

// The retry after the credential is fixed reads up.
func TestSessionRetryAfterTheFixReadsUp(t *testing.T) {
	d := &terminalDialer{refusal: authRefusal()}
	r, clock := newRecorder(), newFakeClock()
	s := startRejectingSession(t, d, r, clock, 0)
	r.waitStates(t, establishing, rejected)
	d.accept()
	s.Retry()
	r.waitStates(t, establishing, rejected, up)
	if got, err := s.Exchange(context.Background(), "GET INPUT", nil, time.Second); err != nil || got != "echo GET INPUT" {
		t.Fatalf("exchange after the retry = %q, %v", got, err)
	}
}

// Retry on a session that is not rejected is a no-op: a healthy connection
// is never recycled by it.
func TestSessionRetryOnAHealthySessionDoesNothing(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	s := startTestSession(t, d, r)
	r.waitStates(t, establishing, up)
	if s.Retry() {
		t.Fatal("Retry on an up session reported a retry")
	}
	time.Sleep(20 * time.Millisecond)
	if n := d.dialCount(); n != 1 {
		t.Fatalf("dialed %d times, want 1", n)
	}
}

// A driver's slow re-probe tries once per interval, and only then.
func TestSessionReprobesAtTheDeclaredInterval(t *testing.T) {
	d := &terminalDialer{refusal: authRefusal()}
	r, clock := newRecorder(), newFakeClock()
	startRejectingSession(t, d, r, clock, 20*time.Minute)
	r.waitStates(t, establishing, rejected)
	// A minute past the hour, so the third re-probe's timer (booked a step
	// late at worst) has come due.
	clock.Advance(time.Hour+time.Minute, 30*time.Second)
	if n := d.dialCount(); n != 4 {
		t.Fatalf("dialed %d times over an hour at a 20m re-probe, want 4", n)
	}
}

// A transient failure is unchanged by the terminal machinery: it spends the
// budget and reads down.
func TestSessionTransientFailureStillSpendsTheBudget(t *testing.T) {
	d, r := &fakeSessionDialer{refuse: true}, newRecorder()
	startTestSession(t, d, r)
	r.waitStates(t, establishing, down)
	if !strings.Contains(r.lastTransition().Reason, "consecutive attempts failed") {
		t.Fatalf("down reason = %q", r.lastTransition().Reason)
	}
}

// stagedDialer runs a scripted auth step between connect and open, logging
// the order every stage ran in.
type stagedDialer struct {
	fakeSessionDialer
	smu    sync.Mutex
	order  []string
	authOK chan error // each Authenticate takes its verdict from here
}

func (d *stagedDialer) log(stage string) {
	d.smu.Lock()
	d.order = append(d.order, stage)
	d.smu.Unlock()
}

func (d *stagedDialer) stages() []string {
	d.smu.Lock()
	defer d.smu.Unlock()
	return append([]string(nil), d.order...)
}

func (d *stagedDialer) Connect(ctx context.Context, target string, timeout time.Duration) (collection.PendingConn, error) {
	d.log("connect")
	return &stagedPending{d: d, target: target, timeout: timeout}, nil
}

type stagedPending struct {
	d       *stagedDialer
	target  string
	timeout time.Duration
}

func (p *stagedPending) Authenticate(ctx context.Context, auth *collection.SessionAuth, _ collection.Classifier, _ time.Duration) error {
	p.d.log("authenticate")
	select {
	case err := <-p.d.authOK:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *stagedPending) Open(in collection.Inbound, _ collection.ChannelOptions) (collection.HeldConn, error) {
	p.d.log("open")
	c, err := p.d.fakeSessionDialer.Dial(context.Background(), p.target, p.timeout, in)
	if err != nil {
		return nil, err
	}
	fc := c.(*fakeHeldConn)
	return &loggingConn{fakeHeldConn: fc, log: p.d.log}, nil
}

func (p *stagedPending) Close() error { p.d.log("close"); return nil }

type loggingConn struct {
	*fakeHeldConn
	log func(string)
}

func (c *loggingConn) Exchange(ctx context.Context, line string, reply *driver.Reply, timeout time.Duration) (string, error) {
	c.log("exchange " + line)
	return c.fakeHeldConn.Exchange(ctx, line, reply, timeout)
}

type authRecorder struct {
	mu   sync.Mutex
	seen []collection.AuthVerdict
}

func (a *authRecorder) on(v collection.AuthVerdict) {
	a.mu.Lock()
	a.seen = append(a.seen, v)
	a.mu.Unlock()
}

func (a *authRecorder) verdicts() []collection.AuthVerdict {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]collection.AuthVerdict(nil), a.seen...)
}

// Nothing borrows the connection, and no listener arms, until the auth step
// passes; the passed step reads authenticated.
func TestSessionAuthGatesArmAndExchanges(t *testing.T) {
	d := &stagedDialer{authOK: make(chan error, 1)}
	r, ar := newRecorder(), &authRecorder{}
	s := collection.StartSession(collection.SessionConfig{
		Target: "10.0.0.9:23", Dialer: d, OnTransition: r.onTransition, Backoff: r.backoff,
		Auth: &collection.SessionAuth{Login: panelDialog()}, Arm: []string{"SUBSCRIBE INPUT"}, OnAuth: ar.on,
	})
	t.Cleanup(s.Close)
	r.waitStates(t, establishing)
	time.Sleep(20 * time.Millisecond)
	if got := d.stages(); strings.Join(got, ",") != "connect,authenticate" {
		t.Fatalf("before the auth step passed the session ran %v", got)
	}
	if _, err := s.Exchange(context.Background(), "GET INPUT", nil, 10*time.Millisecond); err == nil {
		t.Fatal("an exchange rode the session before its auth step passed")
	}
	d.authOK <- nil
	r.waitStates(t, establishing, up)
	want := "connect,authenticate,open,exchange SUBSCRIBE INPUT"
	if got := strings.Join(d.stages(), ","); got != want {
		t.Fatalf("stages = %s, want %s", got, want)
	}
	if v := ar.verdicts(); len(v) != 1 || !v[0].Authenticated {
		t.Fatalf("auth verdicts = %+v, want one authenticated", v)
	}
}

// A refused auth step reads not authenticated and rejects, without opening
// the connection to anything.
func TestSessionAuthRefusalRejects(t *testing.T) {
	d := &stagedDialer{authOK: make(chan error, 1)}
	d.authOK <- authRefusal()
	r, ar := newRecorder(), &authRecorder{}
	s := collection.StartSession(collection.SessionConfig{
		Target: "10.0.0.9:23", Dialer: d, OnTransition: r.onTransition, Backoff: r.backoff,
		Auth: &collection.SessionAuth{Login: panelDialog()}, OnAuth: ar.on,
	})
	t.Cleanup(s.Close)
	r.waitStates(t, establishing, rejected)
	if got := strings.Join(d.stages(), ","); got != "connect,authenticate,close" {
		t.Fatalf("stages = %s, want the auth step refused and closed", got)
	}
	v := ar.verdicts()
	if len(v) != 1 || v[0].Authenticated || v[0].Reason != "auth: auth rejected" {
		t.Fatalf("auth verdicts = %+v, want one refusal naming the device text", v)
	}
}

// A retryable auth failure (the device never prompted) is an ordinary failed
// attempt: no verdict, and the budget is spent.
func TestSessionAuthTimeoutIsRetryable(t *testing.T) {
	d := &stagedDialer{authOK: make(chan error, 16)}
	for i := 0; i < 16; i++ {
		d.authOK <- errors.New("login did not complete: no step 1's prompt within 5s")
	}
	r, ar := newRecorder(), &authRecorder{}
	s := collection.StartSession(collection.SessionConfig{
		Target: "10.0.0.9:23", Dialer: d, OnTransition: r.onTransition, Backoff: r.backoff,
		Auth: &collection.SessionAuth{Login: panelDialog()}, OnAuth: ar.on,
	})
	t.Cleanup(s.Close)
	r.waitStates(t, establishing, down)
	if v := ar.verdicts(); len(v) != 0 {
		t.Fatalf("a timed-out login published verdicts %+v", v)
	}
}

// An auth step declared over a transport that cannot run one is a protocol
// refusal: retrying cannot make a dialer grow a login.
func TestSessionAuthOverAnUnstagedDialerRejects(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	s := collection.StartSession(collection.SessionConfig{
		Target: "10.0.0.9:23", Dialer: d, OnTransition: r.onTransition, Backoff: r.backoff,
		Auth: &collection.SessionAuth{Login: panelDialog()},
	})
	t.Cleanup(s.Close)
	r.waitStates(t, establishing, rejected)
	if !strings.HasPrefix(r.lastTransition().Reason, "protocol: ") {
		t.Fatalf("reason = %q, want a protocol refusal", r.lastTransition().Reason)
	}
	if n := d.dialCount(); n != 0 {
		t.Fatalf("dialed %d times", n)
	}
}

// A reply the driver declares terminal (an "account locked") answers its
// exchange as the refusal and rejects the session, closing the connection.
func TestSessionTerminalReplyRejects(t *testing.T) {
	d, r := &fakeSessionDialer{}, newRecorder()
	s := collection.StartSession(collection.SessionConfig{
		Target: "10.0.0.9:23", Dialer: d, OnTransition: r.onTransition, Backoff: r.backoff,
		Terminal: func(line string) (string, bool) {
			if strings.Contains(line, "LOCKED") {
				return driver.ClassAuth, true
			}
			return "", false
		},
	})
	t.Cleanup(s.Close)
	r.waitStates(t, establishing, up)
	_, err := s.Exchange(context.Background(), "LOCKED", nil, time.Second)
	if terr, ok := collection.AsTerminal(err); !ok || terr.Reason != "echo LOCKED" {
		t.Fatalf("a terminal reply = %v, want the refusal with the device text", err)
	}
	r.waitStates(t, establishing, up, rejected)
	if !d.last().isClosed() {
		t.Fatal("the rejected session kept its connection")
	}
	time.Sleep(20 * time.Millisecond)
	if n := d.dialCount(); n != 1 {
		t.Fatalf("dialed %d times after a terminal reply, want 1", n)
	}
}

// A gate held across a node restart (#1056 ruling): a session started
// already rejected dials nothing, and its transition carries the refusal.
func TestSessionStartedRejectedDialsNothing(t *testing.T) {
	d := &fakeSessionDialer{}
	r, clock := newRecorder(), newFakeClock()
	refusal := authRefusal()
	s := collection.StartSession(collection.SessionConfig{
		Target: "10.0.0.9:23", Dialer: d, OnTransition: r.onTransition, Backoff: r.backoff,
		After: clock.After, Now: clock.Now, Rejected: refusal,
	})
	t.Cleanup(s.Close)
	r.waitStates(t, rejected)
	if tr := r.lastTransition(); tr.Refusal == nil || tr.Refusal.Class != driver.ClassAuth {
		t.Fatalf("the rejected transition carries refusal %+v", tr.Refusal)
	}
	clock.Advance(time.Hour, time.Minute)
	if n := d.dialCount(); n != 0 {
		t.Fatalf("a session started rejected dialed %d times", n)
	}
}
