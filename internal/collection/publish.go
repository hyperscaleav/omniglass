package collection

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// NewBatchID mints a telemetry batch's Nats-Msg-Id (#453): 128 random bits in
// hex. It is minted once, when the batch is built, and the batch keeps it
// across every retry, which is what makes a retry a duplicate the stream (and
// past the stream's window, the server's receipt) can recognize. Random rather
// than a hash of the content: two batches a device really sent twice, the same
// lines in the same instant, are two batches.
func NewBatchID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails on a supported platform
	return hex.EncodeToString(b[:])
}

// PublishBatch publishes one encoded TelemetryBatch to subject under id and
// waits, at most timeout, for the stream's ack (#453). A nil error is the
// stream's word that it stored the batch, or already held a copy under id (the
// ack's Duplicate). Every telemetry publisher goes through it: the node's
// spool and the push lane, so neither can publish a batch the stream may not
// have stored and call it sent.
func PublishBatch(ctx context.Context, js jetstream.JetStream, subject string, data []byte, id string, timeout time.Duration) (*jetstream.PubAck, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return js.Publish(ctx, subject, data, jetstream.WithMsgID(id))
}
