package closedloop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
)

// fakeSource scripts a node's reads by tick: tick k carries 60k http
// requests and 6k batches on top of a base, every one of them carried.
type fakeSource struct {
	tick       int
	impairedAt map[int]bool // ticks whose sim reads carry an applied impairment
	restartAt  int          // the tick the node's sim and node start again; 0 for never
	fail       bool
	receipts   []receiptCall
}

type receiptCall struct {
	node                   string
	from, start, end, next time.Time
}

func (f *fakeSource) Observe(_ context.Context, node string) (collection.EndpointObservation, map[string]DeviceRead, error) {
	if f.fail {
		return collection.EndpointObservation{}, nil, errors.New("node did not answer")
	}
	k, started := int64(f.tick), simStarted
	base := int64(100)
	if f.restartAt > 0 && f.tick >= f.restartAt {
		k, started, base = int64(f.tick-f.restartAt), simStarted.Add(time.Hour), 0
	}
	at := windowStart.Add(time.Duration(f.tick) * time.Minute)
	sim := simRead(at, served("http", base+60*k, base+60*k))
	sim.Started = started
	if f.impairedAt[f.tick] {
		sim.Impaired = 1
	}
	obs := collection.EndpointObservation{
		SimBefore: sim,
		Node: collection.RequestTallySnapshot{Node: node, At: at, Started: started, BatchesAcknowledged: base/10 + 6*k,
			Transports: []collection.TallyCounters{sent("http", base+60*k, base+60*k)}},
		SimAfter: sim,
	}
	devices := map[string]DeviceRead{
		"c-http": {ByProtocol: map[string]int64{"http": base/10 + 6*k}, Impaired: f.impairedAt[f.tick]},
	}
	return obs, devices, nil
}

func (f *fakeSource) Targets(context.Context) (map[string][]PollTarget, error) {
	return map[string][]PollTarget{"edge": {{ComponentID: "c-http", Transport: "http", Interval: 2 * time.Second}}}, nil
}

// Receipts answers a node's acknowledged batches over the window, all landed.
func (f *fakeSource) Receipts(_ context.Context, node string, from, start, end, next time.Time) (ReceiptCount, error) {
	f.receipts = append(f.receipts, receiptCall{node, from, start, end, next})
	return ReceiptCount{Landed: int64(end.Sub(start)/time.Minute) * 6}, nil
}

// ringAt is a ring over src whose clock reads the source's tick.
func ringAt(src *fakeSource, span int) *Ring {
	return NewRing(src, func() time.Time { return windowStart.Add(time.Duration(src.tick) * time.Minute) }, span)
}

// take advances the source to tick and takes a bracket of edge.
func take(t *testing.T, r *Ring, src *fakeSource, tick int) NodeWindow {
	t.Helper()
	src.tick = tick
	r.Take(context.Background(), []string{"edge"})
	w, ok := r.Window("edge")
	if !ok {
		t.Fatalf("tick %d: no window for edge", tick)
	}
	return w
}

// healthy is whether every gap with something in reads a 0 loss ratio.
func healthy(t *testing.T, w NodeWindow) {
	t.Helper()
	if len(w.Gaps) == 0 {
		t.Fatalf("window %+v has no gaps", w)
	}
	for _, g := range w.Gaps {
		if g.Reading != ReadingLoss || g.Count != 0 || (g.In > 0 && (g.Ratio == nil || *g.Ratio != 0)) {
			t.Fatalf("gap %+v, want a healthy 0", g)
		}
	}
}

// A ring holding too few brackets to have a window and the one after it
// reads warming up, with no gap to read.
func TestRingWarmsUp(t *testing.T) {
	src := &fakeSource{}
	r := ringAt(src, 3)
	for tick := range 2 {
		if w := take(t, r, src, tick); w.State != WindowWarmingUp || w.Gaps != nil {
			t.Fatalf("tick %d: %+v, want warming up and no gaps", tick, w)
		}
	}
	if w := take(t, r, src, 2); w.State != WindowMeasured {
		t.Fatalf("tick 2: %+v, want measured", w)
	}
}

// The window runs from the oldest bracket to the one before the newest, and
// slides as brackets arrive once the ring is full.
func TestRingWindowSlides(t *testing.T) {
	src := &fakeSource{}
	r := ringAt(src, 3)
	at := func(tick int) time.Time { return windowStart.Add(time.Duration(tick) * time.Minute) }
	for tick, want := range []struct{ start, end int }{2: {0, 1}, 3: {0, 2}, 4: {0, 3}, 5: {1, 4}, 6: {2, 5}} {
		w := take(t, r, src, tick)
		if tick < 2 {
			continue
		}
		if !w.Start.Equal(at(want.start)) || !w.End.Equal(at(want.end)) {
			t.Fatalf("tick %d: window %s to %s, want tick %d to %d", tick, w.Start, w.End, want.start, want.end)
		}
		healthy(t, w)
		if sent := row(w.Gaps, GapSentNotReceived, "http"); sent.In != int64(60*(want.end-want.start)) {
			t.Fatalf("tick %d: http sent %d, want the window's", tick, sent.In)
		}
		if d := w.Devices; len(d) != 1 || d[0].ComponentID != "c-http" || d[0].Received["http"] != int64(6*(want.end-want.start)) {
			t.Fatalf("tick %d: devices %+v, want c-http's window delta", tick, d)
		}
	}
}

