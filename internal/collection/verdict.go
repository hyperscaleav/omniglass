package collection

import (
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// The interface reachability verdict: a built-in STATE sample, the AND of the
// raw probe results (tcp-open, icmp-reachable). endpoint-reachable is gated
// per interface and its value domain is up/down; availability is time_in_state
// over it. The node computes it and emits it as a state sample; the ingest
// consumer routes it to state by its property_type lane membership.
const (
	// SignalEndpointReachable is the seeded state key the verdict lands under.
	SignalEndpointReachable = probekeys.EndpointReachable
	// VerdictUp / VerdictDown are the state's value domain.
	VerdictUp   = "up"
	VerdictDown = "down"
)

// A probe's result is a state, not a measurement (ADR-1592): a target was
// reached or it was not, and the fact is worth a row only when it changes. So
// icmp-reachable and tcp-open ride the property lane, each in the domain's own
// words, and the transition guard in storage keeps a steady probe to one row
// however often it runs. The timing beside each (icmp-rtt-avg,
// tcp-connect-time) stays a metric: a number, kept on every observation.
const (
	PingUp     = "up"
	PingDown   = "down"
	PortOpen   = "open"
	PortClosed = "closed"
)

// probeUp is each probe result key's value for a reached target; probeDown
// its value for one not reached. A key in neither carries no probe result.
var (
	probeUp   = map[string]string{SignalTCPOpen: PortOpen, SignalICMPReachable: PingUp}
	probeDown = map[string]string{SignalTCPOpen: PortClosed, SignalICMPReachable: PingDown}
)

// ProbeResult is a probe's reached-or-not answer as the text sample its key
// lands under, the reachability reason riding as a label.
func ProbeResult(name string, reached bool, ts time.Time, reason Reachability) Sample {
	text := probeDown[name]
	if reached {
		text = probeUp[name]
	}
	return Sample{Name: name, Text: text, IsText: true, TS: ts, Labels: map[string]string{ReasonLabel: string(reason)}}
}

// ProbeReached reads a sample as a probe result: isResult is false for any
// other key; reached is true only for the key's up word.
func ProbeReached(d Sample) (reached, isResult bool) {
	upWord, ok := probeUp[d.Name]
	if !ok {
		return false, false
	}
	return d.IsText && d.Text == upWord, true
}

// EndpointVerdict computes an interface's reachability verdict from the
// samples its probe(s) produced: up iff at least one probe result is present
// and every present one reads reached (the AND); down if any reads not. ok is
// false when no probe result is present, so the caller emits nothing (an
// interface with only a connect-time reading, or none, has no verdict to record).
// For the inline tcp/icmp interfaces this is degenerate (one probe -> the
// verdict), but it generalizes to an interface with several probes.
func EndpointVerdict(dps []Sample) (up bool, ok bool) {
	up = true
	for _, d := range dps {
		if reached, isResult := ProbeReached(d); isResult {
			ok = true
			if !reached {
				up = false
			}
		}
	}
	if !ok {
		return responsiveVerdict(dps)
	}
	return up, true
}

// responsiveVerdict is the verdict of an endpoint whose transport rung is a
// request answered, where no port can be opened (udp, #1037): the
// responsive rung itself. ok is false when no such rung landed.
func responsiveVerdict(dps []Sample) (up bool, ok bool) {
	for _, d := range dps {
		if d.Name == SignalEndpointResponsive && d.IsText {
			return d.Text == VerdictUp, true
		}
	}
	return false, false
}
