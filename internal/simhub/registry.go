// Package simhub is the server's view of the simulators running beside its
// outposts: what each can do, which devices it serves, and how often each device
// has been reached. It is in-memory and per server process; the hits are a
// live signal for the console, not telemetry, so nothing is persisted.
package simhub

import (
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// MaxHitsPerBatch caps the hits one report may add; the rest are rejected.
// A simulator flushes about once a second, so this is far above a real
// fleet's rate and only stops a misbehaving credential.
const MaxHitsPerBatch = 10_000

// SilenceTTL is how long an outpost's simulator may go without pulling or
// reporting before the registry drops it: ten of the simulator's default
// ten-second pulls. A simulator torn down without its outpost being deleted
// ages out instead of reading offline until the server restarts.
const SilenceTTL = 100 * time.Second

// maxToldProtocols bounds the notices the hub gives of protocols it does not
// count, and the memory of which it has given: a credential reporting made-up
// protocols can neither flood the log nor grow the registry.
const maxToldProtocols = 16

// DeviceHits is the hit tally for one simulated device.
type DeviceHits struct {
	Count        int
	ByProtocol   map[string]int
	LastAt       time.Time
	LastProtocol string
}

// Status is a snapshot of one outpost's simulator.
type Status struct {
	LastSeen     time.Time
	Capabilities collection.SimCapabilities
	Serving      map[string]bool
	// Listed is the components the server handed the simulator on its
	// latest pull: one listed but not serving failed to start; one not
	// listed was added since.
	Listed map[string]bool
	// ListedSince is when each listed component was first handed over, so
	// one handed over a moment ago reads as starting, not failed, until the
	// simulator's next report.
	ListedSince map[string]time.Time
	Devices     map[string]DeviceHits
	// Impairments is the simulator's latest account of impairments: whether
	// it can apply them, its last error, and what it impairs now.
	Impairments collection.SimImpairmentStatus
}

type outpostState struct {
	lastSeen time.Time
	// heardAt is the latest pull or report: what the TTL measures, where
	// lastSeen (the pull alone) is what the console reads as connected.
	heardAt time.Time
	impairs collection.SimImpairmentStatus
	caps    collection.SimCapabilities
	listed  map[string]bool
	since   map[string]time.Time
	serving map[string]bool
	devices map[string]*DeviceHits
}

// Registry holds every outpost's simulator status. Safe for concurrent use.
type Registry struct {
	mu       sync.Mutex
	outposts map[string]*outpostState
	now      func() time.Time
	// told is the outpost and protocol pairs the hub has said it dropped hits
	// of, so it says so once for each (at most maxToldProtocols).
	told map[string]bool
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{outposts: map[string]*outpostState{}, now: time.Now, told: map[string]bool{}}
}

// tell says once, for each outpost and protocol, that the hub drops hits of a
// protocol it does not count (#1366: the websocket hits the hub rejected
// without a word, so a component read as having none). The protocol is
// clipped and made valid UTF-8, since the simulator's report is not trusted
// with the log.
func (r *Registry) tell(outpost, protocol string) {
	protocol = strings.ToValidUTF8(protocol, "?")
	if len(protocol) > 40 {
		protocol = protocol[:40] + "..."
	}
	key := outpost + "\x00" + protocol
	if r.told[key] || len(r.told) >= maxToldProtocols {
		return
	}
	r.told[key] = true
	slog.Warn("simulator hit dropped: the hub does not count this protocol", "facility", "simulation",
		"outpost", outpost, "protocol", protocol)
}

// Pulled records a simulator's device-list pull: its capabilities, the time,
// and the components the server handed it. Hits are accepted only for those,
// and the tallies of components no longer listed are dropped.
func (r *Registry) Pulled(outpost string, caps collection.SimCapabilities, componentIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expire()
	st := r.outposts[outpost]
	if st == nil {
		st = &outpostState{serving: map[string]bool{}, devices: map[string]*DeviceHits{}}
		r.outposts[outpost] = st
	}
	st.lastSeen = r.now()
	st.heardAt = st.lastSeen
	st.caps = caps
	st.listed = make(map[string]bool, len(componentIDs))
	since := make(map[string]time.Time, len(componentIDs))
	for _, id := range componentIDs {
		st.listed[id] = true
		if t, ok := st.since[id]; ok {
			since[id] = t
		} else {
			since[id] = st.lastSeen
		}
	}
	st.since = since
	for id := range st.devices {
		if !st.listed[id] {
			delete(st.devices, id)
		}
	}
	for id := range st.serving {
		if !st.listed[id] {
			delete(st.serving, id)
		}
	}
}

// Report records a hit batch from an outpost's simulator, keeping only devices
// the outpost was handed (a simulator cannot report on another outpost's
// components, or on components it invented), known protocols, and at most
// MaxHitsPerBatch hits. A hit is stamped with the time the server heard it,
// not the simulator's clock. It returns how many hits were accepted and
// rejected.
func (r *Registry) Report(outpost string, b collection.SimHitBatch) (accepted, rejected int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expire()
	st := r.outposts[outpost]
	if st == nil {
		return 0, len(b.Hits)
	}
	st.heardAt = r.now()
	if b.Impairments != nil {
		st.impairs = *b.Impairments
		st.impairs.Impaired = filterListed(st.listed, b.Impairments.Impaired)
	}
	st.serving = map[string]bool{}
	for _, id := range b.Serving {
		if st.listed[id] {
			st.serving[id] = true
		}
	}
	now := r.now()
	for _, h := range b.Hits {
		known := collection.IsSimProtocol(h.Protocol)
		if st.listed[h.ComponentID] && !known {
			r.tell(outpost, h.Protocol)
		}
		if !st.listed[h.ComponentID] || !known || accepted == MaxHitsPerBatch {
			rejected++
			continue
		}
		accepted++
		d := st.devices[h.ComponentID]
		if d == nil {
			d = &DeviceHits{ByProtocol: map[string]int{}}
			st.devices[h.ComponentID] = d
		}
		d.Count++
		d.ByProtocol[h.Protocol]++
		d.LastAt, d.LastProtocol = now, h.Protocol
	}
	return accepted, rejected
}

// Forget drops an outpost's entry, for an outpost that was deleted. A simulator that
// still runs for it is taken as new on its next pull.
func (r *Registry) Forget(outpost string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.outposts, outpost)
}

