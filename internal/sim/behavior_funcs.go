package sim

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"text/template"
)

// behaviorFuncs returns the FuncMap behaviors get parsed against. It
// extends the package-wide templateFuncs (used by route mutations) with
// math and pseudo-random helpers that close over a per-device, mutex-
// protected *rand.Rand seeded from the device id, so behaviors fire
// deterministically across runs and don't tick in unison across the
// fleet.
//
// Helpers added here:
//
//	add a b      -> a + b           (int64 if both int-valued, else float64)
//	sub a b      -> a - b           (same coercion rule)
//	mul a b      -> a * b
//	div a b      -> a / b           (always float64)
//	clamp lo hi v-> v clamped to [lo, hi]
//	randStep step v -> v + uniform(-step, +step), float64
//	round n v    -> v rounded to n decimal places (negative n rounds to tens, hundreds, ...)
//	sigfig n v   -> v rounded to n significant digits (n <= 0 returns v unchanged)
func behaviorFuncs(rng *deviceRand) template.FuncMap {
	funcs := template.FuncMap{}
	for name, fn := range templateFuncs {
		funcs[name] = fn
	}
	funcs["add"] = numAdd
	funcs["sub"] = numSub
	funcs["mul"] = numMul
	funcs["div"] = numDiv
	funcs["clamp"] = clamp
	funcs["round"] = roundN
	funcs["sigfig"] = sigfigN
	funcs["randStep"] = func(step float64, v any) float64 {
		f, _, err := toFloat(v)
		if err != nil {
			return 0
		}
		return f + rng.uniform(-step, step)
	}
	return funcs
}

// deviceRand wraps math/rand.Rand with a mutex so multiple behavior
// goroutines on the same device can share one source. Drawing from a
// single source per device keeps the random walk replayable: starting
// from the same seed produces the same sequence even when several
// behaviors interleave.
type deviceRand struct {
	mu  sync.Mutex
	rng *rand.Rand
}

func newDeviceRand(deviceID string) *deviceRand {
	sum := sha256.Sum256([]byte(deviceID))
	seed := int64(binary.BigEndian.Uint64(sum[:8]))
	return &deviceRand{rng: rand.New(rand.NewSource(seed))}
}

// uniform returns a value uniformly distributed in [lo, hi]. Safe for
// concurrent callers because the underlying rng read is mu-protected.
func (d *deviceRand) uniform(lo, hi float64) float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if hi < lo {
		lo, hi = hi, lo
	}
	return lo + d.rng.Float64()*(hi-lo)
}

// toFloat coerces template-pipeline numeric inputs (int, int64, float64,
// json.Number-shaped strings) into a float64 plus a "was-it-integer"
// flag the arithmetic helpers use to decide whether to round-trip the
// result back to int64. Strings that do not parse as a number return an
// error, which the caller should surface to the template pipeline.
func toFloat(v any) (float64, bool, error) {
	switch n := v.(type) {
	case int:
		return float64(n), true, nil
	case int32:
		return float64(n), true, nil
	case int64:
		return float64(n), true, nil
	case uint:
		return float64(n), true, nil
	case uint32:
		return float64(n), true, nil
	case uint64:
		return float64(n), true, nil
	case float32:
		f := float64(n)
		return f, f == float64(int64(f)), nil
	case float64:
		return n, n == float64(int64(n)), nil
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%g", &f); err != nil {
			return 0, false, fmt.Errorf("not a number: %q", n)
		}
		return f, false, nil
	case nil:
		return 0, true, nil
	default:
		return 0, false, fmt.Errorf("unsupported numeric type %T", v)
	}
}

func numAdd(a, b any) (any, error) { return numOp(a, b, func(x, y float64) float64 { return x + y }) }
func numSub(a, b any) (any, error) { return numOp(a, b, func(x, y float64) float64 { return x - y }) }
func numMul(a, b any) (any, error) { return numOp(a, b, func(x, y float64) float64 { return x * y }) }

// numDiv always returns float64. Returning int64 from a divide silently
// truncates, which is rarely what a profile author wants when computing
// percentages or ratios. Division by zero returns +Inf or NaN per Go
// semantics; the template pipeline will surface those to the renderer.
func numDiv(a, b any) (float64, error) {
	af, _, err := toFloat(a)
	if err != nil {
		return 0, err
	}
	bf, _, err := toFloat(b)
	if err != nil {
		return 0, err
	}
	return af / bf, nil
}

func numOp(a, b any, op func(x, y float64) float64) (any, error) {
	af, aIsInt, err := toFloat(a)
	if err != nil {
		return nil, err
	}
	bf, bIsInt, err := toFloat(b)
	if err != nil {
		return nil, err
	}
	r := op(af, bf)
	if aIsInt && bIsInt && r == float64(int64(r)) {
		return int64(r), nil
	}
	return r, nil
}

// clamp constrains v to the closed interval [lo, hi]. Returns float64
// so callers can chain it with randStep without losing precision; if a
// profile needs an integer output, pipe through int via a separate
// helper or coerce in the value's storage path.
func clamp(lo, hi float64, v any) (float64, error) {
	f, _, err := toFloat(v)
	if err != nil {
		return 0, err
	}
	if hi < lo {
		lo, hi = hi, lo
	}
	if f < lo {
		return lo, nil
	}
	if f > hi {
		return hi, nil
	}
	return f, nil
}

// roundN rounds v to n decimal places using half-away-from-zero
// rounding (Go's math.Round). Positive n rounds to decimals, zero
// rounds to integer, and negative n rounds to tens / hundreds / etc.
// Returns float64 so authors can chain it through arithmetic helpers
// without losing precision; templates render trailing-zero free by
// default. Non-numeric input falls through toFloat's error path and
// returns 0 to keep the behavior pipeline alive.
func roundN(n int, v any) (float64, error) {
	f, _, err := toFloat(v)
	if err != nil {
		return 0, err
	}
	factor := math.Pow(10, float64(n))
	return math.Round(f*factor) / factor, nil
}

// sigfigN rounds v to n significant digits. Standard formula:
// factor = 10^(n - 1 - floor(log10(|v|))), round(v * factor) / factor.
// n <= 0 is a no-op rather than an error, since "zero significant
// digits" has no useful meaning and surfacing an error from a value
// template would just blank the field. Zero input passes through to
// avoid log10(0); negative values are handled symmetrically.
func sigfigN(n int, v any) (float64, error) {
	f, _, err := toFloat(v)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return f, nil
	}
	if f == 0 {
		return 0, nil
	}
	mag := math.Floor(math.Log10(math.Abs(f)))
	factor := math.Pow(10, float64(n-1)-mag)
	return math.Round(f*factor) / factor, nil
}

// behaviorPhaseOffset returns the deterministic startup delay for a
// given (deviceID, behaviorName, interval) triple. Phase offsets are
// in [0, interval); the same triple always produces the same offset so
// runs replay identically. Returns 0 when interval is non-positive.
func behaviorPhaseOffset(deviceID, behaviorName string, interval int64) int64 {
	if interval <= 0 {
		return 0
	}
	h := sha256.Sum256([]byte(deviceID + "\x00" + behaviorName))
	u := binary.BigEndian.Uint64(h[:8])
	return int64(u % uint64(interval))
}
