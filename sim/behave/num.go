package behave

import "math"

// Clamp constrains v to [lo, hi].
func Clamp(lo, hi, v float64) float64 {
	if hi < lo {
		lo, hi = hi, lo
	}
	return math.Min(math.Max(v, lo), hi)
}

// Round rounds v to n decimal places, half away from zero (a negative n
// rounds to tens, hundreds), the way a profile's `round` does. A value that
// rounds to zero is zero, never negative zero: a profile's value reached state
// as the text "-0", which read back as the number 0.
func Round(n int, v float64) float64 {
	factor := math.Pow(10, float64(n))
	if r := math.Round(v*factor) / factor; r != 0 {
		return r
	}
	return 0
}

// SigFig rounds v to n significant digits, the way a profile's `sigfig` does;
// n of zero or less, and a zero v, pass through.
func SigFig(n int, v float64) float64 {
	if n <= 0 || v == 0 {
		return v
	}
	mag := math.Floor(math.Log10(math.Abs(v)))
	factor := math.Pow(10, float64(n-1)-mag)
	return math.Round(v*factor) / factor
}
