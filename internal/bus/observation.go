package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// EndpointObservation takes the three reads a scale lab reconciles (#1639):
// what the simulator beside node observed, what the node tallied sending,
// then the simulator again. The node's read sits between the two simulator
// reads, so a request in transit is inside the bracket and never a false
// gap. Each read is bounded by the simulator-operation timeout.
func (s *Server) EndpointObservation(ctx context.Context, node string) (collection.EndpointObservation, error) {
	var o collection.EndpointObservation
	if err := s.readObservation(ctx, collection.SimobsSubject(node), ErrNoSimulator, &o.SimBefore); err != nil {
		return o, err
	}
	if err := s.readObservation(ctx, collection.RequestTallySubject(node), ErrNodeAbsent, &o.Node); err != nil {
		return o, err
	}
	if err := s.readObservation(ctx, collection.SimobsSubject(node), ErrNoSimulator, &o.SimAfter); err != nil {
		return o, err
	}
	return o, nil
}

// readObservation requests subject and decodes its reply into v; no answer
// (no responder, or none in time) is absent.
func (s *Server) readObservation(ctx context.Context, subject string, absent error, v any) error {
	ctx, cancel := context.WithTimeout(ctx, simopTimeout)
	defer cancel()
	msg, err := s.nc.RequestWithContext(ctx, subject, nil)
	if errors.Is(err, nats.ErrNoResponders) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrTimeout) {
		return absent
	}
	if err != nil {
		return fmt.Errorf("bus: observation read %s: %w", subject, err)
	}
	if err := json.Unmarshal(msg.Data, v); err != nil {
		return fmt.Errorf("bus: observation reply %s: %w", subject, err)
	}
	return nil
}
