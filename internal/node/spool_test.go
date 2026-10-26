package node

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/proto"
)

// The node's telemetry spool (#454, folded into #453): a batch the stream has
// not acked stays held, in order, and ships on the next tick under the id it
// was minted with, so a retry is a duplicate the stream and the server's
// receipt recognize; at its cap the oldest batch drops and the drop is told.

// stubPublisher records every attempt and fails every one while the stream
// is down, as a lost connection fails every batch with the stream at once
// (one connection delivers in order, and the node buffers nothing across a
// reconnect, so a later batch never lands ahead of an earlier one that
// failed). It stores a batch once per id, as the stream's duplicate window
// does, so a retry of a batch it already stored is answered and not stored
// again.
type stubPublisher struct {
	mu       sync.Mutex
	down     bool
	attempts []publishAttempt
	shipped  []publishAttempt
	stored   map[string]bool
}

type publishAttempt struct {
	subject, id, tag string
}

func (p *stubPublisher) publish(subject string, data []byte, id string) (ackWait, error) {
	var ev ogv1.TelemetryBatch
	if err := proto.Unmarshal(data, &ev); err != nil {
		return nil, err
	}
	a := publishAttempt{subject: subject, id: id, tag: ev.GetTaskId()}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attempts = append(p.attempts, a)
	if p.down {
		return answered(errors.New("no ack from the stream")), nil
	}
	if p.stored == nil {
		p.stored = map[string]bool{}
	}
	if !p.stored[id] {
		p.stored[id] = true
		p.shipped = append(p.shipped, a)
	}
	return answered(nil), nil
}

// answered is an ackWait the stream has already answered with err.
func answered(err error) ackWait {
	return func(context.Context) error { return err }
}

// attemptCount is how many publishes the spool has started.
func (p *stubPublisher) attemptCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.attempts)
}

// waitIdle waits until the spool's shipper has stopped: the queue shipped,
// or the spool holding after a failure.
func waitIdle(t *testing.T, s *telemetrySpool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		busy := s.shipping
		s.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the spool's shipper never stopped")
		}
		time.Sleep(time.Millisecond)
	}
}

// tick is the run loop's tick: the retry, and the shipping it starts.
func tick(t *testing.T, s *telemetrySpool) {
	t.Helper()
	s.retry()
	waitIdle(t, s)
}

func (p *stubPublisher) setDown(down bool) {
	p.mu.Lock()
	p.down = down
	p.mu.Unlock()
}

func (p *stubPublisher) tags() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.shipped))
	for i, a := range p.shipped {
		out[i] = a.tag
	}
	return out
}

// tagged is a batch the stub reads back by its tag (carried as the task id).
func tagged(tag string) *ogv1.TelemetryBatch {
	return &ogv1.TelemetryBatch{TaskId: tag, NodeId: "edge"}
}

// TestSpoolShipsEveryBatchOnceInOrder: the stream is away for three ticks,
// then back. Nothing is lost, nothing is stored twice, the batches ship in
// the order they were sent, and every attempt at one batch carries the one
// id it was minted with.
func TestSpoolShipsEveryBatchOnceInOrder(t *testing.T) {
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	for _, tag := range []string{"b1", "b2", "b3"} {
		if err := s.send(tagged(tag)); err != nil {
			t.Fatalf("send %s: %v", tag, err)
		}
	}
	waitIdle(t, s)
	// A failed attempt holds the spool: a send now only queues, rather than
	// paying a failed publish per batch until the tick.
	before := pub.attemptCount()
	if err := s.send(tagged("b4")); err != nil {
		t.Fatalf("send b4: %v", err)
	}
	waitIdle(t, s)
	if got := pub.attemptCount(); got != before {
		t.Fatalf("a send while held started %d publishes, want none before the tick", got-before)
	}
	for i := 0; i < 3; i++ {
		tick(t, s)
	}
	if n := s.pending(); n != 4 {
		t.Fatalf("held while the stream is away: %d, want all 4", n)
	}
	pub.setDown(false)
	tick(t, s)
	if got := pub.tags(); fmt.Sprint(got) != "[b1 b2 b3 b4]" {
		t.Fatalf("shipped %v, want [b1 b2 b3 b4] once each, in order", got)
	}
	ids := map[string]bool{}
	for _, a := range pub.attempts {
		if a.tag == "b1" {
			ids[a.id] = true
		}
		if a.subject != "og.v1.telemetry.edge" {
			t.Fatalf("published on %q, want the node's own subject", a.subject)
		}
	}
	if len(ids) != 1 {
		t.Fatalf("b1's attempts carried %d ids, want the one it was minted with", len(ids))
	}
	if n := s.pending(); n != 0 {
		t.Fatalf("pending after the stream acked: %d", n)
	}
}

