package bus

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// commandHealthEvery is how often the server reads the fleet's command queues
// and writes their health on each node's platform component (#1626), on the
// check-in age's cadence. commandHealthTimeout bounds one pass.
const (
	commandHealthEvery   = 15 * time.Second
	commandHealthTimeout = 10 * time.Second
)

// recordCommandQueueHealth reads every node's command queue over the window
// since the previous pass's read and writes it as platform metrics. It answers
// the read's time, the next pass's since; a failed read keeps since, so the
// next pass's window covers this one's. A failed write after a good read
// moves since on: the window's counts are lost rather than counted twice.
func (s *Server) recordCommandQueueHealth(ctx context.Context, since time.Time) (time.Time, error) {
	read, err := s.store.CommandQueueHealth(ctx, since)
	if err != nil {
		return since, err
	}
	rows := storage.CommandQueueMetrics(read, since)
	if len(rows) == 0 {
		return read.Now, nil
	}
	return read.Now, s.store.RecordPlatformMetrics(ctx, rows)
}

// startCommandHealth writes the command queue health every commandHealthEvery
// on a goroutine of its own, holding the window between passes. A failed pass
// is logged and the next one reads afresh.
func (s *Server) startCommandHealth() {
	ctx, cancel := context.WithCancel(context.Background())
	s.stopCommandHealth = cancel
	s.sweepDone.Add(1)
	go func() {
		defer s.sweepDone.Done()
		t := time.NewTicker(commandHealthEvery)
		defer t.Stop()
		var since time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pass, done := context.WithTimeout(ctx, commandHealthTimeout)
				next, err := s.recordCommandQueueHealth(pass, since)
				done()
				since = next
				if err != nil && ctx.Err() == nil {
					slog.Warn("command queue health write failed", "facility", "command", "error", err.Error())
				}
			}
		}
	}()
}
