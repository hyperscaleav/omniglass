package labfootprint

import (
	"strings"
	"testing"
)

// A driver's cost per task is the slope of a straight line through the
// sweep's steps: three (tasks, bytes) points that lie on a line give its
// slope exactly, and the line's value at zero tasks is the node's own floor.
func TestASweepsSlopeIsTheCostPerTask(t *testing.T) {
	fit, err := FitSlope([]SweepPoint{{Tasks: 0, Value: 30e6}, {Tasks: 100, Value: 32e6}, {Tasks: 200, Value: 34e6}}, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if !near(fit.PerTask, 20_000) || !near(fit.Intercept, 30e6) || !near(fit.Residual, 0) {
		t.Fatalf("fit %+v, want 20,000 per task from a 30e6 floor, no residual", fit)
	}
	// Points that are not on one line still fit (the steps are read in any
	// order), with a residual under the bound.
	fit, err = FitSlope([]SweepPoint{{Tasks: 200, Value: 34.1e6}, {Tasks: 0, Value: 30e6}, {Tasks: 100, Value: 31.9e6}}, 0.1)
	if err != nil {
		t.Fatal(err)
	}
	if fit.PerTask < 19_000 || fit.PerTask > 21_000 || fit.Residual <= 0 || fit.Residual > 0.1 {
		t.Fatalf("a near line: %+v, want about 20,000 per task and a small residual", fit)
	}
}

// A sweep whose steps do not lie near a line has no single cost per task: a
// step jumped (a GC, a neighbour, a cache filling) and the slope would be a
// guess. Its fit is refused, naming the residual, rather than written down.
func TestASweepFarFromALineIsRefused(t *testing.T) {
	_, err := FitSlope([]SweepPoint{{Tasks: 0, Value: 100}, {Tasks: 100, Value: 500}, {Tasks: 200, Value: 500}}, 0.1)
	if err == nil || !strings.Contains(err.Error(), "residual") {
		t.Fatalf("a bent sweep: %v, want a refusal naming the residual", err)
	}
	// A sweep that does not rise at all has no cost to read out of its
	// noise: refused, not written as zero.
	_, err = FitSlope([]SweepPoint{{Tasks: 0, Value: 100}, {Tasks: 100, Value: 101}, {Tasks: 200, Value: 99}}, 0.1)
	if err == nil {
		t.Fatal("a flat sweep was read as a cost")
	}
}

// A slope needs three steps at distinct task counts: two always fit a line
// exactly, so they could never show a bend.
func TestASweepNeedsThreeDistinctSteps(t *testing.T) {
	for _, pts := range [][]SweepPoint{
		{{Tasks: 0, Value: 1}, {Tasks: 100, Value: 2}},
		{{Tasks: 0, Value: 1}, {Tasks: 100, Value: 2}, {Tasks: 100, Value: 2}},
	} {
		if _, err := FitSlope(pts, 0.1); err == nil {
			t.Fatalf("%v: fitted, want a refusal", pts)
		}
	}
}
