package bus

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The rule sweep age (#1986) is how long ago the event rule sweep last
// completed: a server's own from a clock the sweep sets, a peer's from the
// stamp it holds in the shared database.

func TestSweepClockAgesFromTheLastCompletedSweep(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := newSweepClock(start)
	if got := c.Age(start.Add(40 * time.Second)); got != 40*time.Second {
		t.Errorf("age before any sweep = %v, want the time since the clock began (40s)", got)
	}
	c.Done(start.Add(50 * time.Second))
	if got := c.Age(start.Add(53 * time.Second)); got != 3*time.Second {
		t.Errorf("age 3s after a sweep = %v, want 3s", got)
	}
	if got := c.Age(start.Add(10 * time.Second)); got != 0 {
		t.Errorf("age read before the last sweep = %v, want 0, never negative", got)
	}
}

func TestPeerSweepAgesSkipTheServersOwnComponent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	got := peerSweepAges([]storage.ServerSweepAge{
		{ComponentID: "mine", Age: 2 * time.Second},
		{ComponentID: "peer", Age: 90 * time.Second},
	}, "mine", now)
	want := []storage.PlatformMetric{{ComponentID: "peer", Key: writerclass.RuleSweepAge, Value: 90, TS: now}}
	if !slices.Equal(got, want) {
		t.Errorf("peer sweep ages = %+v, want %+v", got, want)
	}
	if got := peerSweepAges(nil, "mine", now); len(got) != 0 {
		t.Errorf("peer sweep ages of no servers = %+v, want none", got)
	}
}

// sweepStore records the rule sweep age's calls into the gateway, in order;
// the rest of the bus's store is the inert fakeStore.
type sweepStore struct {
	fakeStore
	mu       sync.Mutex
	sweepErr error
	calls    []string
	ages     []storage.ServerSweepAge
	within   time.Duration
	rows     []storage.PlatformMetric
	// released runs inside ReleaseRuleSweep, to observe what has stopped.
	released func()
}

func (f *sweepStore) log(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *sweepStore) record() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *sweepStore) SweepEventRules(context.Context, time.Time) error {
	f.log("sweep")
	return f.sweepErr
}
func (f *sweepStore) StampRuleSweep(_ context.Context, comp string) error {
	f.log("stamp " + comp)
	return nil
}
func (f *sweepStore) MarkServerSeen(_ context.Context, comp string) error {
	f.log("seen " + comp)
	return nil
}
func (f *sweepStore) ReleaseRuleSweep(_ context.Context, comp string) error {
	if f.released != nil {
		f.released()
	}
	f.log("release " + comp)
	return nil
}
func (f *sweepStore) ServerSweepAges(_ context.Context, within time.Duration) ([]storage.ServerSweepAge, error) {
	f.log("ages")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.within = within
	return f.ages, nil
}
func (f *sweepStore) RecordPlatformMetrics(_ context.Context, rows []storage.PlatformMetric) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, rows...)
	return nil
}

// A sweep tick stamps only a sweep that completed: on the server's own clock
// and, with a platform component, in the shared database. A failed sweep
// stamps neither, so it ages as a stalled one does.
func TestRuleSweepTickStampsOnlyACompletedSweep(t *testing.T) {
	t.Parallel()
	st := &sweepStore{sweepErr: errors.New("database gone")}
	start := time.Now().Add(-time.Minute)
	s := &Server{store: st, platformComponentID: "c1", sweepClock: newSweepClock(start)}

	s.ruleSweepTick(context.Background())
	if got := st.record(); !slices.Equal(got, []string{"sweep"}) {
		t.Errorf("a failed sweep made calls %v, want the sweep and no stamp", got)
	}
	if age := s.sweepClock.Age(time.Now()); age < 50*time.Second {
		t.Errorf("a failed sweep reset the server's own age to %v; it must keep aging", age)
	}

	st.sweepErr = nil
	s.ruleSweepTick(context.Background())
	if got := st.record(); !slices.Equal(got, []string{"sweep", "sweep", "stamp c1"}) {
		t.Errorf("a completed sweep made calls %v, want it stamped in the shared database", got)
	}
	if age := s.sweepClock.Age(time.Now()); age > 5*time.Second {
		t.Errorf("a completed sweep left the server's own age at %v, want about zero", age)
	}

	// With no platform component the server stamps its own clock alone.
	none := &sweepStore{}
	n := &Server{store: none, sweepClock: newSweepClock(start)}
	n.ruleSweepTick(context.Background())
	if got := none.record(); !slices.Equal(got, []string{"sweep"}) {
		t.Errorf("a server with no platform component made calls %v, want no shared stamp", got)
	}
	if age := n.sweepClock.Age(time.Now()); age > 5*time.Second {
		t.Errorf("own age after a completed sweep = %v, want about zero", age)
	}
}

