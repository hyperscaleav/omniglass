//go:build linux

package sim

import "testing"

// Wire parity of the booking service family with the YAML profile it replaced (#2836). The golden
// transcript was recorded from the profile; the test replays the same script
// against the Go family and holds its bytes, statuses, headers, state, SNMP
// types and values, pushed batches, drift sequences and room days to it.

func TestBookingServiceFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, bookingWire, bookingProduct)
}
