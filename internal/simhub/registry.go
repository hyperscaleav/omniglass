// Package simhub is the server's view of the simulators running beside its
// nodes: what each can do, which devices it serves, and how often each device
// has been reached. It is in-memory and per server process; the hits are a
// live signal for the console, not telemetry, so nothing is persisted.
package simhub

import (
	"sort"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// MaxHitsPerBatch caps the hits one report may add; the rest are rejected.
// A simulator flushes about once a second, so this is far above a real
// fleet's rate and only stops a misbehaving credential.
const MaxHitsPerBatch = 10_000

// knownProtocols are the protocols a simulated device answers. A hit naming
// anything else is rejected rather than becoming a new map key.
var knownProtocols = map[string]bool{"http": true, "snmp": true, "ssh": true, "telnet": true, "tcp": true, "udp": true}

// DeviceHits is the hit tally for one simulated device.
type DeviceHits struct {
	Count        int
	ByProtocol   map[string]int
	LastAt       time.Time
	LastProtocol string
}

// Status is a snapshot of one node's simulator.
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

type nodeState struct {
	lastSeen time.Time
	impairs  collection.SimImpairmentStatus
	caps     collection.SimCapabilities
	listed   map[string]bool
	since    map[string]time.Time
	serving  map[string]bool
	devices  map[string]*DeviceHits
}

// Registry holds every node's simulator status. Safe for concurrent use.
type Registry struct {
	mu    sync.Mutex
	nodes map[string]*nodeState
	now   func() time.Time
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{nodes: map[string]*nodeState{}, now: time.Now}
}

// Pulled records a simulator's device-list pull: its capabilities, the time,
// and the components the server handed it. Hits are accepted only for those,
// and the tallies of components no longer listed are dropped.
func (r *Registry) Pulled(node string, caps collection.SimCapabilities, componentIDs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.nodes[node]
	if st == nil {
		st = &nodeState{serving: map[string]bool{}, devices: map[string]*DeviceHits{}}
		r.nodes[node] = st
	}
	st.lastSeen = r.now()
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

// Report records a hit batch from a node's simulator, keeping only devices
// the node was handed (a simulator cannot report on another node's
// components, or on components it invented), known protocols, and at most
// MaxHitsPerBatch hits. A hit is stamped with the time the server heard it,
// not the simulator's clock. It returns how many hits were accepted and
// rejected.
func (r *Registry) Report(node string, b collection.SimHitBatch) (accepted, rejected int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.nodes[node]
	if st == nil {
		return 0, len(b.Hits)
	}
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
		if !st.listed[h.ComponentID] || !knownProtocols[h.Protocol] || accepted == MaxHitsPerBatch {
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

// Nodes lists the nodes whose simulator has pulled, sorted.
func (r *Registry) Nodes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.nodes))
	for n := range r.nodes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Status returns a copy of a node's simulator status, or false when no
// simulator has pulled for that node since the server started.
func (r *Registry) Status(node string) (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.nodes[node]
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

// filterListed keeps only the components the node was handed: a simulator
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
