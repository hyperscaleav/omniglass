package node

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The wait after a failed pull doubles from 1 s to the 30 s cap, jittered
// over the upper half of each step (#1577).
func TestAFailedPullBacksOffFromOneSecondToTheCap(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		attempt  int
		low, top time.Duration
	}{
		{1, 500 * time.Millisecond, time.Second},
		{2, time.Second, 2 * time.Second},
		{3, 2 * time.Second, 4 * time.Second},
		{5, 8 * time.Second, 16 * time.Second},
		{6, 15 * time.Second, 30 * time.Second},
		{40, 15 * time.Second, 30 * time.Second},
		{0, 15 * time.Second, 30 * time.Second},
	} {
		if got := pullBackoff(c.attempt, 0); got != c.low {
			t.Errorf("attempt %d, no jitter: %v, want %v", c.attempt, got, c.low)
		}
		if got := pullBackoff(c.attempt, 0.999999); got < c.low || got >= c.top {
			t.Errorf("attempt %d, most jitter: %v, want in [%v, %v)", c.attempt, got, c.low, c.top)
		}
	}
}

// Backoff never grows past the cap, however many pulls fail.
func TestPullBackoffNeverPassesTheCap(t *testing.T) {
	t.Parallel()
	for attempt := 1; attempt < 200; attempt++ {
		if got := pullBackoff(attempt, 0.999999); got > pullBackoffCap || got <= 0 {
			t.Fatalf("attempt %d: %v", attempt, got)
		}
	}
}

// The pull interval is spread ten percent either side.
func TestThePullIntervalIsJitteredTenPercent(t *testing.T) {
	t.Parallel()
	every := 30 * time.Second
	if got := jitterInterval(every, 0); got != 27*time.Second {
		t.Errorf("least jitter: %v, want 27s", got)
	}
	if got := jitterInterval(every, 0.5); got != every {
		t.Errorf("middle jitter: %v, want 30s", got)
	}
	if got := jitterInterval(every, 0.999999); got < 32*time.Second || got > 33*time.Second {
		t.Errorf("most jitter: %v, want under 33s", got)
	}
}

// The first worklist's pulls count toward the pull loss like every later one
// (#1882): a node refused once and then served reads the window's refused
// share, and a run stopped while refused tallies no pull it never finished.
func TestTheFirstWorklistsPullsCountTowardThePullLoss(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.DiscardHandler)
	t0 := time.Unix(1_800_000_000, 0)
	key := writerclass.WorklistPullLossRatio

	self := newNodeReport(time.Hour, 0, t0)
	var calls atomic.Int64
	pull := func(context.Context) (collection.WorklistReply, error) {
		if calls.Add(1) == 1 {
			return collection.WorklistReply{}, errors.New("the server sent no worklist")
		}
		return collection.WorklistReply{}, nil
	}
	if _, ok, err := firstWorklist(t.Context(), pull, false, logger, self); !ok || err != nil {
		t.Fatalf("refused once, then served: ok %v, err %v", ok, err)
	}
	if got := readings(self.take(t0.Add(time.Hour)))[key]; got != 0.5 {
		t.Errorf("one of two first pulls refused: %s = %v, want 0.5", key, got)
	}

	stopped := newNodeReport(time.Hour, 0, t0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	refused := func(context.Context) (collection.WorklistReply, error) {
		return collection.WorklistReply{}, context.Canceled
	}
	if _, ok, err := firstWorklist(ctx, refused, false, logger, stopped); ok || err != nil {
		t.Fatalf("stopped: ok %v, err %v, want neither", ok, err)
	}
	if got := readings(stopped.take(t0.Add(time.Hour)))[key]; got != 0 {
		t.Errorf("a stopped pull counted as refused: %s = %v, want 0", key, got)
	}

	// A one-shot run returns the refusal rather than retrying it.
	if _, ok, err := firstWorklist(t.Context(), refused, true, logger, nil); ok || err == nil {
		t.Errorf("one-shot refused: ok %v, err %v, want the error", ok, err)
	}
}

// While the first worklist is outstanding the node ticks on its heartbeat
// (#1882), and once it arrives the wait returns it and ticks no more.
func TestTheNodeTicksWhileItAwaitsItsFirstWorklist(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	ticked := make(chan struct{}, 16)
	var ticks atomic.Int64
	go func() {
		for range 3 {
			<-ticked
		}
		close(release)
	}()
	want := collection.WorklistReply{ConfigGeneration: 7}
	wl, ok, err := awaitFirstWorklist(time.Millisecond, func() (collection.WorklistReply, bool, error) {
		<-release
		return want, true, nil
	}, func() {
		ticks.Add(1)
		select {
		case ticked <- struct{}{}:
		default:
		}
	})
	if !ok || err != nil || wl.ConfigGeneration != 7 {
		t.Fatalf("awaited %+v, ok %v, err %v; want the worklist", wl, ok, err)
	}
	if n := ticks.Load(); n < 3 {
		t.Errorf("%d ticks while the worklist was outstanding, want at least 3", n)
	}
	after := ticks.Load()
	time.Sleep(10 * time.Millisecond)
	if n := ticks.Load(); n != after {
		t.Errorf("ticked %d more times after the worklist arrived", n-after)
	}
}
