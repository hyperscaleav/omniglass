package bus

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// checkinAgeEvery is how often the server writes each claimed outpost's
// check-in age (#1625): half the outpost's default heartbeat, so a silent outpost's
// series steps at least twice per missed heartbeat. checkinAgeTimeout bounds
// one pass.
const (
	checkinAgeEvery   = 15 * time.Second
	checkinAgeTimeout = 10 * time.Second
)

// checkinAges is the check-in age of each outpost at now: the seconds since it
// last spoke (its last heartbeat, else its claim), as a checkin-age row on its
// platform component. An outpost whose clock reads ahead of the database's reads
// zero, never a negative age. Pure.
func checkinAges(outposts []storage.OutpostCheckin, now time.Time) []storage.PlatformMetric {
	out := make([]storage.PlatformMetric, 0, len(outposts))
	for _, n := range outposts {
		out = append(out, storage.PlatformMetric{ComponentID: n.ComponentID, Key: writerclass.CheckinAge,
			Value: max(now.Sub(n.Since).Seconds(), 0), TS: now})
	}
	return out
}

// recordCheckinAges writes every claimed outpost's check-in age at now, the
// server's word for how long each outpost has been silent. The age grows while
// an outpost sends nothing, which is the series an outpost-silent rule reads.
func (s *Server) recordCheckinAges(ctx context.Context, now time.Time) error {
	outposts, err := s.store.OutpostCheckins(ctx)
	if err != nil {
		return err
	}
	if len(outposts) == 0 {
		return nil
	}
	return s.store.RecordPlatformMetrics(ctx, checkinAges(outposts, now))
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
					slog.Warn("checkin age write failed", "facility", "outpost", "error", err.Error())
				}
			}
		}
	}()
}
