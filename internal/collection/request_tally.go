package collection

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/hyperscaleav/omniglass/internal/obsstat"
)

// The node's request tally (#1639): the node half of the closed loop a scale
// lab reconciles against what each simulated endpoint observed. A transport
// counts a connection when the connect (and any TLS handshake) succeeds and a
// request where its bytes leave the node: an http request once its connection
// is up, an snmp datagram once the socket took it. A tally is plain counters,
// injected through the node's config, never a package global, and never
// published as metric rows: the lab pulls it on a subject the node answers.
// TODO(#1661): tally the session and stream exchanges (ssh, telnet, tcp
// session lines, udp, websocket, the http session and stream transports),
// which reach the sim untallied today.

// TallyTransport names a transport the tally counts. The names match the
// simulator's protocols, so the two sides reconcile row for row.
type TallyTransport uint8

const (
	TallyHTTP TallyTransport = iota
	TallySNMP
	TallySSH
	TallyTelnet
	TallyTCP
	TallyUDP
	TallyWebSocket
	tallyTransportCount
)

var tallyTransportNames = [tallyTransportCount]string{"http", "snmp", "ssh", "telnet", "tcp", "udp", "websocket"}

func (t TallyTransport) String() string {
	if t < tallyTransportCount {
		return tallyTransportNames[t]
	}
	return "unknown"
}

// TallyOutcome is how a request the node sent settled.
type TallyOutcome uint8

const (
	// TallyAnswered: an answer came back.
	TallyAnswered TallyOutcome = iota
	// TallyRefused: the component refused the credential (401, 403,
	// authorizationError).
	TallyRefused
	// TallyError: the component answered with a server error, or the
	// exchange failed some other way after the bytes left.
	TallyError
	// TallyTimedOut: no answer came back in time.
	TallyTimedOut
	tallyOutcomeCount
)

var tallyOutcomeNames = [tallyOutcomeCount]string{"answered", "refused", "error", "timed out"}

func (o TallyOutcome) String() string {
	if o < tallyOutcomeCount {
		return tallyOutcomeNames[o]
	}
	return "unknown"
}

type transportTally struct {
	connections atomic.Int64
	sent        atomic.Int64
	inFlight    atomic.Int64
	outcomes    [tallyOutcomeCount]atomic.Int64
}

// gaugeTally is a gauge a pool reports, with its high water per epoch; set
// says a pool reported it at all.
type gaugeTally struct {
	set     atomic.Bool
	current atomic.Int64
	high    obsstat.HighWaterRing
}

func (g *gaugeTally) store(n int64) {
	g.current.Store(n)
	g.high.Observe(obsstat.EpochOf(time.Now()), n)
	g.set.Store(true)
}

func (g *gaugeTally) read() *GaugeReading {
	if !g.set.Load() {
		return nil
	}
	return &GaugeReading{Current: g.current.Load(), HighWater: g.high.Epochs()}
}

// RequestTally counts what one node sent. Every method is safe on a nil
// tally, which counts nothing.
type RequestTally struct {
	started             time.Time
	transports          [tallyTransportCount]transportTally
	batchesAcknowledged atomic.Int64
	batchesDropped      atomic.Int64
	queueDepth          gaugeTally
	busyWorkers         gaugeTally
}

// NewRequestTally returns an empty tally started now.
func NewRequestTally() *RequestTally { return &RequestTally{started: time.Now()} }

// Connected counts one connection the transport opened.
func (t *RequestTally) Connected(tr TallyTransport) {
	if t == nil {
		return
	}
	t.transports[tr].connections.Add(1)
}

// TallyToken is one request in flight; End settles it.
type TallyToken struct{ tr *transportTally }

// Begin counts one request sent and in flight. It raises in flight before
// sent, so a read in the documented order never shows a request sent that is
// neither in flight nor settled.
func (t *RequestTally) Begin(tr TallyTransport) TallyToken {
	if t == nil {
		return TallyToken{}
	}
	tt := &t.transports[tr]
	tt.inFlight.Add(1)
	tt.sent.Add(1)
	return TallyToken{tr: tt}
}

// End settles the request: the outcome is counted before in flight is
// lowered.
func (k TallyToken) End(o TallyOutcome) {
	if k.tr == nil {
		return
	}
	k.tr.outcomes[o].Add(1)
	k.tr.inFlight.Add(-1)
}

// BatchAcknowledged counts one telemetry batch the bus acknowledged.
func (t *RequestTally) BatchAcknowledged() {
	if t != nil {
		t.batchesAcknowledged.Add(1)
	}
}

// BatchDropped counts one telemetry batch the spool gave up on.
func (t *RequestTally) BatchDropped() {
	if t != nil {
		t.batchesDropped.Add(1)
	}
}

// SetQueueDepth reports the depth of the queue of work due and not started.
func (t *RequestTally) SetQueueDepth(n int64) {
	if t != nil {
		t.queueDepth.store(n)
	}
}

// SetBusyWorkers reports how many workers are mid-task.
func (t *RequestTally) SetBusyWorkers(n int64) {
	if t != nil {
		t.busyWorkers.store(n)
	}
}

