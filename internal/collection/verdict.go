package collection

// The interface reachability verdict: a built-in STATE datapoint, distinct from
// the raw probe metrics (tcp.open, icmp.reachable). interface.reachable is gated
// per interface and its value domain is up/down; availability is time_in_state
// over it. The node computes it and emits it as a state datapoint; the ingest
// consumer routes it to state_datapoint by the datapoint_type kind.
const (
	// DatapointInterfaceReachable is the seeded state key the verdict lands under.
	DatapointInterfaceReachable = "interface.reachable"
	// VerdictUp / VerdictDown are the state's value domain.
	VerdictUp   = "up"
	VerdictDown = "down"
)

// reachabilityMetrics is the set of probe datapoint names that each carry one
// probe's up/down reachability answer (value 1 = reachable, 0 = not). The
// interface verdict is the AND of these across the interface's probe results.
var reachabilityMetrics = map[string]bool{
	DatapointTCPOpen:       true,
	DatapointICMPReachable: true,
}

// InterfaceVerdict computes an interface's reachability verdict from the
// datapoints its probe(s) produced: up iff at least one reachability metric is
// present and every present one reads 1 (the AND); down if any reads 0. ok is
// false when no reachability metric is present, so the caller emits nothing (an
// interface with only a connect-time reading, or none, has no verdict to record).
// For the inline tcp/icmp interfaces this is degenerate (one probe -> the
// verdict), but it generalizes to an interface with several probes.
func InterfaceVerdict(dps []Datapoint) (up bool, ok bool) {
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
		return false, false
	}
	return up, true
}
