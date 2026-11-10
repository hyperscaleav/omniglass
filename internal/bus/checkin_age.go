package bus

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// checkinAgeEvery is how often the server writes each claimed node's
// check-in age (#1625): half the node's default heartbeat, so a silent node's
// series steps at least twice per missed heartbeat. checkinAgeTimeout bounds
// one pass.
const (
	checkinAgeEvery   = 15 * time.Second
	checkinAgeTimeout = 10 * time.Second
)

// checkinAges is the check-in age of each node at now: the seconds since it
// last spoke (its last heartbeat, else its claim), as a checkin-age row on its
// platform component. A node whose clock reads ahead of the database's reads
// zero, never a negative age. Pure.
func checkinAges(nodes []storage.NodeCheckin, now time.Time) []storage.PlatformMetric {
	out := make([]storage.PlatformMetric, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, storage.PlatformMetric{ComponentID: n.ComponentID, Key: writerclass.CheckinAge,
			Value: max(now.Sub(n.Since).Seconds(), 0), TS: now})
	}
	return out
}

// recordCheckinAges writes every claimed node's check-in age at now, the
// server's word for how long each node has been silent. The age grows while
// a node sends nothing, which is the series a node-silent rule reads.
func (s *Server) recordCheckinAges(ctx context.Context, now time.Time) error {
	nodes, err := s.store.NodeCheckins(ctx)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return nil
	}
	return s.store.RecordPlatformMetrics(ctx, checkinAges(nodes, now))
}

// startCheckinAge writes the check-in ages every checkinAgeEvery on a
// goroutine of its own, so a slow pass never holds the settlement sweep. A
// failed pass is logged and the next one writes the age afresh.
func (s *Server) startCheckinAge() {
	ctx, cancel := context.WithCancel(context.Background())
	s.stopCheckinAge = cancel
	s.sweepDone.Add(1)
	go func() {
		defer s.sweepDone.Done()
		t := time.NewTicker(checkinAgeEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				pass, done := context.WithTimeout(ctx, checkinAgeTimeout)
				err := s.recordCheckinAges(pass, now)
				done()
				if err != nil && ctx.Err() == nil {
					slog.Warn("checkin age write failed", "facility", "node", "error", err.Error())
				}
			}
		}
	}()
}