// captureLogs records the warnings the spool logs while fn runs.
func captureLogs(t *testing.T, fn func()) []slog.Record {
	t.Helper()
	h := &recordingHandler{}
	prior := slog.Default()
	slog.SetDefault(slog.New(h))
	defer slog.SetDefault(prior)
	fn()
	return h.all()
}

type recordingHandler struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.recs = append(h.recs, r.Clone())
	h.mu.Unlock()
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }
func (h *recordingHandler) all() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]slog.Record(nil), h.recs...)
}

func attr(r slog.Record, key string) (slog.Value, bool) {
	var v slog.Value
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			v, found = a.Value, true
			return false
		}
		return true
	})
	return v, found
}

// TestSpoolDropsTheOldestAtItsCapAndSaysHowMany: with the stream away, a
// spool capped at three batches keeps the newest three, and when the stream
// is back it ships them and tells, once, how many it dropped. The overflow
// is reported to the run loop once, so it can re-send what it remembers as
// sent.
func TestSpoolDropsTheOldestAtItsCapAndSaysHowMany(t *testing.T) {
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	s.maxBatches = 3
	recs := captureLogs(t, func() {
		_ = s.send(tagged("b1"))
		waitIdle(t, s) // b1 failed: held, nothing with the stream
		for i := 2; i <= 5; i++ {
			_ = s.send(tagged(fmt.Sprintf("b%d", i)))
		}
		if !s.takeOverflow() {
			t.Fatal("the spool dropped batches and did not report the overflow")
		}
		if s.takeOverflow() {
			t.Fatal("the overflow was reported twice")
		}
		pub.setDown(false)
		tick(t, s)
	})
	if got := pub.tags(); fmt.Sprint(got) != "[b3 b4 b5]" {
		t.Fatalf("shipped %v, want the newest three [b3 b4 b5]", got)
	}
	notices := 0
	for _, r := range recs {
		if v, ok := attr(r, "dropped"); ok {
			notices++
			if v.Int64() != 2 {
				t.Fatalf("the overflow notice carried dropped=%v, want 2", v)
			}
		}
	}
	if notices != 1 {
		t.Fatalf("overflow notices carrying the count: %d, want exactly 1", notices)
	}
}

// TestSpoolBoundsItsBytes: the byte cap drops the oldest batch too, so a run
// of large batches cannot grow the spool past it.
func TestSpoolBoundsItsBytes(t *testing.T) {
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	one, _ := proto.Marshal(tagged("b1"))
	s.maxBytes = 2*len(one) + 1
	_ = s.send(tagged("b1"))
	waitIdle(t, s)
	for i := 2; i <= 4; i++ {
		_ = s.send(tagged(fmt.Sprintf("b%d", i)))
	}
	if n := s.pending(); n != 2 {
		t.Fatalf("pending under a two-batch byte cap: %d, want 2", n)
	}
	pub.setDown(false)
	tick(t, s)
	if got := pub.tags(); fmt.Sprint(got) != "[b3 b4]" {
		t.Fatalf("shipped %v, want [b3 b4]", got)
	}
}

// TestSpoolNeverDropsTheBatchInFlight: a batch the stream is answering is
// not dropped by an overflow behind it; once acked it leaves, and the
// newest batches ship after it.
func TestSpoolNeverDropsTheBatchInFlight(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var shipped []string
	publish := func(_ string, data []byte, _ string) (ackWait, error) {
		var ev ogv1.TelemetryBatch
		_ = proto.Unmarshal(data, &ev)
		mu.Lock()
		shipped = append(shipped, ev.GetTaskId())
		mu.Unlock()
		if ev.GetTaskId() != "b1" {
			return answered(nil), nil
		}
		// b1's ack is held until the test releases it, with the shipper
		// waiting on it.
		return func(context.Context) error {
			once.Do(func() { close(entered) })
			<-release
			return nil
		}, nil
	}
	s := newTelemetrySpool("og.v1.telemetry.edge", publish)
	s.maxBatches = 2
	_ = s.send(tagged("b1"))
	<-entered
	for i := 2; i <= 5; i++ {
		_ = s.send(tagged(fmt.Sprintf("b%d", i)))
	}
	close(release)
	waitIdle(t, s)
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(shipped) != "[b1 b5]" {
		t.Fatalf("shipped %v, want b1 (in flight, never dropped) then the newest, b5", shipped)
	}
}

