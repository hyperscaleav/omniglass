package bus

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// startWorklistNudge forwards each worklist change the store announces to the
// node it names, as an empty message on the node's worklist-changed subject
// (#1945): the node pulls its worklist at once instead of on its next
// scheduled pull. A nudge is a hint, not a delivery: one lost to a node that
// is offline, or to a dropped publish, costs that node only the wait until its
// scheduled pull, which still runs. The node coalesces a burst, so the bus
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
		for node := range changes {
			if err := s.nc.Publish(collection.WorklistChangedSubject(node), nil); err != nil {
				slog.Warn("worklist nudge not sent", "facility", "collection", "node", node, "error", err.Error())
			}
		}
	}()
	return nil
}
