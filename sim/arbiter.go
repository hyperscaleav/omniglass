package sim

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/sim/impair"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The impairment arbiter (#2799) decides what a simulated device suffers when
// several sources want something of it at once: the operator's impairment route,
// the scripted problems of the lab calendar (#2769) and, next, scenes (#2314).
// Each source registers its wants under its own name and releases only its own;
// the arbiter resolves them the same way every time and is the only writer to
// the two seams that make a device suffer: the behavior-override set
// (SetBehaviorOverrides, WritePath) and the network impairer.
//
// The resolution rules:
//
//   - unplugged if any source wants it;
//   - for loss, latency and jitter the highest wanted, for a rate limit the
//     lowest (the tightest);
//   - for one state path the newest pin wins. A pin is as new as the moment its
//     source first held that path on that device. The losers are kept, and as a
//     winner releases the next newest takes the path; when the last releases the
//     path goes back to what it read before any pin held it.

// Pin holds a state path at the value an expression renders.
type Pin struct {
	Path, Value string
}

// Want is what one source asks of one device.
type Want struct {
	// Impairment is the network's: unplugged, delay, jitter, loss, a rate limit.
	Impairment impair.Impairment
	// Since is when the source began wanting the device unplugged; zero is the
	// moment the arbiter first saw it.
	Since time.Time
	// Pins hold state paths. A path held twice by one source is held by the last.
	Pins []Pin
}

func (w Want) empty() bool { return w.Impairment == (impair.Impairment{}) && len(w.Pins) == 0 }

// NetSink is what applies the network's resolved wants: the simulator's
// impairer. It is called with the served devices (component id to address) and
// the resolved impairment of each device that has one.
type NetSink interface {
	Reconcile(served map[string]netip.Addr, wants map[string]impair.Impairment)
}

// pinHeld is one source's pin on one path of one device.
type pinHeld struct {
	source, value string
	seq           uint64
}

// applied is what the arbiter last wrote for one path of one device.
type applied struct{ source, value string }

type pathKey struct{ device, path string }

type devState struct {
	prev    map[string]any     // path -> what it read before any pin held it
	applied map[string]applied // path -> the winner written now
}

// Arbiter is one per simulator engine. Safe for concurrent use.
type Arbiter struct {
	clock func() time.Time

	// settleMu serializes Settle and Shutdown, which call into devices and the
	// sink; mu guards the wants and is never held across such a call, since a
	// device that reads the arbiter (Offline) holds its own lock when it does.
	settleMu sync.Mutex
	mu       sync.Mutex
	wants    map[string]map[string]Want // source -> device id -> want
	since    map[string]map[string]time.Time
	pinSeq   map[string]map[pathKey]uint64 // source -> (device, path) -> seq
	next     uint64
	devs     map[string]*devState
	hostID   map[string]string // hostname -> device id, as of the last settle
	derived  []derivation
	sink     NetSink
}

type derivation struct {
	name string
	fn   func(world.Listing, time.Time) (int64, bool)
}

// NewArbiter is an arbiter that stamps unplugged-since with the clock; nil is
// time.Now.
func NewArbiter(clock func() time.Time) *Arbiter {
	if clock == nil {
		clock = time.Now
	}
	return &Arbiter{
		clock:  clock,
		wants:  map[string]map[string]Want{},
		since:  map[string]map[string]time.Time{},
		pinSeq: map[string]map[pathKey]uint64{},
		devs:   map[string]*devState{},
		hostID: map[string]string{},
	}
}

// SetNet gives the arbiter the sink that applies the network; nil applies none.
func (a *Arbiter) SetNet(s NetSink) {
	a.mu.Lock()
	a.sink = s
	a.mu.Unlock()
}

// Derive registers a reader-side derivation: a source whose unplugged-ness the
// world can compute for any listed device, served by this simulator or not (the
// scripted problems on the calendar are derived this way, central by
// derivation). Registering a name again replaces it.
func (a *Arbiter) Derive(name string, fn func(l world.Listing, now time.Time) (since int64, unplugged bool)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.derived {
		if a.derived[i].name == name {
			a.derived[i].fn = fn
			return
		}
	}
	a.derived = append(a.derived, derivation{name, fn})
}

