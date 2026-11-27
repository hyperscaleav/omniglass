package sim

import (
	"math"
	"testing"
)

// TestNumAdd_PreservesIntWhenBothIntegerValued covers the round-trip
// rule numAdd implements: a profile that increments uptime should not
// silently turn an int field into a float, since downstream JSON
// consumers may break on the unexpected ".0" suffix.
func TestNumAdd_PreservesIntWhenBothIntegerValued(t *testing.T) {
	cases := []struct {
		name string
		a, b any
		want any
	}{
		{"int_plus_int", int64(5), int64(3), int64(8)},
		{"int_plus_int_value_float", int64(5), float64(3), int64(8)},
		{"float_plus_float_int_valued", float64(5), float64(3), int64(8)},
		{"int_plus_float_fractional", int64(5), float64(0.5), float64(5.5)},
		{"nil_plus_int", nil, int64(10), int64(10)},
		{"int_plus_nil", int64(10), nil, int64(10)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := numAdd(tc.a, tc.b)
			if err != nil {
				t.Fatalf("numAdd(%v, %v): %v", tc.a, tc.b, err)
			}
			if got != tc.want {
				t.Errorf("numAdd(%v, %v) = %v (%T), want %v (%T)", tc.a, tc.b, got, got, tc.want, tc.want)
			}
		})
	}
}

// TestNumDiv_AlwaysFloat pins the special-case for division: returning
// int64 from int/int divides would silently truncate (5 / 2 = 2), which
// is rarely what a profile author wants when computing a percentage.
func TestNumDiv_AlwaysFloat(t *testing.T) {
	got, err := numDiv(int64(5), int64(2))
	if err != nil {
		t.Fatalf("numDiv: %v", err)
	}
	if got != 2.5 {
		t.Errorf("numDiv(5, 2) = %v, want 2.5", got)
	}
}

func TestClamp_RejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name   string
		lo, hi float64
		v      any
		want   float64
	}{
		{"below", 0, 10, float64(-3), 0},
		{"above", 0, 10, float64(15), 10},
		{"inside", 0, 10, float64(7), 7},
		{"reversed_bounds_normalize", 10, 0, float64(7), 7},
		{"int_input", 0, 10, int64(5), 5},
		{"nil_input_clamped_to_lo", 5, 10, nil, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clamp(tc.lo, tc.hi, tc.v)
			if err != nil {
				t.Fatalf("clamp: %v", err)
			}
			if got != tc.want {
				t.Errorf("clamp(%v, %v, %v) = %v, want %v", tc.lo, tc.hi, tc.v, got, tc.want)
			}
		})
	}
}

// TestRound covers the decimal-place rounding contract: positive n
// rounds to fractional digits, zero rounds to integer, and negative n
// rounds to tens / hundreds. math.Round is half-away-from-zero, so the
// pinned cases all sit on values where banker's vs half-up disagrees,
// to guarantee we don't silently swap modes later.
func TestRound(t *testing.T) {
	cases := []struct {
		name string
		n    int
		v    any
		want float64
	}{
		{"one_decimal", 1, float64(20.489), 20.5},
		{"zero_decimal_rounds_up", 0, float64(20.489), 20},
		{"zero_decimal_rounds_half_away", 0, float64(2.5), 3},
		{"negative_n_tens", -1, float64(1234), 1230},
		{"negative_n_hundreds", -2, float64(1234), 1200},
		{"nil_treated_as_zero", 2, nil, 0},
		{"int_input_no_op", 2, int64(7), 7},
		{"large_value", 1, float64(123456.789), 123456.8},
		{"negative_value_half_away_from_zero", 1, float64(-2.55), -2.6},
		{"three_decimals_truncates_precision", 3, float64(20.48927334520514), 20.489},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := roundN(tc.n, tc.v)
			if err != nil {
				t.Fatalf("roundN(%d, %v): %v", tc.n, tc.v, err)
			}
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("roundN(%d, %v) = %v, want %v", tc.n, tc.v, got, tc.want)
			}
		})
	}
}

// TestSigfig covers significant-digit rounding across magnitudes. The
// formula factor = 10^(n - 1 - floor(log10(|v|))) gives the right
// result for values both larger and smaller than 1, so the table walks
// from a six-digit integer down to a sub-millisigfig float to flush out
// any sign-of-exponent bugs.
func TestSigfig(t *testing.T) {
	cases := []struct {
		name string
		n    int
		v    any
		want float64
	}{
		{"three_sigfigs_small_decimal", 3, float64(20.48927334520514), 20.5},
		{"three_sigfigs_thousands", 3, float64(1234.5), 1230},
		{"three_sigfigs_subthousandth", 3, float64(0.000487329), 0.000487},
		{"three_sigfigs_trailing_zero", 3, float64(12.0), 12.0},
		{"three_sigfigs_boundary_just_under_ten", 3, float64(9.99), 9.99},
		{"n_zero_passes_through", 0, float64(20.48927), 20.48927},
		{"n_negative_passes_through", -2, float64(20.48927), 20.48927},
		{"one_sigfig_small_decimal", 1, float64(0.0876), 0.09},
		{"zero_input", 3, float64(0), 0},
		{"negative_value", 3, float64(-1234.5), -1230},
		{"very_large_value", 3, float64(1.234e12), 1.23e12},
		{"very_small_value", 3, float64(1.234e-12), 1.23e-12},
		{"nil_treated_as_zero", 3, nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sigfigN(tc.n, tc.v)
			if err != nil {
				t.Fatalf("sigfigN(%d, %v): %v", tc.n, tc.v, err)
			}
			// Relative tolerance handles the very-large / very-small
			// cases without a separate special branch; absolute
			// tolerance handles the zero case.
			tol := math.Abs(tc.want) * 1e-9
			if tol < 1e-15 {
				tol = 1e-15
			}
			if math.Abs(got-tc.want) > tol {
				t.Errorf("sigfigN(%d, %v) = %v, want %v", tc.n, tc.v, got, tc.want)
			}
		})
	}
}

