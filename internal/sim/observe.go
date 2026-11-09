package sim

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/internal/obsstat"
)

// What an endpoint observed (#1639): every simulated endpoint counts what it
// served, so a lab can reconcile what a node sent against what reached the
// device and how the device answered, hop by hop. One endpointObservation
// per engine, shared by every device it serves; each device holds a pointer
// to its own fixed row, so the hot path is a fixed number of atomic adds, no
// map lookup and no allocation. Nothing resets on read: a reader brackets a
// window with two snapshots and takes the difference.
//
// Three units, matched one for one with the node's RequestTally:
//   - a connection, counted where a TCP face accepts it, with an open gauge;
//   - a request, one exchange (the unit each protocol logs as one RequestLog),
//     begun when it arrives and ended with one Outcome;
//   - a stream (an SSE or held http stream, a websocket upgrade), counted once
//     as opened with an open gauge, never a request in flight, so a held stream
//     cannot read as stuck.
//
// Memory ordering, so a reader never sees a false gap: begin raises in flight
// before it counts the request received; end counts the outcome before it
// lowers in flight; a reader takes received, then in flight, then outcomes.

// obsProtocol indexes the fixed per-protocol rows: the protocols a simulated
// device serves (collection's sim protocol list, in the same order).
type obsProtocol uint8

const (
	obsHTTP obsProtocol = iota
	obsSNMP
	obsSSH
	obsTelnet
	obsTCP
	obsUDP
	obsWebSocket
	obsProtocolCount
)

var obsProtocolNames = [obsProtocolCount]string{"http", "snmp", "ssh", "telnet", "tcp", "udp", "websocket"}

func (p obsProtocol) String() string { return obsProtocolNames[p] }

// ObservedProtocols is the protocols an observation counts, in row order.
func ObservedProtocols() []string { return append([]string(nil), obsProtocolNames[:]...) }

// Outcome is how one request ended, a closed set.
type Outcome uint8

const (
	// OutcomeAnswered: the device answered (any status that is not a refusal
	// or an error, a 404 included: the device told the client something).
	OutcomeAnswered Outcome = iota
	// OutcomeRefused: an auth or community refusal, an http 401 or 403, a
	// telnet silent reject.
	OutcomeRefused
	// OutcomeError: an http 5xx or a protocol error the device sent.
	OutcomeError
	// OutcomeDropped: the client went away before the answer was written.
	OutcomeDropped
	// OutcomeWithheld: the simulator chose not to answer (its own datagram
	// loss, a full source queue), apart from dropped so a planted fault never
	// reads as loss.
	OutcomeWithheld
	outcomeCount
)

var outcomeNames = [outcomeCount]string{"answered", "refused", "error", "dropped", "withheld"}

var allOutcomes = [outcomeCount]Outcome{OutcomeAnswered, OutcomeRefused, OutcomeError, OutcomeDropped, OutcomeWithheld}

func (o Outcome) String() string { return outcomeNames[o] }

// outcomeForStatus maps an http-shaped status to its outcome: 401 and 403
// refuse, 5xx is an error, anything else (404 and 400 included) answered.
func outcomeForStatus(status int) Outcome {
	switch {
	case status == 401 || status == 403:
		return OutcomeRefused
	case status >= 500:
		return OutcomeError
	}
	return OutcomeAnswered
}

// outcomeForLine maps a line protocol's status to its outcome: the device
// answered (200), or its answer said the command failed (unknown or broken).
func outcomeForLine(status int) Outcome {
	if status == 200 {
		return OutcomeAnswered
	}
	return OutcomeError
}

// protocolRow is one device's counters for one protocol.
type protocolRow struct {
	accepted        atomic.Int64
	openConnections atomic.Int64
	received        atomic.Int64
	inFlight        atomic.Int64
	outcomes        [outcomeCount]atomic.Int64
	streamsOpened   atomic.Int64
	openStreams     atomic.Int64
	overlapped      atomic.Int64
}

// engineProtocol is the engine-wide state for one protocol: the latency
// histogram and the in-flight high water, both summed or maxed across the
// engine's devices.
type engineProtocol struct {
	inFlight  atomic.Int64
	highWater obsstat.HighWaterRing
	latency   obsstat.LatencyHistogram
}

// faceObservation is one face's discrete in-flight count and the overlaps it
// saw: a begin that finds the face already serving a request counts one.
type faceObservation struct {
	deviceID   string
	name       string
	protocol   obsProtocol
	inFlight   atomic.Int32
	received   atomic.Int64
	overlapped atomic.Int64
}

// deviceObservation is one device's fixed row.
type deviceObservation struct {
	id     string
	engine *endpointObservation
	rows   [obsProtocolCount]protocolRow
	served [obsProtocolCount]atomic.Bool
	faceMu sync.Mutex
	faces  []*faceObservation
}

