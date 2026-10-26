package bus

import (
	"context"
	"time"

	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go/jetstream"
)

// SetTelemetryAckWait shortens how long the stream waits for an ack before it
// redelivers, for a test that kills a consumer and waits on the redelivery;
// the returned func restores it. The bus tests run serially, so the shared
// value is never read by two at once.
func SetTelemetryAckWait(d time.Duration) (restore func()) {
	prior := telemetryAckWait
	telemetryAckWait = d
	return func() { telemetryAckWait = prior }
}

// PublishTelemetryAs publishes b on the push lane under the given id, as
// PublishTelemetry does under a fresh one, and reports whether the stream
// held it as a duplicate: what a publisher's retry looks like to the stream.
func (s *Server) PublishTelemetryAs(ctx context.Context, b *ogv1.TelemetryBatch, id string) (bool, error) {
	return s.publishTelemetry(ctx, b, id)
}

// DeleteTelemetryStream deletes OG_TELEMETRY (its consumer with it), so a
// publish has no stream to store it.
func (s *Server) DeleteTelemetryStream(ctx context.Context) error {
	if s.consumeCtx != nil {
		s.consumeCtx.Stop()
		s.consumeCtx = nil
	}
	js, err := jetstream.New(s.nc)
	if err != nil {
		return err
	}
	return js.DeleteStream(ctx, telemetryStream)
}

// RestartTelemetryConsumer creates the stream and its consumer again, as the
// server does at start.
func (s *Server) RestartTelemetryConsumer() error {
	return s.startTelemetryConsumer()
}

// TelemetryBacklog is how many batches OG_TELEMETRY still holds. The stream
// is a work queue, so a batch leaves it only when the consumer acks it: zero
// means every batch published so far, a redelivery included, was taken and
// acked.
func (s *Server) TelemetryBacklog(ctx context.Context) (uint64, error) {
	stream, err := s.js.Stream(ctx, telemetryStream)
	if err != nil {
		return 0, err
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return 0, err
	}
	return info.State.Msgs, nil
}

// TelemetryDuplicateWindow is how long OG_TELEMETRY, as the server created
// it, remembers a batch's Nats-Msg-Id.
func (s *Server) TelemetryDuplicateWindow(ctx context.Context) (time.Duration, error) {
	stream, err := s.js.Stream(ctx, telemetryStream)
	if err != nil {
		return 0, err
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return 0, err
	}
	return info.Config.Duplicates, nil
}

// SetTelemetryNakDelay sets the base of the backoff a transiently failed
// batch is redelivered after; the returned func restores it.
func SetTelemetryNakDelay(d time.Duration) (restore func()) {
	prior := telemetryNakDelay
	telemetryNakDelay = d
	return func() { telemetryNakDelay = prior }
}
