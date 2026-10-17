// Package simhub is the server's view of the simulators running beside its
// nodes: what each can do, which devices it serves, and how often each device
// has been reached. It is in-memory and per server process; the hits are a
// live signal for the console, not telemetry, so nothing is persisted.
package simhub

import (
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

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
	Devices      map[string]DeviceHits
}

type nodeState struct {
	lastSeen time.Time
	caps     collection.SimCapabilities
	listed   map[string]bool
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
	for _, id := range componentIDs {
		st.listed[id] = true
	}
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
// components, or on components it invented). It returns how many hits were
// accepted and rejected.
func (r *Registry) Report(node string, b collection.SimHitBatch) (accepted, rejected int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.nodes[node]
	if st == nil {
		return 0, len(b.Hits)
	}
	st.serving = map[string]bool{}
	for _, id := range b.Serving {
		if st.listed[id] {
			st.serving[id] = true
		}
	}
	for _, h := range b.Hits {
		if !st.listed[h.ComponentID] {
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
		if !h.At.Before(d.LastAt) {
			d.LastAt, d.LastProtocol = h.At, h.Protocol
		}
	}
	return accepted, rejected
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
		Devices:      make(map[string]DeviceHits, len(st.devices)),
	}
	for id := range st.serving {
		out.Serving[id] = true
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