// endpointObservation is the engine's observation.
type endpointObservation struct {
	started   time.Time
	protocols [obsProtocolCount]engineProtocol
	mu        sync.Mutex
	devices   []*deviceObservation
	byID      map[string]*deviceObservation
}

func newEndpointObservation() *endpointObservation {
	return &endpointObservation{started: time.Now(), byID: map[string]*deviceObservation{}}
}

// device returns the row for a device id, creating it once: a device the
// engine rebuilds keeps counting into the same row.
func (o *endpointObservation) device(id string) *deviceObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	if d, ok := o.byID[id]; ok {
		return d
	}
	d := &deviceObservation{id: id, engine: o}
	o.devices = append(o.devices, d)
	o.byID[id] = d
	return d
}

// face returns the observation of one named face, creating it once.
func (d *deviceObservation) face(name string, p obsProtocol) *faceObservation {
	if d == nil {
		return nil
	}
	d.served[p].Store(true)
	d.faceMu.Lock()
	defer d.faceMu.Unlock()
	for _, f := range d.faces {
		if f.name == name && f.protocol == p {
			return f
		}
	}
	f := &faceObservation{deviceID: d.id, name: name, protocol: p}
	d.faces = append(d.faces, f)
	return f
}

// obsToken is one request in flight: a value, so begin allocates nothing.
type obsToken struct {
	row   *protocolRow
	eng   *engineProtocol
	face  *faceObservation
	start time.Time
}

// begin counts one request arriving on a protocol (and face, when known).
func (d *deviceObservation) begin(p obsProtocol, f *faceObservation) obsToken {
	if d == nil {
		return obsToken{}
	}
	now := time.Now()
	row, eng := &d.rows[p], &d.engine.protocols[p]
	row.inFlight.Add(1)
	eng.highWater.Observe(obsstat.EpochOf(now), eng.inFlight.Add(1))
	if f != nil {
		if f.inFlight.Add(1) > 1 {
			f.overlapped.Add(1)
			row.overlapped.Add(1)
		}
		f.received.Add(1)
	}
	row.received.Add(1)
	return obsToken{row: row, eng: eng, face: f, start: now}
}

// end settles the request with its outcome.
func (t obsToken) end(out Outcome) {
	if t.row == nil {
		return
	}
	t.row.outcomes[out].Add(1)
	t.eng.latency.Record(time.Since(t.start))
	if t.face != nil {
		t.face.inFlight.Add(-1)
	}
	t.eng.inFlight.Add(-1)
	t.row.inFlight.Add(-1)
}

// accept counts one connection accepted and returns its close, which lowers
// the open gauge once however often it is called.
func (d *deviceObservation) accept(p obsProtocol) func() {
	if d == nil {
		return func() {}
	}
	d.connected(p)
	var once sync.Once
	return func() { once.Do(func() { d.disconnected(p) }) }
}

// connected counts one connection accepted, for a listener whose hooks
// already see each connection's end exactly once (net/http's ConnState).
func (d *deviceObservation) connected(p obsProtocol) {
	if d == nil {
		return
	}
	d.rows[p].openConnections.Add(1)
	d.rows[p].accepted.Add(1)
}

// disconnected lowers the open gauge for one connection ended.
func (d *deviceObservation) disconnected(p obsProtocol) {
	if d == nil {
		return
	}
	d.rows[p].openConnections.Add(-1)
}

// openStream counts one stream opened and returns its close.
func (d *deviceObservation) openStream(p obsProtocol) func() {
	if d == nil {
		return func() {}
	}
	row := &d.rows[p]
	row.openStreams.Add(1)
	row.streamsOpened.Add(1)
	var once sync.Once
	return func() { once.Do(func() { row.openStreams.Add(-1) }) }
}

// ObservationCounters is one device's counters for one protocol, read in the
// documented order.
type ObservationCounters struct {
	Accepted        int64 `json:"accepted"`
	OpenConnections int64 `json:"open_connections"`
	Received        int64 `json:"received"`
	InFlight        int64 `json:"in_flight"`
	Answered        int64 `json:"answered"`
	Refused         int64 `json:"refused"`
	Error           int64 `json:"error"`
	Dropped         int64 `json:"dropped"`
	Withheld        int64 `json:"withheld"`
	StreamsOpened   int64 `json:"streams_opened"`
	OpenStreams     int64 `json:"open_streams"`
	Overlapped      int64 `json:"overlapped"`
}

// Outcome reads one outcome's count.
func (c ObservationCounters) Outcome(o Outcome) int64 {
	switch o {
	case OutcomeAnswered:
		return c.Answered
	case OutcomeRefused:
		return c.Refused
	case OutcomeError:
		return c.Error
	case OutcomeDropped:
		return c.Dropped
	case OutcomeWithheld:
		return c.Withheld
	}
	return 0
}

// Settled is every outcome counted.
func (c ObservationCounters) Settled() int64 {
	return c.Answered + c.Refused + c.Error + c.Dropped + c.Withheld
}