// A peer pass marks this server seen before it reads, reads only peers seen
// within the window, and writes every one but itself.
func TestRecordPeerSweepAgesMarksSeenAndReadsLivePeers(t *testing.T) {
	t.Parallel()
	st := &sweepStore{ages: []storage.ServerSweepAge{{ComponentID: "c1", Age: time.Second}, {ComponentID: "c2", Age: 40 * time.Second}}}
	s := &Server{store: st, platformComponentID: "c1"}
	now := time.Now()
	if err := s.recordPeerSweepAges(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if got := st.record(); !slices.Equal(got, []string{"seen c1", "ages"}) {
		t.Errorf("a peer pass made calls %v, want this server marked seen, then the read", got)
	}
	if st.within != peerSeenWithin || peerSeenWithin >= 30*time.Second-settlementSweepEvery {
		t.Errorf("peers read within %v; want peerSeenWithin (%v), under the rule's 30 s by more than a sweep", st.within, peerSeenWithin)
	}
	want := []storage.PlatformMetric{{ComponentID: "c2", Key: writerclass.RuleSweepAge, Value: 40, TS: now}}
	if !slices.Equal(st.rows, want) {
		t.Errorf("wrote %+v, want only the peer's age %+v", st.rows, want)
	}
}

// Shutdown releases the stamps only after the sweep has stopped, so a sweep
// in flight cannot stamp again after the release; and a server with no
// platform component starts no peer timer and releases nothing.
func TestShutdownReleasesTheStampAfterTheSweepStops(t *testing.T) {
	t.Parallel()
	var sweepStopped atomic.Bool
	st := &sweepStore{}
	st.released = func() {
		if !sweepStopped.Load() {
			t.Error("the stamp was released while the sweep was still running")
		}
	}
	s := &Server{store: st, platformComponentID: "c1", sweepClock: newSweepClock(time.Now())}
	s.startPeerSweepAges()
	if s.stopPeerSweep == nil {
		t.Fatal("a server with a platform component started no peer sweep timer")
	}
	// A sweep in flight: it ends a moment after it is told to stop.
	s.stopSweep = make(chan struct{})
	s.sweepDone.Add(1)
	go func() {
		defer s.sweepDone.Done()
		<-s.stopSweep
		time.Sleep(50 * time.Millisecond)
		sweepStopped.Store(true)
	}()
	s.Shutdown()
	if got := st.record(); !slices.Equal(got, []string{"release c1"}) {
		t.Errorf("shutdown made calls %v, want the stamp released once", got)
	}

	none := &sweepStore{}
	n := &Server{store: none, sweepClock: newSweepClock(time.Now())}
	n.startPeerSweepAges()
	if n.stopPeerSweep != nil {
		t.Error("a server with no platform component started a peer sweep timer")
	}
	n.stopSweep = make(chan struct{})
	n.Shutdown()
	if got := none.record(); len(got) != 0 {
		t.Errorf("a server with no platform component made calls %v on shutdown, want none", got)
	}
}