// Receipts for a window are counted at the bracket after its end: from the
// margin before its start to the next bracket, so a batch acknowledged just
// before the end has had a bracket's time to land.
func TestRingCountsReceiptsAtTheNextBracket(t *testing.T) {
	src := &fakeSource{}
	r := ringAt(src, 3)
	for tick := range 6 {
		take(t, r, src, tick)
	}
	at := func(tick int) time.Time { return windowStart.Add(time.Duration(tick) * time.Minute) }
	got := src.receipts[len(src.receipts)-1]
	want := receiptCall{"edge", at(1).Add(-ReceiptMargin), at(1), at(4), at(5)}
	if got != want {
		t.Fatalf("receipts read %+v, want %+v", got, want)
	}
	w, _ := r.Window("edge")
	if w.Bracket.Receipts.Landed != 18 {
		t.Fatalf("bracket receipts %+v, want the source's 18", w.Bracket.Receipts)
	}
}

// An impairment applied at a bracket inside the window, even one between its
// ends, reads impaired; once the window has slid past it, the gaps read 0.
func TestRingImpairmentInsideTheWindow(t *testing.T) {
	src := &fakeSource{impairedAt: map[int]bool{2: true}}
	r := ringAt(src, 3)
	var w NodeWindow
	for tick := range 5 {
		w = take(t, r, src, tick)
	}
	if !w.Start.Equal(windowStart) || w.Gaps[0].Reading != ReadingImpaired {
		t.Fatalf("window over the impaired tick: %+v, want impaired", w)
	}
	for tick := 5; tick < 8; tick++ {
		w = take(t, r, src, tick)
	}
	healthy(t, w)
	if w.Devices[0].Impaired {
		t.Fatalf("device %+v reads impaired after repair", w.Devices[0])
	}
}

// Counters going backwards read restarted for the windows holding the
// restart only.
func TestRingRestartReadsForThatWindowOnly(t *testing.T) {
	src := &fakeSource{restartAt: 3}
	r := ringAt(src, 2)
	for tick := range 3 {
		take(t, r, src, tick)
	}
	w := take(t, r, src, 3)
	if w.State != WindowMeasured {
		t.Fatalf("tick 3: %+v, want measured", w)
	}
	w = take(t, r, src, 4)
	if w.State != WindowRestarted || len(w.Gaps) != 1 || w.Gaps[0].Reading != ReadingRestarted {
		t.Fatalf("tick 4 over the restart: %+v, want restarted", w)
	}
	for tick := 5; tick < 7; tick++ {
		w = take(t, r, src, tick)
	}
	if w.State != WindowMeasured {
		t.Fatalf("tick 6, window past the restart: %+v, want measured", w)
	}
	healthy(t, w)
}

// A node whose read fails keeps its brackets and says why; a node no longer
// listed is dropped, so one that comes back warms up again.
func TestRingFailedReadAndDroppedNode(t *testing.T) {
	src := &fakeSource{}
	r := ringAt(src, 2)
	for tick := range 3 {
		take(t, r, src, tick)
	}
	src.fail = true
	w := take(t, r, src, 3)
	if w.Err == "" || w.State != WindowMeasured || !w.End.Equal(windowStart.Add(time.Minute)) {
		t.Fatalf("failed read: %+v, want the last window and the error", w)
	}
	src.fail = false
	r.Take(context.Background(), nil)
	if _, ok := r.Window("edge"); ok {
		t.Fatal("a node no longer listed kept its window")
	}
	if w := take(t, r, src, 5); w.State != WindowWarmingUp {
		t.Fatalf("back again: %+v, want warming up", w)
	}
}

// A full window with its margin is far inside the horizon the server keeps
// receipts for, so its earliest receipts are never pruned before it is read.
func TestRingWindowIsInsideTheReceiptHorizon(t *testing.T) {
	r := NewRing(&fakeSource{}, time.Now, RingSpan)
	if w := r.Length() + RingEvery + ReceiptMargin; w >= bus.ReceiptHorizon {
		t.Fatalf("a full window reads %s back, past the %s receipt horizon", w, bus.ReceiptHorizon)
	}
	if r.Length() != RingSpan*RingEvery {
		t.Fatalf("Length = %s, want %d brackets of %s", r.Length(), RingSpan, RingEvery)
	}
}