// TallyCounters is one transport's counts at a read.
type TallyCounters struct {
	Transport   string `json:"transport"`
	Connections int64  `json:"connections"`
	Sent        int64  `json:"sent"`
	InFlight    int64  `json:"in_flight"`
	Answered    int64  `json:"answered"`
	Refused     int64  `json:"refused"`
	Error       int64  `json:"error"`
	TimedOut    int64  `json:"timed_out"`
}

// Settled is the requests that reached an outcome.
func (c TallyCounters) Settled() int64 { return c.Answered + c.Refused + c.Error + c.TimedOut }

// Outcome reads one outcome's count.
func (c TallyCounters) Outcome(o TallyOutcome) int64 {
	switch o {
	case TallyAnswered:
		return c.Answered
	case TallyRefused:
		return c.Refused
	case TallyError:
		return c.Error
	case TallyTimedOut:
		return c.TimedOut
	}
	return 0
}

// Counters reads one transport in the documented order: connections, sent,
// in flight, then the outcomes.
func (t *RequestTally) Counters(tr TallyTransport) TallyCounters {
	c := TallyCounters{Transport: tr.String()}
	if t == nil {
		return c
	}
	tt := &t.transports[tr]
	c.Connections = tt.connections.Load()
	c.Sent = tt.sent.Load()
	c.InFlight = tt.inFlight.Load()
	c.Answered = tt.outcomes[TallyAnswered].Load()
	c.Refused = tt.outcomes[TallyRefused].Load()
	c.Error = tt.outcomes[TallyError].Load()
	c.TimedOut = tt.outcomes[TallyTimedOut].Load()
	return c
}

// GaugeReading is a gauge's current value and its high water per epoch.
type GaugeReading struct {
	Current   int64               `json:"current"`
	HighWater []obsstat.EpochHigh `json:"high_water,omitempty"`
}

// High is the gauge's largest reading in an epoch overlapping [from, to].
func (g *GaugeReading) High(from, to time.Time) int64 {
	if g == nil {
		return 0
	}
	return obsstat.HighWater(g.HighWater, from, to)
}

// RequestTallySnapshot is a whole tally at one read. Transports carries only
// those the node touched; a gauge no pool reported is nil.
type RequestTallySnapshot struct {
	Node                string          `json:"node"`
	At                  time.Time       `json:"at"`
	Started             time.Time       `json:"started"`
	Transports          []TallyCounters `json:"transports,omitempty"`
	BatchesAcknowledged int64           `json:"batches_acknowledged"`
	BatchesDropped      int64           `json:"batches_dropped"`
	QueueDepth          *GaugeReading   `json:"queue_depth,omitempty"`
	BusyWorkers         *GaugeReading   `json:"busy_workers,omitempty"`
}

// Snapshot reads the whole tally.
func (t *RequestTally) Snapshot(at time.Time) RequestTallySnapshot {
	s := RequestTallySnapshot{At: at}
	if t == nil {
		return s
	}
	s.Started = t.started
	for tr := range tallyTransportCount {
		c := t.Counters(tr)
		if c.Connections != 0 || c.Sent != 0 {
			s.Transports = append(s.Transports, c)
		}
	}
	s.BatchesAcknowledged = t.batchesAcknowledged.Load()
	s.BatchesDropped = t.batchesDropped.Load()
	s.QueueDepth = t.queueDepth.read()
	s.BusyWorkers = t.busyWorkers.read()
	return s
}

// tallyCall is one request through a transport whose dial runs apart from
// the caller (net/http dials on its own goroutine): the dial begins the
// request once its connection is up, and the caller settles it. A request
// settled before its dial began is never counted, so a dial a cancelled
// request leaves behind cannot strand one in flight.
type tallyCall struct {
	t     *RequestTally
	tr    TallyTransport
	mu    sync.Mutex
	tok   TallyToken
	begun bool
	done  bool
}

func (t *RequestTally) call(tr TallyTransport) *tallyCall {
	if t == nil {
		return nil
	}
	return &tallyCall{t: t, tr: tr}
}

func (c *tallyCall) connected() {
	if c != nil {
		c.t.Connected(c.tr)
	}
}

func (c *tallyCall) begin() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.done && !c.begun {
		c.tok, c.begun = c.t.Begin(c.tr), true
	}
}

func (c *tallyCall) settle(o TallyOutcome) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.done = true
	if c.begun {
		c.tok.End(o)
	}
}

// statusOutcome reads an http status the way the simulator does: 401 and 403
// refused, a server error an error, anything else answered.
func statusOutcome(status int) TallyOutcome {
	switch {
	case status == 401 || status == 403:
		return TallyRefused
	case status >= 500:
		return TallyError
	}
	return TallyAnswered
}

// errOutcome reads a failed exchange after its bytes left: a deadline is a
// timeout, anything else an error.
func errOutcome(err error) TallyOutcome {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, gosnmp.ErrMaxRetriesExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return TallyTimedOut
	}
	return TallyError
}