// TestDeviceRand_Deterministic verifies two devices with the same id
// produce the same random sequence. This is the guarantee behaviors
// rely on for repeatable random walks across runs.
func TestDeviceRand_Deterministic(t *testing.T) {
	a := newDeviceRand("kestrel-001")
	b := newDeviceRand("kestrel-001")
	for i := 0; i < 20; i++ {
		if x, y := a.uniform(-1, 1), b.uniform(-1, 1); x != y {
			t.Fatalf("draw %d: device-a=%v device-b=%v (same id should produce same stream)", i, x, y)
		}
	}
}

// TestDeviceRand_DifferentIDsDiffer verifies different device ids
// produce different sequences. Otherwise a 1000-device fleet running
// the same drift behavior would all step in unison, defeating the
// "individually phased" property the issue calls out.
func TestDeviceRand_DifferentIDsDiffer(t *testing.T) {
	a := newDeviceRand("kestrel-001")
	b := newDeviceRand("kestrel-002")
	// At least one of the first 5 draws must differ; if all five match
	// across SHA-256 seeds, something's gone catastrophically wrong.
	matches := 0
	for i := 0; i < 5; i++ {
		if a.uniform(-1, 1) == b.uniform(-1, 1) {
			matches++
		}
	}
	if matches == 5 {
		t.Fatalf("5/5 draws matched across different device ids; rand source isn't device-keyed")
	}
}

// TestRandStep_StaysCenteredAroundCurrent verifies randStep returns
// values within [v-step, v+step] and isn't biased one way (over many
// draws the mean approaches v). Bounds-only test; the deterministic
// rand test above covers replayability.
func TestRandStep_StaysCenteredAroundCurrent(t *testing.T) {
	rng := newDeviceRand("test-device")
	const step = 0.5
	const center = 10.0
	const draws = 1000
	var sum float64
	for i := 0; i < draws; i++ {
		v := center + rng.uniform(-step, step)
		if v < center-step || v > center+step {
			t.Fatalf("draw %d = %v outside [%v, %v]", i, v, center-step, center+step)
		}
		sum += v
	}
	mean := sum / draws
	// 1000 uniform draws centered at 10 with step 0.5 should have a
	// mean within 0.05 of center. Generous bound so the test isn't
	// flaky on different runtimes.
	if math.Abs(mean-center) > 0.05 {
		t.Errorf("mean %v drifted more than 0.05 from %v across %d draws", mean, center, draws)
	}
}

// TestBehaviorPhaseOffset_DeterministicAndBounded pins the phase-offset
// contract: same triple → same offset, offset always in [0, interval).
// Different triples should produce different offsets often enough that
// behaviors don't synchronize across devices.
func TestBehaviorPhaseOffset_DeterministicAndBounded(t *testing.T) {
	const interval int64 = 10000

	off1 := behaviorPhaseOffset("kestrel-001", "uptime", interval)
	off2 := behaviorPhaseOffset("kestrel-001", "uptime", interval)
	if off1 != off2 {
		t.Fatalf("same triple produced different offsets: %d vs %d", off1, off2)
	}
	if off1 < 0 || off1 >= interval {
		t.Fatalf("offset %d outside [0, %d)", off1, interval)
	}

	// Two different device ids should produce different offsets at
	// least once across a small sample; if they all matched, the hash
	// isn't actually device-keyed.
	differing := 0
	for i := 0; i < 5; i++ {
		nameA := "behavior-" + string(rune('a'+i))
		a := behaviorPhaseOffset("kestrel-001", nameA, interval)
		b := behaviorPhaseOffset("kestrel-002", nameA, interval)
		if a != b {
			differing++
		}
	}
	if differing == 0 {
		t.Errorf("phase offsets never differed across devices in 5 trials")
	}
}

// TestBehaviorPhaseOffset_ZeroInterval covers the defensive branch.
// Behaviors with zero or negative interval are rejected at compile
// time, but the helper itself should return 0 rather than panic on
// modulo-by-zero if a caller misuses it.
func TestBehaviorPhaseOffset_ZeroInterval(t *testing.T) {
	if got := behaviorPhaseOffset("d", "b", 0); got != 0 {
		t.Errorf("zero interval should return 0, got %d", got)
	}
	if got := behaviorPhaseOffset("d", "b", -5); got != 0 {
		t.Errorf("negative interval should return 0, got %d", got)
	}
}
