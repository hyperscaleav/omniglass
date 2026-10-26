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
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

// The spool's caps (#454): how many batches, how many encoded bytes, and how
// long, a node holds while the stream will not take them. Past any of them
// the oldest batch is dropped, so a node behind a long outage keeps the newest
// stretch of what it collected, and says how much it dropped. The age cap sits
// well inside the server's receipt horizon (24 hours, ADR-0311): a batch the
// stream stored but whose ack was lost cannot come back after its receipt is
// pruned and land twice.
const (
	maxSpoolBatches = 10000
	maxSpoolBytes   = 16 << 20
	maxSpoolAge     = 12 * time.Hour
)

// spoolWindow is how many batches the spool keeps with the stream at once:
// it ships about spoolWindow batches per round trip rather than one, in
// order, since one connection delivers its publishes in the order they were
// made. spoolPublishTimeout bounds the wait for one batch's ack.
const (
	spoolWindow         = 32
	spoolPublishTimeout = 5 * time.Second
)

// ackWait blocks until the stream has answered one batch: nil is its word
// that it stored the batch, or already held a copy under its id.
type ackWait func(ctx context.Context) error

// publishFunc starts publishing one encoded batch to subject under id and
// returns at once, the ackWait being the stream's answer: so the spool can
// keep a window of batches with the stream. An error from starting is the
// publish's own (a batch too large for the bus, a connection that is gone).
type publishFunc func(subject string, data []byte, id string) (ackWait, error)

// jetStreamPublish is the node's publishFunc: a JetStream publish under the
// batch's id whose ack the spool waits on (collection.StartBatch).
func jetStreamPublish(js jetstream.JetStream) publishFunc {
	return func(subject string, data []byte, id string) (ackWait, error) {
		f, err := collection.StartBatch(js, subject, data, id)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context) error {
			select {
			case <-f.Ok():
				return nil
			case err := <-f.Err():
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		}, nil
	}
}

// refused reports whether a publish failed for a reason no retry changes:
// the batch is larger than the bus takes, or its subject is malformed. Such
// a batch is dropped and told, never held: held at the head, it would block
// every batch behind it until an overflow pushed it out.
func refused(err error) bool {
	return errors.Is(err, nats.ErrMaxPayload) || errors.Is(err, nats.ErrBadSubject)
}

// spooledBatch is one encoded batch, the Nats-Msg-Id it was minted with, and
// when it was queued.
type spooledBatch struct {
	id     string
	data   []byte
	queued time.Time
}

// telemetrySpool is the node's every telemetry publish (#453, #454): a batch
// is minted an id and queued, and a shipper ships the queue in order, keeping
// up to spoolWindow batches with the stream and removing each once acked. A
// batch the stream has not acked stays queued and ships on the next tick
// under the same id, so a retry of a batch the stream stored but whose ack was
// lost is a duplicate (the stream's window, then the server's receipt, keeps
// it from landing twice). It is bounded by count, bytes and age, dropping the
// oldest batch past any of them and telling the drop once, the self-log
// buffer's shape (logs.go).
//
// A send only queues and returns: no caller (a session's goroutine, the
// tick) ever waits on the stream. The shipper is a goroutine that runs while
// there is something to ship. After a failed publish the spool holds: sends
// only queue, so a node that cannot reach the bus pays one failed attempt per
// tick rather than one per batch, and the tick's retry ships the queue again
// from its head. The batches that were with the stream behind the failure are
// sent again under their ids, so any copy the stream already stored is a
// duplicate.
type telemetrySpool struct {
	subject    string
	publish    publishFunc
	maxBatches int
	maxBytes   int
	maxAge     time.Duration
	window     int
	now        func() time.Time

	mu         sync.Mutex
	queue      []spooledBatch // queue[:sent] are with the stream: never dropped
	sent       int
	bytes      int
	shipping   bool          // a shipper goroutine is running
	idle       chan struct{} // closed when the running shipper stops
	held       bool          // a publish failed since the last tick: sends only queue
	lastErr    error         // the publish error that held the spool
	dropped    int           // batches dropped since the last notice
	dropping   bool          // the current overflow has been told at its start
	overflowed bool          // batches dropped since the run loop last asked
}

// newTelemetrySpool is a spool shipping to subject through publish, at the
// default caps.
func newTelemetrySpool(subject string, publish publishFunc) *telemetrySpool {
	return &telemetrySpool{subject: subject, publish: publish, maxBatches: maxSpoolBatches, maxBytes: maxSpoolBytes,
		maxAge: maxSpoolAge, window: spoolWindow, now: time.Now}
}

// send mints the batch's id, queues it, and starts the shipper unless the
// spool is holding. It never waits on the stream; it fails only on a batch it
// cannot encode.
func (s *telemetrySpool) send(ev *ogv1.TelemetryBatch) error {
	b, err := proto.Marshal(ev)
	if err != nil {
		return fmt.Errorf("node: marshal telemetry batch: %w", err)
	}
	s.mu.Lock()
	started := s.expireLocked()
	if s.enqueueLocked(spooledBatch{id: collection.NewBatchID(), data: b, queued: s.now()}) {
		started = true
	}
	if !s.held {
		s.shipLocked()
	}
	s.mu.Unlock()
	if started {
		s.tellOverflowStart()
	}
	return nil
}

// tellOverflowStart says, once per overflow, that the spool began dropping.
func (s *telemetrySpool) tellOverflowStart() {
	slog.Warn("telemetry spool full, dropping the oldest batches", "facility", "delivery",
		"batches", s.maxBatches, "bytes", s.maxBytes, "age", s.maxAge.String())
}

