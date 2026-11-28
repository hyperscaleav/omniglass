//go:build linux

package sim

import (
	"strings"
	"testing"
)

// Wire parity of the Kestrel families with the YAML profiles they replaced
// (#2834). The golden transcripts were recorded from the profiles; each test
// here replays the same script against the Go family and holds its bytes,
// statuses, headers, named events, state, drift sequences and room-day behavior
// to them.

func TestKestrelRelayFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, relayWire, kestrelRelayProduct)
}

func TestKestrelGazeFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, gazeWire, kestrelGazeProduct)
}

func TestKestrelVRoomFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, vroomWire, kestrelVRoomProduct)
}

// The one place the profile's own words were wrong (ADR-2834): its uplink
// recovered line printed the loss with a verb its template could not format
// when the loss was exactly zero, so the line read "loss %!f(int64=0)%". The
// family writes "0.0%". The replay holds the recording to that one correction:
// it must have been in the day, and nothing else of it may differ.
const (
	yamlZeroLoss = "loss %!f(int64=0)%"
	zeroLoss     = "loss 0.0%"
)

func vroomDiverges(t *testing.T, product string, golden wireTranscript) wireTranscript {
	t.Helper()
	fixed := 0
	for key, changes := range golden.RoomDays {
		for i, c := range changes {
			if text, ok := c.Value.(string); ok && strings.Contains(text, yamlZeroLoss) {
				golden.RoomDays[key][i].Value = strings.ReplaceAll(text, yamlZeroLoss, zeroLoss)
				fixed++
			}
		}
	}
	if fixed == 0 {
		t.Fatalf("%s: the golden room days no longer hold the profile's malformed recovery line; drop the divergence", product)
	}
	return golden
}
