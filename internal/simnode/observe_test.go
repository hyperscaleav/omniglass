package simnode

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/obsstat"
	"github.com/hyperscaleav/omniglass/internal/sim"
)

func counters(received, answered int64) sim.ObservationCounters {
	return sim.ObservationCounters{Received: received, Answered: answered}
}

// The wire sums each protocol over the devices that serve it, counts the
// served devices that saw nothing on it, and names a few of them.
func TestObservationWireSumsProtocolsAndNamesUnhitDevices(t *testing.T) {
	at := time.Unix(1_000_000, 0)
	snap := sim.ObservationSnapshot{At: at, Started: at.Add(-time.Hour),
		Protocols: []sim.ProtocolObservation{{Protocol: "http", Latency: []uint64{3, 1},
			InFlightHigh: []obsstat.EpochHigh{{Epoch: 1, Max: 2}}}, {Protocol: "snmp"}},
		Devices: []sim.DeviceObservation{
			{DeviceID: "a", Rows: []sim.ProtocolCounters{{Protocol: "http", ObservationCounters: counters(3, 3)}, {Protocol: "snmp"}}},
			{DeviceID: "b", Rows: []sim.ProtocolCounters{{Protocol: "http", ObservationCounters: counters(1, 0)}}},
			{DeviceID: "c", Rows: []sim.ProtocolCounters{{Protocol: "http"}}},
		},
		Faces: []sim.FaceOverlap{{DeviceID: "a", Hostname: "dsp", Face: "api", Protocol: "http", Received: 3, Overlapped: 1}},
	}
	w := observationWire("edge", snap, 1)
	if w.Node != "edge" || w.Devices != 3 || w.Impaired != 1 || !w.At.Equal(at) {
		t.Fatalf("header = %+v", w)
	}
	h := w.Protocol("http")
	if h.Received != 4 || h.Answered != 3 || h.Served != 3 || h.Unhit != 1 || fmt.Sprint(h.UnhitSample) != "[c]" {
		t.Fatalf("http = %+v, want 4 received, 3 answered, 3 served, c unhit", h)
	}
	if len(h.Latency) != 2 || len(h.InFlightHigh) != 1 {
		t.Fatalf("http lost the engine-wide latency and high water: %+v", h)
	}
	if s := w.Protocol("snmp"); s.Served != 1 || s.Unhit != 1 || fmt.Sprint(s.UnhitSample) != "[a]" {
		t.Fatalf("snmp = %+v", s)
	}
	if len(w.Faces) != 1 || w.FacesOverlapped != 1 || w.Faces[0].ComponentID != "a" || w.Faces[0].Component != "dsp" {
		t.Fatalf("faces = %+v, want a (dsp)", w.Faces)
	}
}

// At the 3,000-system target, with every device unhit on every protocol and
// every face overlapped, the reply stays bounded far under the bus payload.
func TestObservationWireStaysBoundedAtFleetScale(t *testing.T) {
	var snap sim.ObservationSnapshot
	for _, p := range sim.ObservedProtocols() {
		snap.Protocols = append(snap.Protocols, sim.ProtocolObservation{Protocol: p, Latency: make([]uint64, 64),
			InFlightHigh: make([]obsstat.EpochHigh, 60)})
	}
	for i := range 3000 {
		d := sim.DeviceObservation{DeviceID: fmt.Sprintf("00000000-0000-0000-0000-%012d", i)}
		for _, p := range sim.ObservedProtocols() {
			d.Rows = append(d.Rows, sim.ProtocolCounters{Protocol: p})
			snap.Faces = append(snap.Faces, sim.FaceOverlap{DeviceID: d.DeviceID, Face: "api", Protocol: p, Received: 2, Overlapped: 1})
		}
		snap.Devices = append(snap.Devices, d)
	}
	w := observationWire("edge", snap, 3000)
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 64<<10 {
		t.Fatalf("a 3,000-device observation is %d bytes, want under 64 KiB", len(b))
	}
	if w.FacesOverlapped != 3000*len(sim.ObservedProtocols()) || len(w.Faces) != collection.SimObservationSampleCap {
		t.Fatalf("faces %d listed of %d", len(w.Faces), w.FacesOverlapped)
	}
	if h := w.Protocol("http"); h.Unhit != 3000 || len(h.UnhitSample) != collection.SimObservationSampleCap {
		t.Fatalf("http unhit %d, sample %d", h.Unhit, len(h.UnhitSample))
	}
}

// The simulator answers an observation request on its node's subject.
func TestSubscribeObservationAnswersOnTheNodesSubject(t *testing.T) {
	nc := startBus(t)
	engine := sim.NewEngine(nil, nil)
	sub, err := subscribeObservation(nc, "edge", engine, func() int { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	msg, err := nc.Request(collection.SimobsSubject("edge"), nil, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var w collection.SimObservation
	if err := json.Unmarshal(msg.Data, &w); err != nil {
		t.Fatal(err)
	}
	if w.Node != "edge" || w.At.IsZero() || len(w.Protocols) != len(sim.ObservedProtocols()) {
		t.Fatalf("reply = %+v", w)
	}
}

// startBus is a bare embedded nats server on a random port and a client to it.
func startBus(t *testing.T) *nats.Conn {
	t.Helper()
	srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("the nats server never came up")
	}
	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}
