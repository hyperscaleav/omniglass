package node

import (
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
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

	mu     sync.Mutex
	states []collection.SessionState
	auths  []bool
	faults []string
}

func (r *resyncRig) count() (states, auths, faults int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.states), len(r.auths), len(r.faults)
}

func newResyncRig(t *testing.T) *resyncRig {
	t.Helper()
	r := &resyncRig{bus: &stubPublisher{}}
	r.spool = newTelemetrySpool("og.v1.telemetry.edge", "edge", r.bus.publish)
	r.spool.maxBatches = 1
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
	r.sessions = newSessionManager(map[string]collection.SessionDialer{"tcp": &authFakeDialer{}}, hooks)
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
	s0, a0, f0 := r.count()
	if s0 == 0 || a0 != 1 || f0 != 1 {
		t.Fatalf("before the overflow: %d states, %d auth verdicts, %d faults; want the session's own, one verdict, one fault", s0, a0, f0)
	}

	// The session goes down while the bus is away, and the batch saying so is
	// dropped at the cap.
	r.overflow(t)
	r.bus.setDown(false)
	resendAfterOverflow(r.spool, r.coll, r.sched, r.sessions)

	s1, a1, f1 := r.count()
	if s1 != s0+1 || a1 != a0+1 || f1 != f0+1 {
		t.Fatalf("after the overflow: %d states, %d auth verdicts, %d faults; want exactly one more of each (%d, %d, %d)",
			s1, a1, f1, s0+1, a0+1, f0+1)
	}
	r.mu.Lock()
	last := r.states[len(r.states)-1]
	r.mu.Unlock()
	if last != collection.SessionUp {
		t.Fatalf("resent state = %s, want the session's current state, up", last)
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
