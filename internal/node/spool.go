package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

// The spool's caps (#454): how many batches, and how many encoded bytes, a
// node holds while the stream will not take them. Past either, the oldest
// batch is dropped, so a node behind a long outage keeps the newest stretch of
// what it collected, and says how much it dropped.
const (
	maxSpoolBatches = 10000
	maxSpoolBytes   = 16 << 20
)

// spoolPublishTimeout bounds one attempt's wait for the stream's ack.
const spoolPublishTimeout = 5 * time.Second

// publishFunc publishes one encoded batch to subject under id and returns
// once the stream has acked it: nil is the stream's word that it stored the
// batch, or already held a copy under id.
type publishFunc func(ctx context.Context, subject string, data []byte, id string) error

// jetStreamPublish is the node's publishFunc: a JetStream publish that waits
// for the stream's ack (collection.PublishBatch).
func jetStreamPublish(js jetstream.JetStream) publishFunc {
	return func(ctx context.Context, subject string, data []byte, id string) error {
		_, err := collection.PublishBatch(ctx, js, subject, data, id, spoolPublishTimeout)
		return err
	}
}

// spooledBatch is one encoded batch and the Nats-Msg-Id it was minted with.
type spooledBatch struct {
	id   string
	data []byte
}

// telemetrySpool is the node's every telemetry publish (#453, #454): a batch
// is minted an id, queued, and shipped in order once the stream acks it; one
// the stream has not acked stays queued and ships on the next tick under the
// same id, so a retry of a batch the stream stored but whose ack was lost is a
// duplicate (the stream's window, then the server's receipt, keeps it from
// landing twice). It is bounded by count and bytes, dropping the oldest batch
// past either and telling the drop once, the self-log buffer's shape
// (logs.go).
//
// After a failed publish the spool holds: a send only queues, so a node that
// cannot reach the bus pays one failed attempt per tick rather than one per
// batch, and the tick's retry ships the queue. Sends are safe from any
// goroutine (the session hooks send beside the tick); one goroutine ships at a
// time, and a send arriving while another ships leaves its batch to it.
type telemetrySpool struct {
	subject    string
	publish    publishFunc
	maxBatches int
	maxBytes   int

	mu         sync.Mutex
	queue      []spooledBatch
	bytes      int
	flushing   bool // a goroutine is shipping the queue
	inflight   bool // the head is with the stream: never dropped
	held       bool // a publish failed since the last tick: sends only queue
	dropped    int  // batches dropped since the last notice
	dropping   bool // the current overflow has been told at its start
	overflowed bool // batches dropped since the run loop last asked
}

// newTelemetrySpool is a spool shipping to subject through publish, at the
// default caps.
func newTelemetrySpool(subject string, publish publishFunc) *telemetrySpool {
	return &telemetrySpool{subject: subject, publish: publish, maxBatches: maxSpoolBatches, maxBytes: maxSpoolBytes}
}

// send mints the batch's id, queues it, and ships the queue unless the spool
// is holding or another goroutine is shipping. A batch that does not ship now
// is held, not lost, so send fails only on a batch it cannot encode.
func (s *telemetrySpool) send(ev *ogv1.TelemetryBatch) error {
	b, err := proto.Marshal(ev)
	if err != nil {
		return fmt.Errorf("node: marshal telemetry batch: %w", err)
	}
	s.mu.Lock()
	started := s.enqueueLocked(spooledBatch{id: collection.NewBatchID(), data: b})
	wait := s.held || s.flushing
	s.mu.Unlock()
	if started {
		slog.Warn("telemetry spool full, dropping the oldest batches", "facility", "delivery", "batches", s.maxBatches, "bytes", s.maxBytes)
	}
	if !wait {
		_ = s.flush(context.Background(), false)
	}
	return nil
}

// enqueueLocked appends b, first dropping the oldest batches (never the one
// in flight) until it fits both caps. It reports whether this drop began an
// overflow, so the caller tells it once.
func (s *telemetrySpool) enqueueLocked(b spooledBatch) bool {
	started := false
	for len(s.queue) > 0 && (len(s.queue)+1 > s.maxBatches || s.bytes+len(b.data) > s.maxBytes) {
		i := 0
		if s.inflight {
			i = 1
		}
		if i >= len(s.queue) {
			break
		}
		s.bytes -= len(s.queue[i].data)
		s.queue = append(s.queue[:i], s.queue[i+1:]...)
		s.dropped++
		s.overflowed = true
		if !s.dropping {
			s.dropping, started = true, true
		}
	}
	s.queue = append(s.queue, b)
	s.bytes += len(b.data)
	return started
}

// errSpoolBusy is flush's answer when another goroutine is shipping.
var errSpoolBusy = errors.New("node: telemetry spool is shipping")

// flush ships the queue in order until it is empty or a publish fails, which
// holds the spool until the next tick (force clears the hold first). It
// returns the failed publish's error, errSpoolBusy when another goroutine is
// shipping, and nil once the queue is empty, telling then how many batches an
// overflow dropped.
func (s *telemetrySpool) flush(ctx context.Context, force bool) error {
	s.mu.Lock()
	if force {
		s.held = false
	}
	if s.flushing {
		s.mu.Unlock()
		return errSpoolBusy
	}
	if s.held {
		s.mu.Unlock()
		return nil
	}
	s.flushing = true
	for len(s.queue) > 0 {
		head := s.queue[0]
		s.inflight = true
		s.mu.Unlock()
		err := s.publish(ctx, s.subject, head.data, head.id)
		s.mu.Lock()
		s.inflight = false
		if err != nil {
			s.flushing = false
			wasHeld := s.held
			s.held = true
			n := len(s.queue)
			s.mu.Unlock()
			if !wasHeld {
				slog.Warn("telemetry publish not acked, holding batches for the next tick", "facility", "delivery", "held", n, "error", err.Error())
			}
			return err
		}
		// The head stayed first while it was with the stream (an overflow
		// drops behind it), so it is the one to remove.
		s.bytes -= len(s.queue[0].data)
		s.queue[0] = spooledBatch{}
		s.queue = s.queue[1:]
	}
	s.flushing = false
	dropped := s.dropped
	s.dropped, s.dropping = 0, false
	s.mu.Unlock()
	if dropped > 0 {
		slog.Warn("telemetry spool overflow, oldest batches dropped", "facility", "delivery", "dropped", dropped)
	}
	return nil
}

// retry is the tick's call: clear the hold and ship the queue.
func (s *telemetrySpool) retry() error {
	err := s.flush(context.Background(), true)
	if errors.Is(err, errSpoolBusy) {
		return nil
	}
	return err
}

// drain ships the queue and waits until it is empty (a goroutine already
// shipping included), the one-shot run's last word: a batch still held when
// the stream will not take it, or when ctx ends, is an error.
func (s *telemetrySpool) drain(ctx context.Context) error {
	for {
		err := s.flush(ctx, true)
		if n := s.pending(); n == 0 {
			return nil
		} else if err != nil && !errors.Is(err, errSpoolBusy) {
			return fmt.Errorf("node: %d telemetry batches not acked by the stream: %w", n, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("node: %d telemetry batches not acked by the stream: %w", s.pending(), ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// pending is how many batches the spool holds.
func (s *telemetrySpool) pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// takeOverflow reports, once, that batches were dropped since the last call:
// the run loop's cue that a transition it remembers as sent may never land,
// so it forgets what it sent and the next tick sends current state again.
func (s *telemetrySpool) takeOverflow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.overflowed
	s.overflowed = false
	return o
}