// IsZero reports a row nothing touched.
func (c ObservationCounters) IsZero() bool { return c == ObservationCounters{} }

func (d *deviceObservation) counters(p obsProtocol) ObservationCounters {
	if d == nil {
		return ObservationCounters{}
	}
	r := &d.rows[p]
	var c ObservationCounters
	c.Accepted = r.accepted.Load()
	c.OpenConnections = r.openConnections.Load()
	c.Received = r.received.Load()
	c.InFlight = r.inFlight.Load()
	c.Answered = r.outcomes[OutcomeAnswered].Load()
	c.Refused = r.outcomes[OutcomeRefused].Load()
	c.Error = r.outcomes[OutcomeError].Load()
	c.Dropped = r.outcomes[OutcomeDropped].Load()
	c.Withheld = r.outcomes[OutcomeWithheld].Load()
	c.StreamsOpened = r.streamsOpened.Load()
	c.OpenStreams = r.openStreams.Load()
	c.Overlapped = r.overlapped.Load()
	return c
}

// ObservationSnapshot is one read of an engine's observation.
type ObservationSnapshot struct {
	At        time.Time
	Started   time.Time
	Protocols []ProtocolObservation
	Devices   []DeviceObservation
	// Faces lists only the faces that saw an overlap, so the snapshot stays
	// small at fleet scale; the per-protocol overlap totals are on each row.
	Faces []FaceOverlap
}

// ProtocolObservation is the engine-wide view of one protocol.
type ProtocolObservation struct {
	Protocol string
	// Latency is the service-time histogram's bucket counts (see
	// latencyBucket), trailing empty buckets trimmed.
	Latency []uint64
	// InFlightHigh is the in-flight high water per 10 s epoch, the last ten
	// minutes of them.
	InFlightHigh []obsstat.EpochHigh
}

// HighWater is the largest in-flight count in any epoch overlapping
// [from, to].
func (p ProtocolObservation) HighWater(from, to time.Time) int64 {
	return obsstat.HighWater(p.InFlightHigh, from, to)
}

// DeviceObservation is one device's rows, only the protocols it serves or
// that saw traffic.
type DeviceObservation struct {
	DeviceID string
	Rows     []ProtocolCounters
}

// ProtocolCounters is one row of a device observation.
type ProtocolCounters struct {
	Protocol string
	ObservationCounters
}

// FaceOverlap is one face that saw two requests in flight at once.
type FaceOverlap struct {
	DeviceID   string
	Face       string
	Protocol   string
	Received   int64
	Overlapped int64
}

// Protocol returns the named protocol's engine-wide view.
func (s ObservationSnapshot) Protocol(name string) ProtocolObservation {
	for _, p := range s.Protocols {
		if p.Protocol == name {
			return p
		}
	}
	return ProtocolObservation{Protocol: name}
}

func (o *endpointObservation) snapshot(at time.Time) ObservationSnapshot {
	o.mu.Lock()
	devices := append([]*deviceObservation(nil), o.devices...)
	o.mu.Unlock()
	s := ObservationSnapshot{At: at, Started: o.started}
	for p := range obsProtocolCount {
		eng := &o.protocols[p]
		s.Protocols = append(s.Protocols, ProtocolObservation{
			Protocol: obsProtocolNames[p], Latency: eng.latency.Counts(), InFlightHigh: eng.highWater.Epochs(),
		})
	}
	for _, d := range devices {
		row := DeviceObservation{DeviceID: d.id}
		for p := range obsProtocolCount {
			c := d.counters(p)
			if c.IsZero() && !d.served[p].Load() {
				continue
			}
			row.Rows = append(row.Rows, ProtocolCounters{Protocol: obsProtocolNames[p], ObservationCounters: c})
		}
		s.Devices = append(s.Devices, row)
		d.faceMu.Lock()
		for _, f := range d.faces {
			if n := f.overlapped.Load(); n > 0 {
				s.Faces = append(s.Faces, FaceOverlap{DeviceID: d.id, Face: f.name, Protocol: obsProtocolNames[f.protocol],
					Received: f.received.Load(), Overlapped: n})
			}
		}
		d.faceMu.Unlock()
	}
	return s
}

// obsProtocolOf names a protocol back to its index.
func obsProtocolOf(name string) (obsProtocol, bool) {
	for p, n := range obsProtocolNames {
		if n == name {
			return obsProtocol(p), true
		}
	}
	return 0, false
}

// Observed is what the device's faces of one protocol saw: connections,
// requests and how each settled. An unknown protocol reads zero.
func (d *Device) Observed(protocol string) ObservationCounters {
	p, ok := obsProtocolOf(protocol)
	if !ok || d.observe == nil {
		return ObservationCounters{}
	}
	return d.observe.counters(p)
}

// Observation reads every device's counters, the per-protocol latency and
// in-flight high water, and the faces that saw an overlap.
func (e *Engine) Observation() ObservationSnapshot {
	return e.obs.snapshot(time.Now())
}
