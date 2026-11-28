package spec_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// An elapsed value is whole seconds since an anchor: zero while the anchor is
// unset and never negative when the clock runs behind it.
func TestElapsedReadsWholeSecondsSinceItsAnchor(t *testing.T) {
	e := spec.Elapsed{Path: "status.device.uptime", Since: "status.device.boot_epoch"}
	now := time.Unix(10_000, 999_000_000)
	for _, tc := range []struct {
		epoch int64
		want  int64
	}{
		{9_000, 1_000},
		{10_000, 0},
		{0, 0},
		{-5, 0},
		{10_500, 0},
	} {
		if got := e.At(tc.epoch, now); got != tc.want {
			t.Errorf("At(%d) = %d, want %d", tc.epoch, got, tc.want)
		}
	}
}

// A family declaring an elapsed value must say where it is read and where its
// anchor lives, and may not read two values at one path.
func TestSpecRefusesAMalformedElapsed(t *testing.T) {
	ok := spec.Elapsed{Path: "status.device.uptime", Since: "status.device.boot_epoch"}
	for name, elapsed := range map[string][]spec.Elapsed{
		"no path":   {{Since: "a.b"}},
		"no anchor": {{Path: "a.b"}},
		"twice":     {ok, ok},
	} {
		if _, err := spec.NewFamily(spec.Spec{Name: "x", Elapsed: elapsed}); err == nil {
			t.Errorf("%s: a family with %v was accepted", name, elapsed)
		}
	}
	if _, err := spec.NewFamily(spec.Spec{Name: "x", Elapsed: []spec.Elapsed{ok}}); err != nil {
		t.Errorf("a well-formed elapsed was refused: %v", err)
	}
}

// A YAML profile's computed field and a family's Elapsed present the same
// read-derived value to the engine.
func TestDefinitionsListTheirElapsedValues(t *testing.T) {
	fam, err := spec.NewFamily(spec.Spec{Name: "x", Elapsed: []spec.Elapsed{{Path: "p", Since: "s"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := fam.ElapsedList()
	if len(got) != 1 || got[0] != (spec.Elapsed{Path: "p", Since: "s"}) {
		t.Errorf("family ElapsedList = %v", got)
	}
}
