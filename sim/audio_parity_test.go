//go:build linux

package sim

import "testing"

// Wire parity of the room audio families with the YAML profiles they replaced
// (#2835). The golden transcripts were recorded from the profiles; each test
// here replays the same script against the Go family and holds its bytes,
// statuses, headers, state, SNMP types and values, shell sessions, drift
// sequences and room-day behavior to them.

func TestLyraArcFamilyReadsAsTheProfileDid(t *testing.T) { replayWire(t, lyraArcWire, lyraArcProduct) }

func TestLyraHXFamilyReadsAsTheProfileDid(t *testing.T) { replayWire(t, lyraHXWire, lyraHXProduct) }

func TestPolarisDSPFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, polarisDSPWire, polarisDSPProd)
}

func TestPolarisAmpFamilyReadsAsTheProfileDid(t *testing.T) {
	replayWire(t, polarisAmpWire, polarisAmpProd)
}

// A family whose goldens came from a profile that is gone cannot be recorded
// again, however it is asked for.
func TestTheAudioFamiliesAreFrozen(t *testing.T) {
	for _, fam := range []wireFamily{lyraArcWire, lyraHXWire, polarisDSPWire, polarisAmpWire} {
		if err := recordable(fam); err == nil {
			t.Errorf("%s is recordable, and its goldens came from a deleted profile", fam.Name)
		}
	}
}
