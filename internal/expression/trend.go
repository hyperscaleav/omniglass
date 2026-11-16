package expression

import (
	"fmt"
	"math"
	"reflect"

	"github.com/expr-lang/expr/builtin"
)

// The trend functions (#1985): slope, growth and span read a list of samples
// (records with a numeric value and a numeric at, in unix seconds, the shape
// a windowed calc input and a windowed rule's window bind) as a series over
// time, so a criterion can fire on a sustained rise rather than on one
// value. Each is one pass over a list a charge has already paid for, output
// one number, and pure: a fixed series always yields the same trend.
//
//   - slope is the least-squares line's rise per second.
//   - span is the seconds from the earliest sample to the latest.
//   - growth is the line's rise across the span as a fraction of the
//     series' mean: 0.25 is a fitted rise of a quarter of its level.
//
// Fewer than two samples, or samples all taken at one time, have no trend:
// each yields 0, so a series just begun never meets a criterion that needs
// a rise. A series whose mean is 0 has no growth (0).

const trendCost = "one list of samples (records with a value and an at): one pass over the charged list, output a number"

func trendFunc(name string, fold func(pts []trendPoint) float64) *builtin.Function {
	return &builtin.Function{
		Name: name,
		Func: func(args ...any) (any, error) {
			pts, err := trendPoints(name, args[0])
			if err != nil {
				return nil, err
			}
			return fold(pts), nil
		},
		Validate: oneArg(name, reflect.TypeFor[float64]()),
	}
}

// trendPoint is one sample of a series: when, and its value.
type trendPoint struct{ at, value float64 }

// trendPoints reads a list of samples: records with value and at fields
// (tagged expr:"value" and expr:"at") or maps with those keys (a literal).
func trendPoints(name string, list any) ([]trendPoint, error) {
	rv := reflect.ValueOf(list)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("%s takes one list of samples, not %T", name, list)
	}
	out := make([]trendPoint, rv.Len())
	for i := range out {
		at, v, ok := samplePoint(rv.Index(i))
		if !ok {
			return nil, fmt.Errorf("%s takes samples (records with a numeric value and at), not %T", name, rv.Index(i).Interface())
		}
		out[i] = trendPoint{at: at, value: v}
	}
	return out, nil
}

func samplePoint(rv reflect.Value) (at, value float64, ok bool) {
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return 0, 0, false
		}
		rv = rv.Elem()
	}
	var a, v reflect.Value
	switch rv.Kind() {
	case reflect.Struct:
		vi, vok := valueField(rv.Type())
		ai, aok := atField(rv.Type())
		if !vok || !aok {
			return 0, 0, false
		}
		a, v = rv.Field(ai), rv.Field(vi)
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return 0, 0, false
		}
		a, v = rv.MapIndex(reflect.ValueOf("at")), rv.MapIndex(reflect.ValueOf("value"))
		if !a.IsValid() || !v.IsValid() {
			return 0, 0, false
		}
	default:
		return 0, 0, false
	}
	at, _, aok := number(a.Interface())
	value, _, vok := number(v.Interface())
	return at, value, aok && vok
}

// atField is the index of a record type's at field (tagged expr:"at").
func atField(t reflect.Type) (int, bool) {
	if t.Kind() != reflect.Struct {
		return 0, false
	}
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Tag.Get("expr") == "at" {
			return i, true
		}
	}
	return 0, false
}

// isSampleType is a record type a trend function reads: a numeric value
// field and a numeric at field.
func isSampleType(t reflect.Type) bool {
	vi, vok := valueField(t)
	ai, aok := atField(t)
	return vok && aok && isNumberKind(t.Field(vi).Type.Kind()) && isNumberKind(t.Field(ai).Type.Kind())
}

// fitLine is the least-squares line through the points: its slope (value
// per second), the series' mean, and its span in seconds. The times are
// centred on their mean first, so unix seconds squared lose no precision.
func fitLine(pts []trendPoint) (slope, mean, span float64) {
	if len(pts) == 0 {
		return 0, 0, 0
	}
	n := float64(len(pts))
	var sumAt, sumV float64
	lo, hi := pts[0].at, pts[0].at
	for _, p := range pts {
		sumAt += p.at
		sumV += p.value
		lo, hi = math.Min(lo, p.at), math.Max(hi, p.at)
	}
	meanAt, mean := sumAt/n, sumV/n
	var sxy, sxx float64
	for _, p := range pts {
		dx := p.at - meanAt
		sxy += dx * (p.value - mean)
		sxx += dx * dx
	}
	if len(pts) < 2 || sxx == 0 {
		return 0, mean, hi - lo
	}
	return sxy / sxx, mean, hi - lo
}

func slopeOf(pts []trendPoint) float64 {
	s, _, _ := fitLine(pts)
	return s
}

func spanOf(pts []trendPoint) float64 {
	_, _, sp := fitLine(pts)
	return sp
}

func growthOf(pts []trendPoint) float64 {
	s, mean, sp := fitLine(pts)
	if mean == 0 {
		return 0
	}
	return s * sp / math.Abs(mean)
}
