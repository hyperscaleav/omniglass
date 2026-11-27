package behave_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
)

// A device's source is a function of its id: the same id draws the same run,
// another id another one.
func TestSourceIsDeterministicPerDevice(t *testing.T) {
	a, b, c := behave.NewSource("d-1"), behave.NewSource("d-1"), behave.NewSource("d-2")
	same, differ := true, false
	for range 50 {
		x, y, z := a.Uniform(0, 1), b.Uniform(0, 1), c.Uniform(0, 1)
		same = same && x == y
		differ = differ || x != z
	}
	if !same || !differ {
		t.Fatalf("same id same run: %v, other id another run: %v", same, differ)
	}
}

func TestSourceDrawsStayInRange(t *testing.T) {
	s := behave.NewSource("d-range")
	for range 500 {
		if v := s.Uniform(-2, 3); v < -2 || v > 3 {
			t.Fatalf("uniform %v outside [-2, 3]", v)
		}
		if v := s.IntN(4, 9); v < 4 || v > 9 {
			t.Fatalf("int %d outside [4, 9]", v)
		}
		if v := s.IntN(9, 4); v < 4 || v > 9 {
			t.Fatalf("swapped int %d outside [4, 9]", v)
		}
	}
	if s.Chance(0) || !s.Chance(1.0001) {
		t.Fatal("a chance of 0 is never and over 1 always")
	}
}

func TestNumbersRoundTheWayProfilesDo(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want float64
	}{
		{"round half away", behave.Round(0, 2.5), 3},
		{"round negative half away", behave.Round(0, -2.5), -3},
		{"round decimals", behave.Round(1, 21.449), 21.4},
		{"round tens", behave.Round(-1, 1234), 1230},
		{"sigfig", behave.SigFig(3, 36.4567), 36.5},
		{"sigfig small", behave.SigFig(3, 0.0123456), 0.0123},
		{"sigfig zero", behave.SigFig(3, 0), 0},
		{"sigfig none", behave.SigFig(0, 1.234), 1.234},
		{"clamp low", behave.Clamp(1, 4, 0.2), 1},
		{"clamp high", behave.Clamp(1, 4, 9), 4},
		{"clamp swapped", behave.Clamp(4, 1, 2), 2},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}
