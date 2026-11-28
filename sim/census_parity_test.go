//go:build linux

package sim

import "testing"

// Wire parity of the Tessellate Census C1 family with the YAML profile it replaced (#2836). The golden
// transcript was recorded from the profile; the test replays the same script
// against the Go family and holds its bytes, statuses, headers, state, SNMP
// types and values, line-protocol conversations, drift sequences and room days to it.

func TestTessellateCensusFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, censusWire, censusProduct)
}
