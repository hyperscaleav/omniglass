package collection

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// NewBatchID mints a telemetry batch's Nats-Msg-Id (#453): a UUIDv7, a
// millisecond timestamp and 74 random bits. It is minted once, when the batch
// is built, and the batch keeps it across every retry, which is what makes a
// retry a duplicate the stream (and past the stream's window, the server's
// receipt) can recognize. Random in its tail rather than a hash of the
// content: two batches a device really sent twice, the same lines in the same
// instant, are two batches. Time-ordered in its head: the server keys its
// receipt by subject and id (ADR-0311), so each publisher's receipts append at
// the right edge of the index rather than dirtying a random page of it on
// every batch.
func NewBatchID() string {
	// NewV7 fails only when crypto/rand does, which it never does on a
	// supported platform.
	return uuid.Must(uuid.NewV7()).String()
}

// PublishBatch publishes one encoded TelemetryBatch to subject under id and
// waits, at most timeout, for the stream's ack (#453). A nil error is the
// stream's word that it stored the batch, or already held a copy under id (the
// ack's Duplicate). The push lane publishes through it, and the node's spool
// through StartBatch, so no publisher can send a batch the stream may not
// have stored and call it sent.
func PublishBatch(ctx context.Context, js jetstream.JetStream, subject string, data []byte, id string, timeout time.Duration) (*jetstream.PubAck, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return js.Publish(ctx, subject, data, jetstream.WithMsgID(id))
}

// StartBatch begins publishing one encoded TelemetryBatch to subject under id
// and returns without waiting: the future is the stream's ack, or its refusal
// (#453). The node's spool keeps a window of these with the stream, so it
// ships faster than one batch per round trip, and treats a batch as sent only
// once its future says the stream stored it.
func StartBatch(js jetstream.JetStream, subject string, data []byte, id string) (jetstream.PubAckFuture, error) {
	return js.PublishAsync(subject, data, jetstream.WithMsgID(id))
}
