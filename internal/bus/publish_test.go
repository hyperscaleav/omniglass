package bus

import (
	"context"
	"testing"

	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// TestPublishTelemetryTakesAContextWithoutDeadline is a regression test for a real
// bug: PublishTelemetry flushed with the caller's context, but nats.FlushWithContext
// REQUIRES a deadline and an HTTP request context has none, so every push failed
// with "context requires a deadline" and surfaced only as an opaque 503. The flush
// now carries its own bound, so a deadline-free caller context is fine.
func TestPublishTelemetryTakesAContextWithoutDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs nats-server")
	}
	srv, err := New(Config{Host: "127.0.0.1", Port: -1}, fakeStore{})
	if err != nil {
		t.Fatalf("new bus: %v", err)
	}
	defer srv.Shutdown()

	if err := srv.PublishTelemetry(context.Background(), &ogv1.TelemetryBatch{
		Owner: &ogv1.Owner{Kind: "component", Ref: "bar-1"},
	}); err != nil {
		t.Fatalf("publish with a deadline-free context: %v", err)
	}
}
