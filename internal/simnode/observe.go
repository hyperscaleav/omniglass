package simnode

import (
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim"
)

// observationWire folds an engine's observation into its bounded wire form
// (#1639): each protocol summed over its devices, the served devices that saw
// nothing on it counted with a capped sample of their ids, and a capped list
// of the faces that saw an overlap. Pure.
func observationWire(node string, snap sim.ObservationSnapshot, impaired int) collection.SimObservation {
	w := collection.SimObservation{Node: node, At: snap.At, Started: snap.Started, Devices: len(snap.Devices), Impaired: impaired}
	index := map[string]int{}
	for _, p := range snap.Protocols {
		index[p.Protocol] = len(w.Protocols)
		w.Protocols = append(w.Protocols, collection.SimProtocolObservation{Protocol: p.Protocol, Latency: p.Latency, InFlightHigh: p.InFlightHigh})
	}
	for _, d := range snap.Devices {
		for _, r := range d.Rows {
			i, ok := index[r.Protocol]
			if !ok {
				index[r.Protocol] = len(w.Protocols)
				i = len(w.Protocols)
				w.Protocols = append(w.Protocols, collection.SimProtocolObservation{Protocol: r.Protocol})
			}
			p := &w.Protocols[i]
			c := r.ObservationCounters
			p.Accepted += c.Accepted
			p.OpenConnections += c.OpenConnections
			p.Received += c.Received
			p.InFlight += c.InFlight
			p.Answered += c.Answered
			p.Refused += c.Refused
			p.Error += c.Error
			p.Dropped += c.Dropped
			p.Withheld += c.Withheld
			p.StreamsOpened += c.StreamsOpened
			p.OpenStreams += c.OpenStreams
			p.Overlapped += c.Overlapped
			p.Served++
			if c.Accepted == 0 && c.Received == 0 && c.StreamsOpened == 0 {
				p.Unhit++
				if len(p.UnhitSample) < collection.SimObservationSampleCap {
					p.UnhitSample = append(p.UnhitSample, d.DeviceID)
				}
			}
		}
	}
	w.FacesOverlapped = len(snap.Faces)
	for _, f := range snap.Faces[:min(len(snap.Faces), collection.SimObservationSampleCap)] {
		w.Faces = append(w.Faces, collection.SimFaceOverlap{ComponentID: f.DeviceID, Component: f.Hostname,
			Face: f.Face, Protocol: f.Protocol, Received: f.Received, Overlapped: f.Overlapped})
	}
	return w
}

// subscribeObservation answers the server's observation requests for this
// node's simulator with a fresh read of the engine. impaired reports how many
// devices carry an applied impairment.
func subscribeObservation(nc *nats.Conn, name string, engine *sim.Engine, impaired func() int) (*nats.Subscription, error) {
	return nc.Subscribe(collection.SimobsSubject(name), func(m *nats.Msg) {
		snap := engine.Observation()
		if snap.At.IsZero() {
			snap.At = time.Now()
		}
		b, err := json.Marshal(observationWire(name, snap, impaired()))
		if err != nil {
			return
		}
		_ = m.Respond(b)
	})
}
