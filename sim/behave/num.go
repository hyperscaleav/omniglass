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
// rounds to tens, hundreds), the way a profile's `round` does.
func Round(n int, v float64) float64 {
	factor := math.Pow(10, float64(n))
	return math.Round(v*factor) / factor
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