// expire drops every outpost unheard for SilenceTTL. The caller holds mu.
func (r *Registry) expire() {
	now := r.now()
	for n, st := range r.outposts {
		if now.Sub(st.heardAt) > SilenceTTL {
			delete(r.outposts, n)
		}
	}
}

// Outposts lists the outposts whose simulator has pulled and been heard from within
// SilenceTTL, sorted.
func (r *Registry) Outposts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expire()
	out := make([]string, 0, len(r.outposts))
	for n := range r.outposts {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Status returns a copy of an outpost's simulator status, or false when no
// simulator has pulled for that outpost since the server started, or it has been
// forgotten or silent for SilenceTTL.
func (r *Registry) Status(outpost string) (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expire()
	st := r.outposts[outpost]
	if st == nil {
		return Status{}, false
	}
	out := Status{
		LastSeen:     st.lastSeen,
		Capabilities: st.caps,
		Serving:      make(map[string]bool, len(st.serving)),
		Listed:       make(map[string]bool, len(st.listed)),
		ListedSince:  make(map[string]time.Time, len(st.since)),
		Impairments:  st.impairs,
		Devices:      make(map[string]DeviceHits, len(st.devices)),
	}
	for id := range st.serving {
		out.Serving[id] = true
	}
	for id := range st.listed {
		out.Listed[id] = true
	}
	for id, t := range st.since {
		out.ListedSince[id] = t
	}
	for id, d := range st.devices {
		cp := *d
		cp.ByProtocol = make(map[string]int, len(d.ByProtocol))
		for k, v := range d.ByProtocol {
			cp.ByProtocol[k] = v
		}
		out.Devices[id] = cp
	}
	return out, true
}

// filterListed keeps only the components the outpost was handed: a simulator
// reports on its own devices, never others.
func filterListed(listed map[string]bool, ids []string) []string {
	var out []string
	for _, id := range ids {
		if listed[id] {
			out = append(out, id)
		}
	}
	return out
}
