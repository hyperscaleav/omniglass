package spec_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// A state path reads the same however its value was written: a number is an
// int, an int64 or a float64 depending on the writer.
func TestTypedReadsOfAStatePath(t *testing.T) {
	s := spec.Spec{Name: "x", State: map[string]any{
		"s": "text", "b": true, "i": 7, "i64": int64(-3), "f": 38.8, "neg": -2.9, "str": "12",
	}}
	f := spectest.NewFake(s, "")
	for _, tc := range []struct {
		path string
		str  string
		int  int64
		flt  float64
		flag bool
	}{
		{"s", "text", 0, 0, false},
		{"b", "true", 1, 1, true},
		{"i", "7", 7, 7, false},
		{"i64", "-3", -3, -3, false},
		{"f", "38.8", 38, 38.8, false},
		{"neg", "-2.9", -2, -2.9, false},
		{"str", "12", 0, 0, false},
		{"missing", "", 0, 0, false},
	} {
		if got := spec.String(f, tc.path); got != tc.str {
			t.Errorf("String(%s) = %q, want %q", tc.path, got, tc.str)
		}
		if got := spec.Int64(f, tc.path); got != tc.int {
			t.Errorf("Int64(%s) = %d, want %d", tc.path, got, tc.int)
		}
		if got := spec.Float(f, tc.path); got != tc.flt {
			t.Errorf("Float(%s) = %v, want %v", tc.path, got, tc.flt)
		}
		if got := spec.Bool(f, tc.path); got != tc.flag {
			t.Errorf("Bool(%s) = %v, want %v", tc.path, got, tc.flag)
		}
	}
	if spec.Int(f, "i") != 7 {
		t.Error("Int(i) != 7")
	}
}

func TestWholeReadsAnyValueOutOfATreeAsANumber(t *testing.T) {
	for in, want := range map[any]int64{int(3): 3, int64(4): 4, 5.9: 5, uint32(6): 6, true: 1, false: 0, "7": 0, nil: 0} {
		if got := spec.Whole(in); got != want {
			t.Errorf("Whole(%#v) = %d, want %d", in, got, want)
		}
	}
}
