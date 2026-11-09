package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The node's telemetry spool (#454, folded into #453): a batch the stream has
// not acked stays held, in order, and ships on the next tick under the id it
// was minted with, so a retry is a duplicate the stream and the server's
// receipt recognize; at its cap the oldest batch drops and the drop is told.
// A node's tick lands as one batch of task groups (#1582): the spool gathers
// the groups and flushes them on its timer, at its byte threshold, or ahead of
// a whole batch, minting the batch's id and bytes once at the flush.

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
	subject, id string
	tags        []string // the task ids of the batch's groups, in order
	data        []byte   // the bytes the attempt carried
}

// batchTags is the task ids of a shipped batch's groups, in order.
func batchTags(data []byte) ([]string, error) {
	var ev ogv1.TelemetryBatch
	if err := proto.Unmarshal(data, &ev); err != nil {
		return nil, err
	}
	var tags []string
	for _, g := range ev.GetGroups() {
		tags = append(tags, g.GetTaskId())
	}
	return tags, nil
}

func (p *stubPublisher) publish(subject string, data []byte, id string) (ackWait, error) {
	tags, err := batchTags(data)
	if err != nil {
		return nil, err
	}
	a := publishAttempt{subject: subject, id: id, tags: tags, data: append([]byte(nil), data...)}
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

// tags is the task ids of every group the stream stored, in the order stored.
func (p *stubPublisher) tags() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, a := range p.shipped {
		out = append(out, a.tags...)
	}
	return out
}

// tagged is a one-sample task group the stub reads back by its tag (carried
// as the task id).
func tagged(tag string) *ogv1.TaskGroup {
	return &ogv1.TaskGroup{TaskId: tag, Metrics: []*ogv1.MetricSample{{Name: "tcp-open", Value: 1}}}
}

// queueTagged is one batch of one tagged group: the group added, then the
// spool flushed, as a tick of one task is.
func queueTagged(t *testing.T, s *telemetrySpool, tag string) {
	t.Helper()
	if err := s.add(tagged(tag)); err != nil {
		t.Fatalf("add %s: %v", tag, err)
	}
	s.flush()
}

// fakeTimers stands in for the spool's flush timer: the test fires it.
type fakeTimers struct {
	mu    sync.Mutex
	armed []*fakeTimer
}

type fakeTimer struct {
	mu      sync.Mutex
	every   time.Duration
	fn      func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	was := !t.stopped
	t.stopped = true
	return was
}

func (f *fakeTimers) afterFunc(d time.Duration, fn func()) stopper {
	t := &fakeTimer{every: d, fn: fn}
	f.mu.Lock()
	f.armed = append(f.armed, t)
	f.mu.Unlock()
	return t
}

// live is the timers armed and not stopped.
func (f *fakeTimers) live() []*fakeTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*fakeTimer
	for _, t := range f.armed {
		t.mu.Lock()
		if !t.stopped {
			out = append(out, t)
		}
		t.mu.Unlock()
	}
	return out
}

// fire runs every timer still armed, as their interval passing would.
func (f *fakeTimers) fire() {
	for _, t := range f.live() {
		t.mu.Lock()
		t.stopped = true
		t.mu.Unlock()
		t.fn()
	}
}

// timedSpool is a spool on the stub whose flush timer the test fires.
func timedSpool(pub *stubPublisher) (*telemetrySpool, *fakeTimers) {
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	timers := &fakeTimers{}
	s.afterFunc = timers.afterFunc
	return s, timers
}