// Set replaces everything source wants, by device id: devices left out, and a
// device given an empty want, are released. Other sources are untouched.
// Nothing is applied until Settle.
func (a *Arbiter) Set(source string, wants map[string]Want) {
	a.mu.Lock()
	defer a.mu.Unlock()
	kept := map[string]Want{}
	ids := make([]string, 0, len(wants))
	for id, w := range wants {
		if !w.empty() {
			kept[id] = w
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	seqs := a.pinSeq[source]
	nextSeqs := map[pathKey]uint64{}
	for _, id := range ids {
		pins := lastPerPath(kept[id].Pins)
		w := kept[id]
		w.Pins = pins
		kept[id] = w
		for _, p := range pins {
			k := pathKey{id, p.Path}
			if s, ok := seqs[k]; ok {
				nextSeqs[k] = s
				continue
			}
			a.next++
			nextSeqs[k] = a.next
		}
	}
	sinces := map[string]time.Time{}
	for _, id := range ids {
		w := kept[id]
		if !w.Impairment.Unplugged {
			continue
		}
		switch {
		case !w.Since.IsZero():
			sinces[id] = w.Since
		case !a.since[source][id].IsZero():
			sinces[id] = a.since[source][id]
		default:
			sinces[id] = a.clock()
		}
	}
	if len(kept) == 0 {
		delete(a.wants, source)
		delete(a.since, source)
		delete(a.pinSeq, source)
		return
	}
	a.wants[source] = kept
	a.since[source] = sinces
	a.pinSeq[source] = nextSeqs
}

func lastPerPath(pins []Pin) []Pin {
	idx := map[string]int{}
	var out []Pin
	for _, p := range pins {
		if i, ok := idx[p.Path]; ok {
			out[i] = p
			continue
		}
		idx[p.Path] = len(out)
		out = append(out, p)
	}
	return out
}

// Release drops everything source wants. Other sources are untouched.
func (a *Arbiter) Release(source string) { a.Set(source, nil) }

// Sources is the names of the sources holding something now, sorted.
func (a *Arbiter) Sources() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.wants))
	for s := range a.wants {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// resolveNet folds every source's network wants for one device.
func resolveNet(ws []impair.Impairment) impair.Impairment {
	var out impair.Impairment
	for _, w := range ws {
		out.Unplugged = out.Unplugged || w.Unplugged
		out.LatencyMs = max(out.LatencyMs, w.LatencyMs)
		out.JitterMs = max(out.JitterMs, w.JitterMs)
		out.LossPct = max(out.LossPct, w.LossPct)
		if w.RateKbit > 0 && (out.RateKbit == 0 || w.RateKbit < out.RateKbit) {
			out.RateKbit = w.RateKbit
		}
	}
	return out
}

// Resolved is the network each device with a want suffers now, by device id.
func (a *Arbiter) Resolved() map[string]impair.Impairment {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resolvedLocked()
}

func (a *Arbiter) resolvedLocked() map[string]impair.Impairment {
	per := map[string][]impair.Impairment{}
	for _, byDev := range a.wants {
		for id, w := range byDev {
			if w.Impairment != (impair.Impairment{}) {
				per[id] = append(per[id], w.Impairment)
			}
		}
	}
	out := make(map[string]impair.Impairment, len(per))
	for id, ws := range per {
		out[id] = resolveNet(ws)
	}
	return out
}

// pinsOn is the pins held on one device, newest first within a path.
func (a *Arbiter) winnersLocked(id string) map[string]pinHeld {
	best := map[string]pinHeld{}
	for source, byDev := range a.wants {
		w, ok := byDev[id]
		if !ok {
			continue
		}
		for _, p := range w.Pins {
			h := pinHeld{source: source, value: p.Value, seq: a.pinSeq[source][pathKey{id, p.Path}]}
			if cur, ok := best[p.Path]; !ok || h.seq > cur.seq {
				best[p.Path] = h
			}
		}
	}
	return best
}

// Settle writes what the wants resolve to: each device's pins through the
// override seam, each path put back when its last pin releases, and the network
// to the sink. It returns the resolved network by device id. Devices not
// listed are forgotten.
func (a *Arbiter) Settle(devices []*Device) (map[string]impair.Impairment, []error) {
	a.settleMu.Lock()
	defer a.settleMu.Unlock()
	errs := a.settlePins(devices)
	a.mu.Lock()
	net, sink := a.resolvedLocked(), a.sink
	a.mu.Unlock()
	if sink != nil {
		served := map[string]netip.Addr{}
		for _, d := range devices {
			if ip, ok := netip.AddrFromSlice(d.LoopbackIP.To4()); ok {
				served[d.DeviceID] = ip
			}
		}
		sink.Reconcile(served, net)
	}
	return net, errs
}

// Shutdown releases every source and puts every pinned path back, for a
// simulator that stops. The network is the sink's to clear.
func (a *Arbiter) Shutdown(devices []*Device) []error {
	a.settleMu.Lock()
	defer a.settleMu.Unlock()
	a.mu.Lock()
	a.wants = map[string]map[string]Want{}
	a.since = map[string]map[string]time.Time{}
	a.pinSeq = map[string]map[pathKey]uint64{}
	a.mu.Unlock()
	return a.settlePins(devices)
}

func (a *Arbiter) settlePins(devices []*Device) []error {
	var errs []error
	live := map[string]bool{}
	hosts := make(map[string]string, len(devices))
	for _, d := range devices {
		live[d.DeviceID] = true
		hosts[d.Hostname] = d.DeviceID
	}
	a.mu.Lock()
	a.hostID = hosts
	a.mu.Unlock()
	for _, d := range devices {
		a.mu.Lock()
		want := a.winnersLocked(d.DeviceID)
		a.mu.Unlock()
		errs = append(errs, a.settleDevice(d, want)...)
	}
	for id := range a.devs { // only Settle and Shutdown touch devs, under settleMu
		if !live[id] {
			delete(a.devs, id)
		}
	}
	return errs
}

func (a *Arbiter) settleDevice(d *Device, want map[string]pinHeld) []error {
	st := a.devs[d.DeviceID]
	if st == nil {
		if len(want) == 0 {
			return nil
		}
		st = &devState{prev: map[string]any{}, applied: map[string]applied{}}
		a.devs[d.DeviceID] = st
	}
	same := len(want) == len(st.applied)
	for path, h := range want {
		if cur, ok := st.applied[path]; !ok || cur.source != h.source || cur.value != h.value {
			same = false
		}
	}
	if same {
		if len(want) == 0 {
			delete(a.devs, d.DeviceID)
		}
		return nil
	}
	paths := make([]string, 0, len(want))
	for p := range want {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var errs []error
	specs := make([]BehaviorOverrideSpec, 0, len(paths))
	for _, p := range paths {
		if _, held := st.prev[p]; !held {
			st.prev[p] = d.ReadPath(p)
		}
		h := want[p]
		specs = append(specs, BehaviorOverrideSpec{OverrideID: h.source, Path: p, Expression: h.value, IntervalMs: int32(pinInterval / time.Millisecond)})
	}
	if err := d.SetBehaviorOverrides(specs); err != nil {
		return []error{fmt.Errorf("a pin did not apply on %s: %w", d.Hostname, err)}
	}
	next := map[string]applied{}
	for _, p := range paths {
		h := want[p]
		next[p] = applied{h.source, h.value}
		if cur, ok := st.applied[p]; ok && cur == next[p] {
			continue
		}
		if err := d.WritePath(h.source, p, h.value); err != nil {
			errs = append(errs, fmt.Errorf("a pin did not apply on %s: %w", d.Hostname, err))
		}
	}
	for p, was := range st.applied {
		if _, still := next[p]; still {
			continue
		}
		prev := st.prev[p]
		delete(st.prev, p)
		if prev == nil || strings.Contains(fmt.Sprint(prev), "{{") {
			continue
		}
		if err := d.WritePath(was.source, p, fmt.Sprint(prev)); err != nil {
			errs = append(errs, fmt.Errorf("a pin did not release on %s: %w", d.Hostname, err))
		}
	}
	st.applied = next
	if len(next) == 0 {
		delete(a.devs, d.DeviceID)
	}
	return errs
}

// pinInterval is how often a held pin rewrites its path, so a write by
// anything else is undone within it.
const pinInterval = 10 * time.Second

// Offline says whether the listed device is unplugged now, and since when
// (unix seconds, the earliest of the sources that have it so): any source
// registered with the arbiter, or any derivation. It is what the meeting
// platform's listing reads, so an operator's unplug reads the same as a
// scripted one.
func (a *Arbiter) Offline(l world.Listing, now time.Time) (int64, bool) {
	if a == nil {
		return 0, false
	}
	a.mu.Lock()
	derived := append([]derivation(nil), a.derived...)
	var since int64
	found := false
	take := func(s int64) {
		if !found || s < since {
			since, found = s, true
		}
	}
	if id, ok := a.hostID[l.Name]; ok {
		for source, byDev := range a.wants {
			if w, ok := byDev[id]; ok && w.Impairment.Unplugged {
				take(a.since[source][id].Unix())
			}
		}
	}
	a.mu.Unlock()
	for _, dv := range derived { // outside the lock: a derivation reads the engine
		if s, ok := dv.fn(l, now); ok {
			take(s)
		}
	}
	return since, found
}
