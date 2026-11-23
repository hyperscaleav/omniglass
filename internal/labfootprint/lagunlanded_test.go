package labfootprint

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
)

func behindBracket(acked, landed int64) closedloop.OutpostBracket {
	var b closedloop.OutpostBracket
	b.Start.SimBefore.FleetGeneration, b.Start.SimAfter.FleetGeneration = 1, 1
	b.End.SimBefore.FleetGeneration, b.End.SimAfter.FleetGeneration = 1, 1
	b.End.Outpost.BatchesAcknowledged = acked
	b.Receipts.Landed = landed
	return b
}

// A lane far behind: the few samples that skipped the queue read a low lag.
func TestRefuseBehindLagWhenMostBatchesHaveNotLanded(t *testing.T) {
	lags := []float64{0.2, 0.3, 0.25}
	got, gap, _ := RefuseBehindLag(lags, nil, []closedloop.OutpostBracket{behindBracket(100, 30)}, 0, 0)
	if got != nil || gap == nil || gap.Unlanded == nil || *gap.Unlanded < 0.69 || *gap.Unlanded > 0.71 {
		t.Fatalf("got lags %v gap %+v, want refused at 70 %% unlanded", got, gap)
	}
	if !strings.Contains(gap.Reason, "70 %") || !strings.Contains(gap.footer(), "not measured") {
		t.Fatalf("reason %q footer %q", gap.Reason, gap.footer())
	}
}

func TestRefuseBehindLagKeepsAHealthyLane(t *testing.T) {
	lags := []float64{0.2}
	got, gap, _ := RefuseBehindLag(lags, nil, []closedloop.OutpostBracket{behindBracket(100, 100)}, 0, 0)
	if len(got) != 1 || gap != nil {
		t.Fatalf("got %v %+v, want the lag kept", got, gap)
	}
}

// One batch of a hundred still in flight at the end mark is not a backlog.
func TestRefuseBehindLagToleratesABatchInFlight(t *testing.T) {
	lags := []float64{0.2}
	got, gap, _ := RefuseBehindLag(lags, nil, []closedloop.OutpostBracket{behindBracket(100, 99)}, 0, 0)
	if len(got) != 1 || gap != nil {
		t.Fatalf("got %v %+v, want the lag kept with one batch in flight", got, gap)
	}
}

func TestRefuseBehindLagEstimatesWithoutTheClosedLoop(t *testing.T) {
	lags := make([]float64, 600)
	got, gap, _ := RefuseBehindLag(lags, nil, nil, 600, 2000)
	if got != nil || gap == nil || !gap.Estimated || !strings.Contains(gap.Reason, "floor estimate") {
		t.Fatalf("got %v %+v, want a refused floor estimate", got, gap)
	}
	if got, gap, _ = RefuseBehindLag(make([]float64, 5000), nil, nil, 1980, 2000); gap != nil || len(got) != 5000 {
		t.Fatalf("within tolerance must stand, got %+v", gap)
	}
}

// Rows are not probes: a probe that emits many metrics must not hide a backlog.
func TestRefuseBehindLagCountsProbesNotRows(t *testing.T) {
	if _, gap, _ := RefuseBehindLag(make([]float64, 6000), nil, nil, 600, 2000); gap == nil {
		t.Fatal("600 landed probes of 2000 expected must refuse however many rows they held")
	}
}

func TestRefuseBehindLagSaysWhenTheEstimateCannotBeComputed(t *testing.T) {
	lags := make([]float64, 600)
	for _, c := range [][2]int64{{600, 0}, {-1, 2000}} {
		got, gap, note := RefuseBehindLag(lags, nil, nil, c[0], c[1])
		if gap != nil || len(got) != 600 || !strings.Contains(note, "not checked") {
			t.Fatalf("landed %d expected %d: got %+v note %q, want the lag kept and annotated", c[0], c[1], gap, note)
		}
	}
}

func TestExpectedProbes(t *testing.T) {
	targets := map[string][]closedloop.PollTarget{"a": {{Interval: 10 * time.Second}, {Interval: 0}}}
	if n := ExpectedProbes(targets, time.Minute); n != 6 {
		t.Fatalf("got %d, want 6", n)
	}
}
