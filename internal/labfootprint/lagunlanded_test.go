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
	got, gap := RefuseBehindLag(lags, nil, []closedloop.OutpostBracket{behindBracket(100, 30)}, 0)
	if got != nil || gap == nil || gap.Unlanded == nil || *gap.Unlanded < 0.69 || *gap.Unlanded > 0.71 {
		t.Fatalf("got lags %v gap %+v, want refused at 70 %% unlanded", got, gap)
	}
	if !strings.Contains(gap.Reason, "70 %") || !strings.Contains(gap.footer(), "not measured") {
		t.Fatalf("reason %q footer %q", gap.Reason, gap.footer())
	}
}

func TestRefuseBehindLagKeepsAHealthyLane(t *testing.T) {
	lags := []float64{0.2}
	got, gap := RefuseBehindLag(lags, nil, []closedloop.OutpostBracket{behindBracket(100, 100)}, 0)
	if len(got) != 1 || gap != nil {
		t.Fatalf("got %v %+v, want the lag kept", got, gap)
	}
}

func TestRefuseBehindLagEstimatesWithoutTheClosedLoop(t *testing.T) {
	lags := make([]float64, 600)
	got, gap := RefuseBehindLag(lags, nil, nil, 2000)
	if got != nil || gap == nil || !gap.Estimated || !strings.Contains(gap.Reason, "floor estimate") {
		t.Fatalf("got %v %+v, want a refused floor estimate", got, gap)
	}
	if got, gap = RefuseBehindLag(make([]float64, 1980), nil, nil, 2000); gap != nil || len(got) != 1980 {
		t.Fatalf("within tolerance must stand, got %+v", gap)
	}
	if _, gap = RefuseBehindLag(lags, nil, nil, 0); gap != nil {
		t.Fatal("no cadence, no estimate")
	}
}

func TestExpectedProbes(t *testing.T) {
	targets := map[string][]closedloop.PollTarget{"a": {{Interval: 10 * time.Second}, {Interval: 0}}}
	if n := ExpectedProbes(targets, time.Minute); n != 6 {
		t.Fatalf("got %d, want 6", n)
	}
}
