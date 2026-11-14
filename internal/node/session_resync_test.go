package node

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/proto"
)

// A held session says its state, its auth verdict and its listeners' faults
// only when they change, so a spool overflow that drops the batch carrying
// one leaves the server believing the old value (#1213). After an overflow
// the run loop resends all three, once each, and on a tick without one it
// resends nothing.

// resyncRig is a held session, its listener's fault, a spool the bus can be
// cut from, and the hooks counting what the session published, as the run
// loop's hooks do.
type resyncRig struct {
	sessions *sessionManager
	spool    *telemetrySpool
	coll     *collector
	sched    *pollSchedule
	bus      *stubPublisher
	dialer   *stallingDialer

	mu     sync.Mutex
	states []collection.SessionState
	auths  []bool
	faults []string
}

// stallingDialer is the login dialer, until stall is set: then a connect
// hangs until the session gives up on it, so a dropped session stays
// recovering for the test and publishes no further transition.
type stallingDialer struct {
	*authFakeDialer
	stall atomic.Bool
}

func (d *stallingDialer) Connect(ctx context.Context, target string, timeout time.Duration) (collection.PendingConn, error) {
	if d.stall.Load() {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return d.authFakeDialer.Connect(ctx, target, timeout)
}

func (r *resyncRig) lastState() collection.SessionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.states[len(r.states)-1]
}

// shippedSessionStates is the session-state values the bus stored, in order.
func shippedSessionStates(t *testing.T, bus *stubPublisher) []string {
	t.Helper()
	bus.mu.Lock()
	defer bus.mu.Unlock()
	var got []string
	for _, a := range bus.shipped {
		var b ogv1.TelemetryBatch
		if err := proto.Unmarshal(a.data, &b); err != nil {
			t.Fatalf("decode a shipped batch: %v", err)
		}
		for _, g := range b.GetGroups() {
			for _, p := range g.GetProperties() {
				if p.GetName() == collection.SignalSessionState {
					got = append(got, strings.Trim(p.GetValueJson(), `"`))
				}
			}
		}
	}
	return got
}

func (r *resyncRig) count() (states, auths, faults int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states), len(r.auths), len(r.faults)
}

func newResyncRig(t *testing.T) *resyncRig {
	t.Helper()
	r := &resyncRig{bus: &stubPublisher{}, dialer: &stallingDialer{authFakeDialer: &authFakeDialer{}}}
	r.spool = newTelemetrySpool("og.v1.telemetry.edge", "edge", r.bus.publish)
	r.spool.maxBatches = 1
	// The test flushes; a timer flush that split the resend in two would drop
	// half of it at the one-batch cap.
	r.spool.afterFunc = (&fakeTimers{}).afterFunc
	r.sched = newPollSchedule()
	hooks := sessionHooks{
		transition: func(taskID, _ string, tr collection.SessionTransition) {
			r.mu.Lock()
			r.states = append(r.states, tr.State)
			r.mu.Unlock()
			_ = publishBatch(r.spool, "edge", taskID, sessionSamples(tr))
		},
		auth: func(taskID, _ string, v collection.AuthVerdict) {
			r.mu.Lock()
			r.auths = append(r.auths, v.Authenticated)
			r.mu.Unlock()
			_ = publishBatch(r.spool, "edge", taskID, []collection.Sample{authSample(v)})
		},
		fault: func(taskID string, cause error) {
			r.mu.Lock()
			r.faults = append(r.faults, cause.Error())
			r.mu.Unlock()
			_ = publishCollectionFailed(r.spool, "edge", taskID, cause)
		},
	}
	r.sessions = newSessionManager(map[string]collection.SessionDialer{"tcp": r.dialer}, hooks)
	t.Cleanup(r.sessions.closeAll)
	r.coll = newCollector(r.spool, "edge", nil, r.sessions, r.sched, nil)
	return r
}

// hold starts a listen session that logs in, waits until it is up and
// authenticated, and has its listener meet a fault.
func (r *resyncRig) hold(t *testing.T) {
	t.Helper()
	task := listenTask("l1", "ep-1", "10.0.0.5:23")
	task.Session = loginSession()
	task.Secrets = map[string]map[string]string{"login": {"username": "admin", "password": "newtron"}}
	task.Fingerprint = "fp-1"
	r.sessions.reconcile([]collection.TaskSpec{task})
	deadline := time.Now().Add(3 * time.Second)
	for r.sessions.lookup("ep-1") == nil || !r.sessions.isUp("ep-1") {
		if time.Now().After(deadline) {
			t.Fatal("the session never came up")
		}
		time.Sleep(2 * time.Millisecond)
	}
	for {
		if _, a, _ := r.count(); a > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the auth verdict was never published")
		}
		time.Sleep(2 * time.Millisecond)
	}
	// A claimed line that does not extract is a listener fault.
	r.sessions.held["ep-1"].router.Claim(driver.LineMessage("EVT VOLUME 3"))
}

