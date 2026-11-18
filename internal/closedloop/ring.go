package closedloop

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The server's closed loop reads a sliding window rather than a since-start
// total (ADR-1647): a bracket of every outpost with a simulator each bracket
// interval, RingSpan intervals of them making the window, and one more after
// its end to count the window's receipts at. RingEvery is the default bracket
// interval and the longest a ring takes; a lab brackets faster
// (OMNIGLASS_CLOSED_LOOP_BRACKET), so its window fills in seconds.
const (
	RingEvery = time.Minute
	RingSpan  = 10
)

// ReceiptMargin is how far before a window's start an outpost's receipts are
// counted: the stream's ack wait (telemetryAckWait in internal/bus), the
// longest a batch the bus acknowledged after the start read can have landed
// before it.
const ReceiptMargin = 30 * time.Second

// How an outpost's window reads.
const (
	// WindowWarmingUp: the ring does not yet hold a window and the bracket
	// after it, so there is nothing to read.
	WindowWarmingUp = "warming up"
	// WindowMeasured: the window is read.
	WindowMeasured = "measured"
	// WindowRestarted: the outpost or its sim started again inside the window,
	// or the fleet the sim serves changed (#1895), so no figure for it is
	// meaningful until the window slides past.
	WindowRestarted = "restarted"
)

// DeviceRead is one simulated device's hits since its sim started, by
// protocol, and whether the sim impairs it now.
type DeviceRead struct {
	ByProtocol map[string]int64
	Impaired   bool
}

// Source takes the reads a ring brackets: an outpost's bracketed observation and
// its devices' hits, the enabled poll targets by outpost, and an outpost's receipts
// landed in [from, next], split by start and end as ReceiptCount says.
type Source interface {
	Observe(ctx context.Context, outpost string) (collection.EndpointObservation, map[string]DeviceRead, error)
	Targets(ctx context.Context) (map[string][]PollTarget, error)
	Receipts(ctx context.Context, outpost string, from, start, end, next time.Time) (ReceiptCount, error)
}

// DeviceWindow is one simulated device over the window: the hits it saw by
// protocol, whether the sim impairs it at the window's end, and whether it
// is a poll target the sim names as never hit.
type DeviceWindow struct {
	ComponentID string           `json:"component_id"`
	Received    map[string]int64 `json:"received"`
	Impaired    bool             `json:"impaired"`
	Unhit       bool             `json:"unhit"`
}

// OutpostWindow is one outpost's read of the ring. Bracket is what Gaps was read
// from, for a caller summarizing the window (SummarizeEndpoints). Err is the
// last bracket's failure; the window stays the last one read.
type OutpostWindow struct {
	Outpost  string
	State    string
	Start    time.Time
	End      time.Time
	Brackets int
	Bracket  OutpostBracket
	Gaps     []GapRatio
	Devices  []DeviceWindow
	Err      string
}

// ringBracket is one outpost's reads at one tick.
type ringBracket struct {
	at      time.Time
	obs     collection.EndpointObservation
	devices map[string]DeviceRead
}

// Ring holds each outpost's most recent brackets and the window read off them.
// Safe for concurrent use.
type Ring struct {
	src   Source
	now   func() time.Time
	span  int
	every time.Duration

	mu       sync.Mutex
	brackets map[string][]ringBracket
	windows  map[string]OutpostWindow
}

// NewRing is a ring over src, its window span brackets of every long. An
// every of zero or past RingEvery is RingEvery, so a window never reads past
// the receipt horizon.
func NewRing(src Source, now func() time.Time, span int, every time.Duration) *Ring {
	if every <= 0 || every > RingEvery {
		every = RingEvery
	}
	return &Ring{src: src, now: now, span: max(span, 1), every: every, brackets: map[string][]ringBracket{}, windows: map[string]OutpostWindow{}}
}

// Every is how often the ring brackets each outpost.
func (r *Ring) Every() time.Duration { return r.every }

// Length is a full window's length.
func (r *Ring) Length() time.Duration { return time.Duration(r.span) * r.every }

