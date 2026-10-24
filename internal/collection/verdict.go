package collection

import "github.com/hyperscaleav/omniglass/internal/collection/probekeys"

// The interface reachability verdict: a built-in STATE sample, distinct from
// the raw probe metrics (tcp-open, icmp-reachable). endpoint-reachable is gated
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

// reachabilityMetrics is the set of probe sample names that each carry one
// probe's up/down reachability answer (value 1 = reachable, 0 = not). The
// interface verdict is the AND of these across the interface's probe results.
var reachabilityMetrics = map[string]bool{
	SignalTCPOpen:       true,
	SignalICMPReachable: true,
}

// EndpointVerdict computes an interface's reachability verdict from the
// samples its probe(s) produced: up iff at least one reachability metric is
// present and every present one reads 1 (the AND); down if any reads 0. ok is
// false when no reachability metric is present, so the caller emits nothing (an
// interface with only a connect-time reading, or none, has no verdict to record).
// For the inline tcp/icmp interfaces this is degenerate (one probe -> the
// verdict), but it generalizes to an interface with several probes.
func EndpointVerdict(dps []Sample) (up bool, ok bool) {
	up = true
	for _, d := range dps {
		if reachabilityMetrics[d.Name] {
			ok = true
			if d.Value != 1 {
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
