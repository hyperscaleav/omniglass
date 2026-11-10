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
// total (ADR-1647): a bracket of every node with a simulator each RingEvery,
// RingSpan intervals of them making the window, and one more after its end
// to count the window's receipts at.
const (
	RingEvery = time.Minute
	RingSpan  = 10
)

// ReceiptMargin is how far before a window's start a node's receipts are
// counted: the stream's ack wait (telemetryAckWait in internal/bus), the
// longest a batch the bus acknowledged after the start read can have landed
// before it.
const ReceiptMargin = 30 * time.Second

// How a node's window reads.
const (
	// WindowWarmingUp: the ring does not yet hold a window and the bracket
	// after it, so there is nothing to read.
	WindowWarmingUp = "warming up"
	// WindowMeasured: the window is read.
	WindowMeasured = "measured"
	// WindowRestarted: the node or its sim started again inside the window,
	// so no figure for it is meaningful until the window slides past.
	WindowRestarted = "restarted"
)

// DeviceRead is one simulated device's hits since its sim started, by
// protocol, and whether the sim impairs it now.
type DeviceRead struct {
	ByProtocol map[string]int64
	Impaired   bool
}

// Source takes the reads a ring brackets: a node's bracketed observation and
// its devices' hits, the enabled poll targets by node, and a node's receipts
// landed in [from, next], split by start and end as ReceiptCount says.
type Source interface {
	Observe(ctx context.Context, node string) (collection.EndpointObservation, map[string]DeviceRead, error)
	Targets(ctx context.Context) (map[string][]PollTarget, error)
	Receipts(ctx context.Context, node string, from, start, end, next time.Time) (ReceiptCount, error)
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

// NodeWindow is one node's read of the ring. Bracket is what Gaps was read
// from, for a caller summarizing the window (SummarizeEndpoints). Err is the
// last bracket's failure; the window stays the last one read.
type NodeWindow struct {
	Node     string
	State    string
	Start    time.Time
	End      time.Time
	Brackets int
	Bracket  NodeBracket
	Gaps     []GapRatio
	Devices  []DeviceWindow
	Err      string
}

// ringBracket is one node's reads at one tick.
type ringBracket struct {
	at      time.Time
	obs     collection.EndpointObservation
	devices map[string]DeviceRead
}

// Ring holds each node's most recent brackets and the window read off them.
// Safe for concurrent use.
type Ring struct {
	src  Source
	now  func() time.Time
	span int

	mu       sync.Mutex
	brackets map[string][]ringBracket
	windows  map[string]NodeWindow
}

// NewRing is a ring over src, its window span brackets long.
func NewRing(src Source, now func() time.Time, span int) *Ring {
	return &Ring{src: src, now: now, span: max(span, 1), brackets: map[string][]ringBracket{}, windows: map[string]NodeWindow{}}
}

// Length is a full window's length.
func (r *Ring) Length() time.Duration { return time.Duration(r.span) * RingEvery }

// Run takes a bracket of the nodes listed now, then every RingEvery, until
// ctx ends.
func (r *Ring) Run(ctx context.Context, nodes func() []string) {
	t := time.NewTicker(RingEvery)
	defer t.Stop()
	for {
		r.Take(ctx, nodes())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Take brackets each listed node and reads its window. A node no longer
// listed is dropped, so one that comes back warms up again.
func (r *Ring) Take(ctx context.Context, nodes []string) {
	r.mu.Lock()
	for n := range r.brackets {
		if !slices.Contains(nodes, n) {
			delete(r.brackets, n)
			delete(r.windows, n)
		}
	}
	r.mu.Unlock()
	if len(nodes) == 0 {
		return
	}
	targets, terr := r.src.Targets(ctx)
	for _, n := range nodes {
		at := r.now()
		obs, devices, err := r.src.Observe(ctx, n)
		r.mu.Lock()
		w := r.windows[n]
		w.Node = n
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

		w = NodeWindow{Node: n, State: WindowWarmingUp, Brackets: len(bs)}
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
func (r *Ring) read(ctx context.Context, node string, bs []ringBracket, targets []PollTarget) NodeWindow {
	first, end, next := bs[0], bs[len(bs)-2], bs[len(bs)-1]
	b := NodeBracket{Node: node, Start: first.obs, End: end.obs, Targets: targets, Window: end.at.Sub(first.at)}
	for _, m := range bs[1 : len(bs)-2] {
		b.Between = append(b.Between, m.obs.SimBefore, m.obs.SimAfter)
	}
	w := NodeWindow{Node: node, State: WindowMeasured, Start: first.at, End: end.at, Brackets: len(bs)}
	rc, err := r.src.Receipts(ctx, node, first.at.Add(-ReceiptMargin), first.at, end.at, next.at)
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

// Window is one node's read.
func (r *Ring) Window(node string) (NodeWindow, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.windows[node]
	return w, ok
}

// Windows is every node's read, by node.
func (r *Ring) Windows() []NodeWindow {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]NodeWindow, 0, len(r.windows))
	for _, w := range r.windows {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Node < out[j].Node })
	return out
}
