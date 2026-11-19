package bus

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// startWorklistNudge forwards each worklist change the store announces to the
// outpost it names, as an empty message on the outpost's worklist-changed subject
// (#1945): the outpost pulls its worklist at once instead of on its next
// scheduled pull. A nudge is a hint, not a delivery: one lost to an outpost that
// is offline, or to a dropped publish, costs that outpost only the wait until its
// scheduled pull, which still runs. The outpost coalesces a burst, so the bus
// forwards every announcement as it comes.
func (s *Server) startWorklistNudge() error {
	ctx, cancel := context.WithCancel(context.Background())
	changes, err := s.store.WorklistChanges(ctx)
	if err != nil {
		cancel()
		return fmt.Errorf("bus: listen for worklist changes: %w", err)
	}
	s.stopNudge = cancel
	if changes == nil {
		return nil
	}
	s.sweepDone.Add(1)
	go func() {
		defer s.sweepDone.Done()
		for outpost := range changes {
			if err := s.nc.Publish(collection.WorklistChangedSubject(outpost), nil); err != nil {
				slog.Warn("worklist nudge not sent", "facility", "collection", "outpost", outpost, "error", err.Error())
			}
		}
	}()
	return nil
}