// dropLocked drops the queued batch at i (never one with the stream) and
// counts it, reporting whether this drop began an overflow.
func (s *telemetrySpool) dropLocked(i int) bool {
	s.bytes -= len(s.queue[i].data)
	s.queue = append(s.queue[:i], s.queue[i+1:]...)
	s.dropped++
	s.overflowed = true
	if !s.dropping {
		s.dropping = true
		return true
	}
	return false
}

// expireLocked drops the batches past the age cap. The queue is in the order
// batches were queued, so they are the oldest, just behind those with the
// stream.
func (s *telemetrySpool) expireLocked() bool {
	started := false
	cutoff := s.now().Add(-s.maxAge)
	for s.sent < len(s.queue) && s.queue[s.sent].queued.Before(cutoff) {
		if s.dropLocked(s.sent) {
			started = true
		}
	}
	return started
}

// enqueueLocked appends b, first dropping the oldest batches (never one with
// the stream) until it fits both caps. It reports whether a drop began an
// overflow, so the caller tells it once.
func (s *telemetrySpool) enqueueLocked(b spooledBatch) bool {
	started := false
	for s.sent < len(s.queue) && (len(s.queue)+1 > s.maxBatches || s.bytes+len(b.data) > s.maxBytes) {
		if s.dropLocked(s.sent) {
			started = true
		}
	}
	s.queue = append(s.queue, b)
	s.bytes += len(b.data)
	return started
}

// shipLocked starts the shipper if there is something to ship and none is
// running.
func (s *telemetrySpool) shipLocked() {
	if s.shipping || len(s.queue) == 0 {
		return
	}
	s.shipping = true
	s.idle = make(chan struct{})
	go s.ship()
}

// flight is one batch with the stream: the ackWait to its answer, or the
// error that kept it from starting.
type flight struct {
	wait ackWait
	err  error
}

// ship is the shipper: it keeps up to window batches with the stream, in
// queue order, and removes the head as each is acked. A batch the bus
// refuses outright is dropped and told; any other failure holds the spool
// until the next tick, and the batches with the stream behind it are sent
// again then, under their ids.
func (s *telemetrySpool) ship() {
	var inflight []flight
	for {
		s.mu.Lock()
		for !s.held && len(inflight) < s.window && s.sent < len(s.queue) {
			b := s.queue[s.sent]
			s.sent++
			s.mu.Unlock()
			wait, err := s.publish(s.subject, b.data, b.id)
			s.mu.Lock()
			inflight = append(inflight, flight{wait: wait, err: err})
			if err != nil {
				break
			}
		}
		if len(inflight) == 0 {
			s.stopLocked()
			dropped := s.takeNoticeLocked()
			s.mu.Unlock()
			if dropped > 0 {
				slog.Warn("telemetry spool overflow, oldest batches dropped", "facility", "delivery", "dropped", dropped)
			}
			return
		}
		s.mu.Unlock()

		f := inflight[0]
		inflight = inflight[1:]
		err := f.err
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), spoolPublishTimeout)
			err = f.wait(ctx)
			cancel()
		}

		s.mu.Lock()
		if err == nil || refused(err) {
			// The head was with the stream, so nothing dropped it: it is
			// still queue[0].
			s.bytes -= len(s.queue[0].data)
			s.queue[0] = spooledBatch{}
			s.queue = s.queue[1:]
			s.sent--
			if err != nil {
				s.dropped++
				s.overflowed = true
			}
			s.mu.Unlock()
			if err != nil {
				slog.Warn("telemetry batch refused by the bus, dropped", "facility", "delivery", "error", err.Error())
			}
			continue
		}
		wasHeld := s.held
		s.held = true
		s.lastErr = err
		n := len(s.queue)
		s.stopLocked()
		s.mu.Unlock()
		if !wasHeld {
			slog.Warn("telemetry publish not acked, holding batches for the next tick", "facility", "delivery", "held", n, "error", err.Error())
		}
		return
	}
}

// stopLocked marks the shipper stopped: what was with the stream is queued
// again, to be sent from the head.
func (s *telemetrySpool) stopLocked() {
	s.sent = 0
	s.shipping = false
	close(s.idle)
}

// takeNoticeLocked is how many batches were dropped since the last notice,
// told once the queue drains.
func (s *telemetrySpool) takeNoticeLocked() int {
	if len(s.queue) > 0 {
		return 0
	}
	dropped := s.dropped
	s.dropped, s.dropping = 0, false
	return dropped
}

// retry is the tick's call: clear the hold, drop what has aged out, and ship
// the queue from its head. It does not wait on the stream.
func (s *telemetrySpool) retry() {
	s.mu.Lock()
	s.held = false
	started := s.expireLocked()
	s.shipLocked()
	s.mu.Unlock()
	if started {
		s.tellOverflowStart()
	}
}

// drain ships the queue and waits until it is empty, the one-shot run's last
// word and a stopping node's: a batch still held when the stream will not
// take it, or when ctx ends, is an error.
func (s *telemetrySpool) drain(ctx context.Context) error {
	for {
		s.mu.Lock()
		if len(s.queue) == 0 && !s.shipping {
			s.mu.Unlock()
			return nil
		}
		if !s.shipping {
			s.held = false
			s.shipLocked()
		}
		idle := s.idle
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return fmt.Errorf("node: %d telemetry batches not acked by the stream: %w", s.pending(), ctx.Err())
		case <-idle:
		}
		s.mu.Lock()
		held, n, err := s.held, len(s.queue), s.lastErr
		s.mu.Unlock()
		if n == 0 {
			return nil
		}
		if held {
			return fmt.Errorf("node: %d telemetry batches not acked by the stream: %w", n, err)
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
