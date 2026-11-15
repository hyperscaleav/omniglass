package bus

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// peerSweepEvery is how often the server writes its peers' rule sweep ages
// (#1986). A peer's age is read from the shared database, so a pass adds at
// most this much staleness to it, well inside the 30 s the rule-sweep-stalled
// rule tolerates. peerSweepTimeout bounds one pass.
const (
	peerSweepEvery   = 10 * time.Second
	peerSweepTimeout = 8 * time.Second
)

// sweepClock is when this server's event rule sweep last completed. The sweep
// goroutine sets it and the report window reads it, so the age it reports
// does not depend on that goroutine running: a stalled sweep stops setting
// it and the age grows. It begins at the server's start, so a sweep that
// never completes ages too.
type sweepClock struct {
	mu   sync.Mutex
	last time.Time
}

func newSweepClock(start time.Time) *sweepClock { return &sweepClock{last: start} }

// Done records a sweep that completed at at.
func (c *sweepClock) Done(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = at
}

// Age is the time from the last completed sweep to now, never negative.
func (c *sweepClock) Age(now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return max(now.Sub(c.last), 0)
}

// peerSweepAges is the rule-sweep-age row of every server but this one, at
// now: the ages the shared database holds for the others, each on that
// server's own platform component. A server writes its own age from its
// clock, not from its stamp. Pure.
func peerSweepAges(ages []storage.ServerSweepAge, own string, now time.Time) []storage.PlatformMetric {
	out := make([]storage.PlatformMetric, 0, len(ages))
	for _, a := range ages {
		if a.ComponentID == own {
			continue
		}
		out = append(out, storage.PlatformMetric{ComponentID: a.ComponentID, Key: writerclass.RuleSweepAge,
			Value: a.Age.Seconds(), TS: now})
	}
	return out
}

// recordPeerSweepAges writes every peer server's rule sweep age at now, so a
// live server raises rule-sweep-stalled for a peer whose sweep has stalled
// (#1986). A peer that stopped cleanly holds no stamp and is not judged; one
// that died keeps its stamp, which ages.
func (s *Server) recordPeerSweepAges(ctx context.Context, now time.Time) error {
	ages, err := s.store.ServerSweepAges(ctx)
	if err != nil {
		return err
	}
	rows := peerSweepAges(ages, s.platformComponentID, now)
	if len(rows) == 0 {
		return nil
	}
	return s.store.RecordPlatformMetrics(ctx, rows)
}

// stampRuleSweep records a completed sweep: on the clock this server reports
// its own age from, and, when it has a platform component, in the shared
// database where a peer reads it. A failed stamp is logged; the next sweep
// stamps afresh.
func (s *Server) stampRuleSweep(ctx context.Context, at time.Time) {
	s.sweepClock.Done(at)
	if s.platformComponentID == "" {
		return
	}
	if err := s.store.StampRuleSweep(ctx, s.platformComponentID); err != nil {
		slog.Warn("rule sweep stamp failed", "facility", "rules", "error", err.Error())
	}
}

// startPeerSweepAges writes the peers' sweep ages every peerSweepEvery on a
// goroutine of its own, as the check-in age timer does. Without a platform
// component there is no one to compare against and it does not start.
func (s *Server) startPeerSweepAges() {
	if s.platformComponentID == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.stopPeerSweep = cancel
	s.sweepDone.Add(1)
	go func() {
		defer s.sweepDone.Done()
		t := time.NewTicker(peerSweepEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				pass, done := context.WithTimeout(ctx, peerSweepTimeout)
				err := s.recordPeerSweepAges(pass, now)
				done()
				if err != nil && ctx.Err() == nil {
					slog.Warn("peer sweep age write failed", "facility", "rules", "error", err.Error())
				}
			}
		}
	}()
}

// releaseRuleSweep clears this server's stamp as it stops cleanly, so a peer
// does not alarm on a server that was told to stop.
func (s *Server) releaseRuleSweep() {
	if s.platformComponentID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), peerSweepTimeout)
	defer cancel()
	if err := s.store.ReleaseRuleSweep(ctx, s.platformComponentID); err != nil {
		slog.Warn("rule sweep release failed", "facility", "rules", "error", err.Error())
	}
}