// TestSpoolShipsEveryBatchOnceInOrder: the stream is away for three ticks,
// then back. Nothing is lost, nothing is stored twice, the batches ship in
// the order they were sent, and every attempt at one batch carries the one
// id it was minted with. (Adapted for #1582: each batch is one group added
// and flushed, the node's path, where it was a whole batch sent.)
func TestSpoolShipsEveryBatchOnceInOrder(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	for _, tag := range []string{"b1", "b2", "b3"} {
		queueTagged(t, s, tag)
	}
	waitIdle(t, s)
	// A failed attempt holds the spool: a flush now only queues, rather than
	// paying a failed publish per batch until the tick.
	before := pub.attemptCount()
	queueTagged(t, s, "b4")
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
		if fmt.Sprint(a.tags) == "[b1]" {
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
//
// Adapted for #1582: each batch is one group added and flushed.
func TestSpoolDropsTheOldestAtItsCapAndSaysHowMany(t *testing.T) {
	// Serial: it captures the slog default through captureLogs, which swaps it.
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	s.maxBatches = 3
	recs := captureLogs(t, func() {
		queueTagged(t, s, "b1")
		waitIdle(t, s) // b1 failed: held, nothing with the stream
		for i := 2; i <= 5; i++ {
			queueTagged(t, s, fmt.Sprintf("b%d", i))
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
// of large batches cannot grow the spool past it. (Adapted for #1582: one
// group per flushed batch, the clock fixed so every batch is the same size.)
func TestSpoolBoundsItsBytes(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	at := time.Unix(1_000_000, 0)
	s.now = func() time.Time { return at }
	one, _ := proto.Marshal(&ogv1.TelemetryBatch{NodeId: "edge", Ts: timestamppb.New(at), Groups: []*ogv1.TaskGroup{tagged("b1")}})
	s.maxBytes = 2*len(one) + 1
	queueTagged(t, s, "b1")
	waitIdle(t, s)
	for i := 2; i <= 4; i++ {
		queueTagged(t, s, fmt.Sprintf("b%d", i))
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
// newest batches ship after it. (Adapted for #1582: one group per flushed
// batch.)
func TestSpoolNeverDropsTheBatchInFlight(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var shipped []string
	publish := func(_ string, data []byte, _ string) (ackWait, error) {
		tags, _ := batchTags(data)
		tag := strings.Join(tags, ",")
		mu.Lock()
		shipped = append(shipped, tag)
		mu.Unlock()
		if tag != "b1" {
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
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", publish)
	s.maxBatches = 2
	queueTagged(t, s, "b1")
	<-entered
	for i := 2; i <= 5; i++ {
		queueTagged(t, s, fmt.Sprintf("b%d", i))
	}
	close(release)
	waitIdle(t, s)
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(shipped) != "[b1 b5]" {
		t.Fatalf("shipped %v, want b1 (in flight, never dropped) then the newest, b5", shipped)
	}
}

// TestSpoolConcurrentSendsShipOnce: many goroutines adding at once (the
// session hooks beside the tick) ship every group once. (Adapted for #1582:
// the goroutines add groups and some flush, so groups share batches.)
func TestSpoolConcurrentSendsShipOnce(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.add(tagged(fmt.Sprintf("b%d", i)))
			if i%7 == 0 {
				s.flush()
			}
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
		t.Fatalf("shipped %d distinct groups, want 50", len(seen))
	}
	for tag, n := range seen {
		if n != 1 {
			t.Fatalf("%s shipped %d times", tag, n)
		}
	}
}

// TestSpoolDrainReportsWhatItCouldNotShip: the one-shot run's last word is a
// drain, and a batch still held when the stream will not take it is an
// error, never a quiet return. (Adapted for #1582: one group added, flushed
// by the drain itself.)
func TestSpoolDrainReportsWhatItCouldNotShip(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	if err := s.add(tagged("b1")); err != nil {
		t.Fatal(err)
	}
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
// (Adapted for #1582: one group per flushed batch; the age runs from the
// flush.)
func TestSpoolDropsABatchPastItsAge(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	now := time.Unix(1_000_000, 0)
	var nmu sync.Mutex
	s.now = func() time.Time { nmu.Lock(); defer nmu.Unlock(); return now }
	queueTagged(t, s, "b1")
	waitIdle(t, s) // b1 failed: held, nothing with the stream
	nmu.Lock()
	now = now.Add(s.maxAge + time.Minute)
	nmu.Unlock()
	queueTagged(t, s, "b2")
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
// (Adapted for #1582: each batch is one group added and flushed.)
func TestSpoolSendNeverWaitsOnTheStream(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var shipped []string
	// A stream 50 ms away: a batch is stored in the order it was published,
	// and its ack arrives a round trip later.
	publish := func(_ string, data []byte, _ string) (ackWait, error) {
		tags, _ := batchTags(data)
		mu.Lock()
		shipped = append(shipped, tags...)
		mu.Unlock()
		acked := time.Now().Add(50 * time.Millisecond)
		return func(context.Context) error {
			time.Sleep(time.Until(acked))
			return nil
		}, nil
	}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", publish)
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
				_ = s.add(tagged(fmt.Sprintf("g%d-%02d", g, i)))
				s.flush()
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
		t.Fatalf("shipped %d groups, want 120", len(shipped))
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

// shippedBatches is the batches the stream stored, decoded, in order.
func (p *stubPublisher) shippedBatches(t *testing.T) []*ogv1.TelemetryBatch {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*ogv1.TelemetryBatch, 0, len(p.shipped))
	for _, a := range p.shipped {
		var ev ogv1.TelemetryBatch
		if err := proto.Unmarshal(a.data, &ev); err != nil {
			t.Fatalf("unmarshal shipped batch: %v", err)
		}
		out = append(out, &ev)
	}
	return out
}

// TestSpoolFlushesATickAsOneBatch: N groups added within one interval ship
// as one batch of N groups, in the order added, under the node's id, when
// the interval's timer fires; nothing ships before it.
func TestSpoolFlushesATickAsOneBatch(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s, timers := timedSpool(pub)
	const n = 7
	for i := 0; i < n; i++ {
		if err := s.add(tagged(fmt.Sprintf("t%d", i))); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	live := timers.live()
	if len(live) != 1 {
		t.Fatalf("%d flush timers armed, want 1 for the interval", len(live))
	}
	if live[0].every != defaultFlushEvery {
		t.Fatalf("flush timer armed for %v, want the default %v", live[0].every, defaultFlushEvery)
	}
	if got := pub.attemptCount(); got != 0 {
		t.Fatalf("%d publishes before the interval passed, want none", got)
	}
	timers.fire()
	waitIdle(t, s)
	batches := pub.shippedBatches(t)
	if len(batches) != 1 {
		t.Fatalf("shipped %d batches, want 1 for the interval", len(batches))
	}
	b := batches[0]
	if b.GetNodeId() != "edge" || b.GetTs() == nil {
		t.Fatalf("batch node %q ts %v, want edge and a flush time", b.GetNodeId(), b.GetTs())
	}
	if b.GetTaskId() != "" || len(b.GetMetrics()) != 0 {
		t.Fatalf("a tick batch carried top-level task samples: task %q, %d metrics", b.GetTaskId(), len(b.GetMetrics()))
	}
	want := "[t0 t1 t2 t3 t4 t5 t6]"
	if got := fmt.Sprint(pub.tags()); got != want {
		t.Fatalf("groups %s, want %s", got, want)
	}
	// The next add arms a fresh interval.
	_ = s.add(tagged("next"))
	if len(timers.live()) != 1 {
		t.Fatal("the next add after a flush armed no timer")
	}
}

// TestSpoolFlushEveryIsConfigurable: the interval is a field on the spool.
func TestSpoolFlushEveryIsConfigurable(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s, timers := timedSpool(pub)
	s.flushEvery = 250 * time.Millisecond
	_ = s.add(tagged("t"))
	live := timers.live()
	if len(live) != 1 || live[0].every != 250*time.Millisecond {
		t.Fatalf("armed %v, want one 250ms timer", live)
	}
}

// TestSpoolFlushesEarlyAtItsBytes: at the default 512 KiB threshold, groups
// whose pending bytes would pass it flush early, ahead of the timer, and no
// shipped batch is over the threshold.
func TestSpoolFlushesEarlyAtItsBytes(t *testing.T) {
	t.Parallel()
	if defaultFlushBytes != 512<<10 {
		t.Fatalf("default flush threshold %d, want 512 KiB", defaultFlushBytes)
	}
	pub := &stubPublisher{}
	s, _ := timedSpool(pub)
	big := strings.Repeat("x", 100<<10)
	for i := 0; i < 6; i++ {
		g := tagged(fmt.Sprintf("t%d", i))
		g.Properties = []*ogv1.PropertySample{{Name: "blob", ValueJson: big}}
		if err := s.add(g); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	waitIdle(t, s)
	p := func() []publishAttempt {
		pub.mu.Lock()
		defer pub.mu.Unlock()
		return append([]publishAttempt(nil), pub.shipped...)
	}()
	if len(p) != 1 {
		t.Fatalf("shipped %d batches before the timer, want 1 flushed early at the threshold", len(p))
	}
	if len(p[0].data) > defaultFlushBytes {
		t.Fatalf("early batch is %d bytes, over the %d threshold", len(p[0].data), defaultFlushBytes)
	}
	if got := fmt.Sprint(p[0].tags); got != "[t0 t1 t2 t3 t4]" {
		t.Fatalf("early batch carried %s, want the five groups under the threshold", got)
	}
	if err := s.drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(pub.tags()); got != "[t0 t1 t2 t3 t4 t5]" {
		t.Fatalf("shipped %s, want every group once in order", got)
	}
}

// TestSpoolFlushesWhenItReachesItsBytes: a group that brings the pending
// bytes to the threshold flushes them at once.
func TestSpoolFlushesWhenItReachesItsBytes(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s, timers := timedSpool(pub)
	one, _ := proto.Marshal(&ogv1.TelemetryBatch{Groups: []*ogv1.TaskGroup{tagged("a")}})
	s.flushBytes = 2 * len(one)
	_ = s.add(tagged("a"))
	if pub.attemptCount() != 0 {
		t.Fatal("one group under the threshold shipped")
	}
	_ = s.add(tagged("b"))
	waitIdle(t, s)
	if got := fmt.Sprint(pub.tags()); got != "[a b]" {
		t.Fatalf("shipped %s at the threshold, want [a b]", got)
	}
	if len(timers.live()) != 0 {
		t.Fatal("the interval timer stayed armed after the threshold flushed")
	}
}

// TestSpoolOversizedGroupRidesAlone: a group bigger than the threshold
// flushes what was pending first, then ships in a batch of its own.
func TestSpoolOversizedGroupRidesAlone(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s, _ := timedSpool(pub)
	s.flushBytes = 1 << 10
	_ = s.add(tagged("small"))
	huge := tagged("huge")
	huge.Properties = []*ogv1.PropertySample{{Name: "blob", ValueJson: strings.Repeat("x", 4<<10)}}
	if err := s.add(huge); err != nil {
		t.Fatalf("add: %v", err)
	}
	waitIdle(t, s)
	var got []string
	pub.mu.Lock()
	for _, a := range pub.shipped {
		got = append(got, strings.Join(a.tags, ","))
	}
	pub.mu.Unlock()
	if fmt.Sprint(got) != "[small huge]" {
		t.Fatalf("batches %v, want [small huge]: the pending group first, the oversized one alone", got)
	}
}

// TestSpoolSkipsAnEmptyGroup: a group with nothing in it adds nothing, so a
// tick that collected nothing ships no batch.
func TestSpoolSkipsAnEmptyGroup(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s, timers := timedSpool(pub)
	if err := s.add(&ogv1.TaskGroup{TaskId: "empty"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(timers.live()) != 0 {
		t.Fatal("an empty group armed the flush timer")
	}
	s.flush()
	waitIdle(t, s)
	if pub.attemptCount() != 0 {
		t.Fatalf("an empty group shipped %d batches", pub.attemptCount())
	}
}

// TestSpoolRetryShipsTheSameIdAndBytes: the batch id and bytes are minted
// once at the flush, so a publish that errors and is retried carries the same
// id and identical bytes, and the stream's duplicate window can recognize it.
func TestSpoolRetryShipsTheSameIdAndBytes(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var ids []string
	var datas [][]byte
	calls := 0
	publish := func(_ string, data []byte, id string) (ackWait, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		ids = append(ids, id)
		datas = append(datas, append([]byte(nil), data...))
		if calls == 1 {
			return nil, errors.New("stream away")
		}
		return answered(nil), nil
	}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", publish)
	now := time.Unix(1_000_000, 0)
	var nmu sync.Mutex
	s.now = func() time.Time { nmu.Lock(); defer nmu.Unlock(); return now }
	_ = s.add(tagged("a"))
	_ = s.add(tagged("b"))
	s.flush()
	waitIdle(t, s)
	nmu.Lock()
	now = now.Add(time.Second) // a retry a tick later must not re-stamp the batch
	nmu.Unlock()
	tick(t, s)
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 2 {
		t.Fatalf("%d publishes, want 2 (one failed, one retry)", len(ids))
	}
	if ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("retry ids %q then %q, want the same minted id", ids[0], ids[1])
	}
	if !bytes.Equal(datas[0], datas[1]) {
		t.Fatal("the retry carried different bytes than the first attempt")
	}
	if s.pending() != 0 {
		t.Fatalf("%d batches still held after the retry succeeded", s.pending())
	}
}

// TestSpoolSendFlushesPendingFirst: a whole batch (the node's own logs) sent
// while groups are pending ships after them, so the stream keeps the order
// things happened in.
func TestSpoolSendFlushesPendingFirst(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s, timers := timedSpool(pub)
	_ = s.add(tagged("g"))
	if err := s.send(&ogv1.TelemetryBatch{NodeId: "edge", Logs: []*ogv1.LogLine{{Message: "hello"}}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitIdle(t, s)
	batches := pub.shippedBatches(t)
	if len(batches) != 2 {
		t.Fatalf("shipped %d batches, want the pending groups then the logs", len(batches))
	}
	if len(batches[0].GetGroups()) != 1 || len(batches[1].GetLogs()) != 1 {
		t.Fatalf("order %v, want groups first then node logs", batches)
	}
	if len(timers.live()) != 0 {
		t.Fatal("the flush timer stayed armed after send flushed the pending groups")
	}
}

// TestSpoolDrainFlushesPending: a shutdown drain ships the groups still
// pending, not only the batches already queued.
func TestSpoolDrainFlushesPending(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s, _ := timedSpool(pub)
	_ = s.add(tagged("a"))
	_ = s.add(tagged("b"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got := fmt.Sprint(pub.tags()); got != "[a b]" {
		t.Fatalf("drain shipped %s, want [a b]", got)
	}
}

// TestSpoolTimerAfterAFlushIsANoOp: a timer that fires after its groups
// already flushed (at the threshold, or ahead of a send) ships nothing more.
func TestSpoolTimerAfterAFlushIsANoOp(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s, timers := timedSpool(pub)
	_ = s.add(tagged("a"))
	stale := timers.live()[0]
	s.flush()
	waitIdle(t, s)
	_ = s.add(tagged("b"))
	stale.fn() // the first interval's timer, firing late
	waitIdle(t, s)
	if got := fmt.Sprint(pub.tags()); got != "[a]" {
		t.Fatalf("a stale timer shipped %s, want only [a] (b waits for its own interval)", got)
	}
}

// TestSpoolShipsPushedSamplesAtOnce: a session's pushed samples are a change
// an endpoint just told the node about, so they flush at once, carrying the
// groups already pending with them, instead of waiting out the tick's timer.
func TestSpoolShipsPushedSamplesAtOnce(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{}
	s, _ := timedSpool(pub)
	if err := s.add(tagged("polled")); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := publishPushed(s, "pushed", []collection.Sample{{Name: "pushed", Value: 1, TS: time.Now()}}); err != nil {
		t.Fatalf("publish pushed: %v", err)
	}
	waitIdle(t, s)
	batches := pub.shippedBatches(t)
	if len(batches) != 1 || len(batches[0].GetGroups()) != 2 {
		t.Fatalf("shipped %d batches (%v), want one carrying the pending group and the pushed one, before any timer", len(batches), pub.tags())
	}
	if got := batches[0].GetGroups()[1].GetTaskId(); got != "pushed" {
		t.Fatalf("second group is task %q, want the pushed one", got)
	}
}