// TestSpoolConcurrentSendsShipOnce: many goroutines sending at once (the
// session hooks beside the tick) ship every batch once.
func TestSpoolConcurrentSendsShipOnce(t *testing.T) {
	pub := &stubPublisher{}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.send(tagged(fmt.Sprintf("b%d", i)))
		}(i)
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	seen := map[string]int{}
	for _, tag := range pub.tags() {
		seen[tag]++
	}
	if len(seen) != 50 {
		t.Fatalf("shipped %d distinct batches, want 50", len(seen))
	}
	for tag, n := range seen {
		if n != 1 {
			t.Fatalf("%s shipped %d times", tag, n)
		}
	}
}

// TestSpoolDrainReportsWhatItCouldNotShip: the one-shot run's last word is a
// drain, and a batch still held when the stream will not take it is an
// error, never a quiet return.
func TestSpoolDrainReportsWhatItCouldNotShip(t *testing.T) {
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	_ = s.send(tagged("b1"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.drain(ctx); err == nil {
		t.Fatal("drain with the stream away: want an error naming what is still held")
	}
	pub.setDown(false)
	if err := s.drain(ctx); err != nil {
		t.Fatalf("drain with the stream back: %v", err)
	}
}

// TestSpoolDropsABatchPastItsAge: a batch held longer than the spool's age
// cap is dropped, as the caps drop one, and told: the cap sits inside the
// server's receipt horizon, so a batch the stream stored but whose ack was
// lost cannot come back after its receipt is pruned and land twice.
func TestSpoolDropsABatchPastItsAge(t *testing.T) {
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	now := time.Unix(1_000_000, 0)
	s.now = func() time.Time { return now }
	_ = s.send(tagged("b1"))
	waitIdle(t, s) // b1 failed: held, nothing with the stream
	now = now.Add(s.maxAge + time.Minute)
	_ = s.send(tagged("b2"))
	pub.setDown(false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got := pub.tags(); fmt.Sprint(got) != "[b2]" {
		t.Fatalf("shipped %v, want [b2] (b1 past the age cap dropped)", got)
	}
	if !s.takeOverflow() {
		t.Fatal("the spool dropped a batch past its age and did not report it")
	}
}

// TestSpoolSendNeverWaitsOnTheStream: a send queues and returns, whatever the
// stream's round trip, so a session's goroutine is never held shipping
// everyone's batches, and the spool keeps several batches with the stream at
// once, so it ships faster than one per round trip, still in order.
func TestSpoolSendNeverWaitsOnTheStream(t *testing.T) {
	var mu sync.Mutex
	var shipped []string
	// A stream 50 ms away: a batch is stored in the order it was published,
	// and its ack arrives a round trip later.
	publish := func(_ string, data []byte, _ string) (ackWait, error) {
		var ev ogv1.TelemetryBatch
		_ = proto.Unmarshal(data, &ev)
		mu.Lock()
		shipped = append(shipped, ev.GetTaskId())
		mu.Unlock()
		acked := time.Now().Add(50 * time.Millisecond)
		return func(context.Context) error {
			time.Sleep(time.Until(acked))
			return nil
		}, nil
	}
	s := newTelemetrySpool("og.v1.telemetry.edge", publish)
	start := time.Now()
	var slowest time.Duration
	var wg sync.WaitGroup
	var smu sync.Mutex
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				t0 := time.Now()
				_ = s.send(tagged(fmt.Sprintf("g%d-%02d", g, i)))
				if d := time.Since(t0); d > 0 {
					smu.Lock()
					if d > slowest {
						slowest = d
					}
					smu.Unlock()
				}
			}
		}(g)
	}
	wg.Wait()
	if slowest > 20*time.Millisecond {
		t.Fatalf("the slowest send took %v: a send waited on the stream", slowest)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("120 batches over a 50 ms round trip took %v: shipped one round trip at a time", took)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(shipped) != 120 {
		t.Fatalf("shipped %d batches, want 120", len(shipped))
	}
	last := map[string]string{}
	for _, tag := range shipped {
		g := tag[:2]
		if tag <= last[g] {
			t.Fatalf("%s shipped after %s: out of the order it was sent in", tag, last[g])
		}
		last[g] = tag
	}
}