// drop cuts the bus and the session's connection, and waits until the
// session reports it is recovering: the batch saying so sits in the spool.
func (r *resyncRig) drop(t *testing.T) {
	t.Helper()
	r.bus.setDown(true)
	r.dialer.stall.Store(true)
	for _, c := range r.dialer.all() {
		_ = c.Close()
	}
	deadline := time.Now().Add(3 * time.Second)
	for r.lastState() != collection.SessionRecovering {
		if time.Now().After(deadline) {
			t.Fatal("the session never reported the drop")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// overflow cuts the bus and publishes enough batches to drop what the spool
// held, so the overflow is raised and the session's state is gone from it.
func (r *resyncRig) overflow(t *testing.T) {
	t.Helper()
	r.bus.setDown(true)
	queueTagged(t, r.spool, "n1")
	waitIdle(t, r.spool)
	queueTagged(t, r.spool, "n2")
	queueTagged(t, r.spool, "n3")
}

func TestSessionResyncsItsStateAuthAndFaultsAfterAnOverflow(t *testing.T) {
	t.Parallel()
	r := newResyncRig(t)
	r.hold(t)

	// The session goes down while the bus is away, and the batch saying so is
	// dropped at the cap.
	r.drop(t)
	s0, a0, f0 := r.count()
	if a0 != 1 || f0 != 1 {
		t.Fatalf("before the overflow: %d auth verdicts, %d faults; want one of each", a0, f0)
	}
	r.overflow(t)
	// The bus returns, and the next tick runs as the run loop's does: the
	// spool retries what it held, then the overflow is answered.
	r.bus.setDown(false)
	r.spool.retry()
	waitIdle(t, r.spool)
	resendAfterOverflow(r.spool, r.coll, r.sched, r.sessions)

	s1, a1, f1 := r.count()
	if s1 != s0+1 || a1 != a0+1 || f1 != f0+1 {
		t.Fatalf("after the overflow: %d states, %d auth verdicts, %d faults; want exactly one more of each (%d, %d, %d)",
			s1, a1, f1, s0+1, a0+1, f0+1)
	}
	if last := r.lastState(); last != collection.SessionRecovering {
		t.Fatalf("resent state = %s, want the session's current state, recovering", last)
	}
	r.spool.flush()
	waitIdle(t, r.spool)
	if got := shippedSessionStates(t, r.bus); len(got) == 0 || got[len(got)-1] != string(collection.SessionRecovering) {
		t.Fatalf("the bus received session states %v, want the resent recovering last", got)
	}

	// The overflow was told once: the next tick resends nothing.
	resendAfterOverflow(r.spool, r.coll, r.sched, r.sessions)
	if s, a, f := r.count(); s != s1 || a != a1 || f != f1 {
		t.Fatalf("a tick without an overflow resent: %d states, %d auth verdicts, %d faults; want %d, %d, %d", s, a, f, s1, a1, f1)
	}
}

func TestSessionResyncsNothingOnATickWithoutAnOverflow(t *testing.T) {
	t.Parallel()
	r := newResyncRig(t)
	r.hold(t)
	s0, a0, f0 := r.count()
	for i := 0; i < 3; i++ {
		resendAfterOverflow(r.spool, r.coll, r.sched, r.sessions)
	}
	if s, a, f := r.count(); s != s0 || a != a0 || f != f0 {
		t.Fatalf("ticks without an overflow resent: %d states, %d auth verdicts, %d faults; want %d, %d, %d", s, a, f, s0, a0, f0)
	}
}

// A fault the listener's next line cleared is not resent, and a session
// that has said nothing has nothing to resend.
func TestSessionResyncResendsOnlyWhatIsCurrent(t *testing.T) {
	t.Parallel()
	r := newResyncRig(t)
	r.hold(t)
	r.sessions.held["ep-1"].router.Claim(driver.LineMessage("EVT INPUT hdmi2"))
	_, _, f0 := r.count()
	r.sessions.resync()
	if _, _, f := r.count(); f != f0 {
		t.Fatalf("a cleared fault was resent: %d faults, want %d", f, f0)
	}

	quiet := newSessionManager(nil, sessionHooks{transition: func(string, string, collection.SessionTransition) {
		t.Error("a resync published a transition nothing had reported")
	}})
	quiet.held["ep-q"] = &heldSession{router: newListenerRouter(sessionHooks{})}
	quiet.resync()
}
