package collection

import (
	"time"

	"github.com/hyperscaleav/omniglass/internal/obsstat"
)

// The endpoint observation's wire (#1639): what the simulator beside a node
// saw arrive at its devices, and what the node tallied sending, each read on
// request so a scale lab can reconcile the two. Both are request-reply on the
// node's own grant, the server requesting, like a simulator operation. Both
// replies are bounded whatever the fleet: per-protocol totals, a capped sample
// of device ids, never a row per device.

// SimobsSubject is where the server asks the simulator beside a node for its
// endpoint observation.
func SimobsSubject(node string) string { return subjectPrefix + "simobs." + node }

// RequestTallySubject is where the server asks a node for its request tally.
func RequestTallySubject(node string) string { return subjectPrefix + "tally." + node }

// SimObservationSampleCap bounds the device ids an observation names per
// protocol, and the overlapping faces it lists, so the reply stays well under
// the bus's payload at any fleet size.
const SimObservationSampleCap = 20

// SimObservation is one read of a simulator's endpoint observation.
type SimObservation struct {
	Node    string    `json:"node"`
	At      time.Time `json:"at"`
	Started time.Time `json:"started"`
	// Devices is how many devices the simulator serves; Impaired how many of
	// them carry an applied impairment.
	Devices   int                      `json:"devices"`
	Impaired  int                      `json:"impaired"`
	Protocols []SimProtocolObservation `json:"protocols"`
	// Faces lists up to SimObservationSampleCap faces that saw two requests
	// at once; FacesOverlapped is how many did.
	Faces           []SimFaceOverlap `json:"faces,omitempty"`
	FacesOverlapped int              `json:"faces_overlapped,omitempty"`
}

// SimProtocolObservation is one protocol summed over every device that
// serves it.
type SimProtocolObservation struct {
	Protocol        string `json:"protocol"`
	Accepted        int64  `json:"accepted,omitempty"`
	OpenConnections int64  `json:"open_connections,omitempty"`
	Received        int64  `json:"received,omitempty"`
	InFlight        int64  `json:"in_flight,omitempty"`
	Answered        int64  `json:"answered,omitempty"`
	Refused         int64  `json:"refused,omitempty"`
	Error           int64  `json:"error,omitempty"`
	Dropped         int64  `json:"dropped,omitempty"`
	Withheld        int64  `json:"withheld,omitempty"`
	StreamsOpened   int64  `json:"streams_opened,omitempty"`
	OpenStreams     int64  `json:"open_streams,omitempty"`
	Overlapped      int64  `json:"overlapped,omitempty"`
	// Latency is the service-time histogram's bucket counts.
	Latency []uint64 `json:"latency,omitempty"`
	// InFlightHigh is the in-flight high water per epoch.
	InFlightHigh []obsstat.EpochHigh `json:"in_flight_high,omitempty"`
	// Served is how many devices serve the protocol; Unhit how many of them
	// saw nothing on it (no connection, request or stream), UnhitSample up to
	// SimObservationSampleCap of their ids.
	Served      int      `json:"served,omitempty"`
	Unhit       int      `json:"unhit,omitempty"`
	UnhitSample []string `json:"unhit_sample,omitempty"`
}

// Settled is the requests that reached an outcome.
func (p SimProtocolObservation) Settled() int64 {
	return p.Answered + p.Refused + p.Error + p.Dropped + p.Withheld
}

// SimFaceOverlap is one face that saw two requests in flight at once, on
// the component the simulated device stands in for.
type SimFaceOverlap struct {
	ComponentID string `json:"component_id"`
	Component   string `json:"component"`
	Face        string `json:"face"`
	Protocol    string `json:"protocol"`
	Received    int64  `json:"received"`
	Overlapped  int64  `json:"overlapped"`
}

// Protocol returns the named protocol's row, zero when the simulator has none.
func (o SimObservation) Protocol(name string) SimProtocolObservation {
	for _, p := range o.Protocols {
		if p.Protocol == name {
			return p
		}
	}
	return SimProtocolObservation{Protocol: name}
}

// EndpointObservation is the three reads a lab reconciles, taken in order:
// the simulator, then the node, then the simulator again. The first sim read
// is the floor of what arrived before the node's count; the second the
// ceiling after it, so a request in transit between the two reads is never
// a false gap.
type EndpointObservation struct {
	SimBefore SimObservation       `json:"sim_before"`
	Node      RequestTallySnapshot `json:"node"`
	SimAfter  SimObservation       `json:"sim_after"`
}
