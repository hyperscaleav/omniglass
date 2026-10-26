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

// stubPublisher records every attempt and fails the first fail of them.
type stubPublisher struct {
	mu       sync.Mutex
	fail     int
	attempts []publishAttempt
	shipped  []publishAttempt
}

type publishAttempt struct {
	subject, id, tag string
}

func (p *stubPublisher) publish(_ context.Context, subject string, data []byte, id string) error {
	var ev ogv1.TelemetryBatch
	if err := proto.Unmarshal(data, &ev); err != nil {
		return err
	}
	a := publishAttempt{subject: subject, id: id, tag: ev.GetTaskId()}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attempts = append(p.attempts, a)
	if len(p.attempts) <= p.fail {
		return errors.New("no ack from the stream")
	}
	p.shipped = append(p.shipped, a)
	return nil
}

func (p *stubPublisher) setFail(n int) {
	p.mu.Lock()
	p.fail = len(p.attempts) + n
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

// TestSpoolShipsEveryBatchOnceInOrder: the stream refuses three attempts,
// then acks. Nothing is lost, nothing ships twice, the batches ship in the
// order they were sent, and every attempt at one batch carries the one id it
// was minted with.
func TestSpoolShipsEveryBatchOnceInOrder(t *testing.T) {
	pub := &stubPublisher{fail: 3}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	for _, tag := range []string{"b1", "b2", "b3"} {
		if err := s.send(tagged(tag)); err != nil {
			t.Fatalf("send %s: %v", tag, err)
		}
	}
	// The first send's attempt failed, so the spool holds the rest for the
	// tick rather than paying a failed publish on every send.
	if got := len(pub.attempts); got != 1 {
		t.Fatalf("attempts before the tick: %d, want 1 (b1, then held)", got)
	}
	for tick := 0; tick < 3; tick++ {
		_ = s.retry()
	}
	if got := pub.tags(); fmt.Sprint(got) != "[b1 b2 b3]" {
		t.Fatalf("shipped %v, want [b1 b2 b3] once each, in order", got)
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
	pub := &stubPublisher{fail: 1 << 30}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	s.maxBatches = 3
	recs := captureLogs(t, func() {
		for i := 1; i <= 5; i++ {
			_ = s.send(tagged(fmt.Sprintf("b%d", i)))
		}
		if !s.takeOverflow() {
			t.Fatal("the spool dropped batches and did not report the overflow")
		}
		if s.takeOverflow() {
			t.Fatal("the overflow was reported twice")
		}
		pub.setFail(0)
		_ = s.retry()
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
	pub := &stubPublisher{fail: 1 << 30}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	one, _ := proto.Marshal(tagged("b1"))
	s.maxBytes = 2*len(one) + 1
	for i := 1; i <= 4; i++ {
		_ = s.send(tagged(fmt.Sprintf("b%d", i)))
	}
	if n := s.pending(); n != 2 {
		t.Fatalf("pending under a two-batch byte cap: %d, want 2", n)
	}
	pub.setFail(0)
	_ = s.retry()
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
	publish := func(_ context.Context, _ string, data []byte, _ string) error {
		var ev ogv1.TelemetryBatch
		_ = proto.Unmarshal(data, &ev)
		if ev.GetTaskId() == "b1" {
			once.Do(func() { close(entered) })
			<-release
		}
		mu.Lock()
		shipped = append(shipped, ev.GetTaskId())
		mu.Unlock()
		return nil
	}
	s := newTelemetrySpool("og.v1.telemetry.edge", publish)
	s.maxBatches = 2
	done := make(chan struct{})
	go func() { _ = s.send(tagged("b1")); close(done) }()
	<-entered
	for i := 2; i <= 5; i++ {
		_ = s.send(tagged(fmt.Sprintf("b%d", i)))
	}
	close(release)
	<-done
	_ = s.retry()
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
	_ = s.retry()
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
	pub := &stubPublisher{fail: 1 << 30}
	s := newTelemetrySpool("og.v1.telemetry.edge", pub.publish)
	_ = s.send(tagged("b1"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.drain(ctx); err == nil {
		t.Fatal("drain with the stream away: want an error naming what is still held")
	}
	pub.setFail(0)
	if err := s.drain(ctx); err != nil {
		t.Fatalf("drain with the stream back: %v", err)
	}
}