// Run takes a bracket of the outposts listed now, then every bracket interval,
// until ctx ends.
func (r *Ring) Run(ctx context.Context, outposts func() []string) {
	t := time.NewTicker(r.every)
	defer t.Stop()
	for {
		r.Take(ctx, outposts())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Take brackets each listed outpost and reads its window. An outpost no longer
// listed is dropped, so one that comes back warms up again.
func (r *Ring) Take(ctx context.Context, outposts []string) {
	r.mu.Lock()
	for n := range r.brackets {
		if !slices.Contains(outposts, n) {
			delete(r.brackets, n)
			delete(r.windows, n)
		}
	}
	r.mu.Unlock()
	if len(outposts) == 0 {
		return
	}
	targets, terr := r.src.Targets(ctx)
	for _, n := range outposts {
		at := r.now()
		obs, devices, err := r.src.Observe(ctx, n)
		r.mu.Lock()
		w := r.windows[n]
		w.Outpost = n
		if err != nil {
			w.Err = err.Error()
			if w.State == "" {
				w.State = WindowWarmingUp
			}
			r.windows[n] = w
			r.mu.Unlock()
			continue
		}
		bs := append(r.brackets[n], ringBracket{at: at, obs: obs, devices: devices})
		if len(bs) > r.span+2 {
			bs = bs[len(bs)-(r.span+2):]
		}
		r.brackets[n] = bs
		r.mu.Unlock()

		w = OutpostWindow{Outpost: n, State: WindowWarmingUp, Brackets: len(bs)}
		if len(bs) >= 3 {
			w = r.read(ctx, n, bs, targets[n])
		}
		if terr != nil && w.Err == "" {
			w.Err = "poll targets: " + terr.Error()
		}
		r.mu.Lock()
		r.windows[n] = w
		r.mu.Unlock()
	}
}

// read is the window over bs: the oldest bracket to the one before the
// newest, its receipts counted up to the newest.
func (r *Ring) read(ctx context.Context, outpost string, bs []ringBracket, targets []PollTarget) OutpostWindow {
	first, end, next := bs[0], bs[len(bs)-2], bs[len(bs)-1]
	b := OutpostBracket{Outpost: outpost, Start: first.obs, End: end.obs, Targets: targets, Window: end.at.Sub(first.at)}
	for _, m := range bs[1 : len(bs)-2] {
		b.Between = append(b.Between, m.obs.SimBefore, m.obs.SimAfter)
	}
	w := OutpostWindow{Outpost: outpost, State: WindowMeasured, Start: first.at, End: end.at, Brackets: len(bs)}
	rc, err := r.src.Receipts(ctx, outpost, first.at.Add(-ReceiptMargin), first.at, end.at, next.at)
	if err != nil {
		w.Err = "receipts: " + err.Error()
	}
	b.Receipts = rc
	w.Bracket = b
	w.Gaps = Ratios(b)
	if restarted(b) {
		w.State = WindowRestarted
	}
	unhitIDs := map[string]bool{}
	for _, g := range Reconcile(b) {
		if g.Gap == GapUnhitEndpoint && g.ComponentID != "" {
			unhitIDs[g.ComponentID] = true
		}
	}
	w.Devices = devicesOver(first.devices, end.devices, unhitIDs)
	return w
}

// devicesOver is each device's hits between two reads, by protocol; a count
// that went backwards (the sim started again) reads from zero.
func devicesOver(from, to map[string]DeviceRead, unhit map[string]bool) []DeviceWindow {
	out := make([]DeviceWindow, 0, len(to))
	for id, d := range to {
		dw := DeviceWindow{ComponentID: id, Received: map[string]int64{}, Impaired: d.Impaired, Unhit: unhit[id]}
		for p, n := range d.ByProtocol {
			was := from[id].ByProtocol[p]
			if n < was {
				was = 0
			}
			dw.Received[p] = n - was
		}
		out = append(out, dw)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ComponentID < out[j].ComponentID })
	return out
}

// Window is one outpost's read.
func (r *Ring) Window(outpost string) (OutpostWindow, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.windows[outpost]
	return w, ok
}

// Windows is every outpost's read, by outpost.
func (r *Ring) Windows() []OutpostWindow {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]OutpostWindow, 0, len(r.windows))
	for _, w := range r.windows {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Outpost < out[j].Outpost })
	return out
}
