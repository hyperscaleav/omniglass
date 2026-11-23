package labfootprint

import (
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
)

// unlandedTolerance is the unlanded share the lag must pass before it is
// refused: a batch in flight at the end mark, or the noise of a cadence
// estimate, is a few percent, not a backlog.
const unlandedTolerance = 0.05

// ClosedLoopUnlanded is the share of the window's published batches that had
// not landed, over every outpost the closed loop read (#1897). The closed loop
// reads each outpost's tally outside the ingest stream, so the figure does not
// sit in the backlog it measures. False when no outpost has a ratio to read.
func ClosedLoopUnlanded(brackets []closedloop.OutpostBracket) (float64, bool) {
	var count, in int64
	for _, b := range brackets {
		for _, r := range closedloop.Ratios(b) {
			if r.Gap == closedloop.GapPublishedNotLanded && r.Reading != closedloop.ReadingRestarted {
				count += max(r.Count, 0)
				in += r.In
			}
		}
	}
	return closedloop.Ratio(count, in)
}

// ExpectedProbes is how many samples the poll targets should have drawn over
// the window at their cadence: the denominator of the lag's floor estimate.
func ExpectedProbes(targets map[string][]closedloop.PollTarget, window time.Duration) int64 {
	var n int64
	for _, ts := range targets {
		for _, t := range ts {
			if t.Interval > 0 {
				n += int64(window / t.Interval)
			}
		}
	}
	return n
}

// RefuseBehindLag decides whether a lag read over a partial landing may stand.
// The lag times only the samples that landed, so a lane behind reads healthy
// (#1897). When the closed loop's published-not-landed share is past
// unlandedTolerance the lag is refused as not measured, with the share as the
// reason; a batch still in flight at the end mark is under it, not a backlog.
// When the closed loop is not read (brackets empty), the share is estimated
// in one unit, probes: the probes that landed (landed) against the probes the
// targets should have drawn (expected), a floor, refused past the same
// tolerance. When that estimate cannot be computed the lag stands, and the
// note says it stands unchecked.
func RefuseBehindLag(lags []float64, gap *LagGap, brackets []closedloop.OutpostBracket, landed, expected int64) ([]float64, *LagGap, string) {
	if gap != nil || len(lags) == 0 {
		return lags, gap, ""
	}
	if share, ok := ClosedLoopUnlanded(brackets); ok {
		if share > unlandedTolerance {
			return nil, unlandedGap(share, false), ""
		}
		return lags, nil, ""
	}
	if expected <= 0 || landed < 0 {
		return lags, nil, "ingest lag not checked for a partial landing: the closed loop was not read and the poll cadence could not be estimated"
	}
	if share := 1 - float64(landed)/float64(expected); share > unlandedTolerance {
		return nil, unlandedGap(share, true), ""
	}
	return lags, nil, ""
}

func unlandedGap(share float64, estimated bool) *LagGap {
	reason := fmt.Sprintf("not measured: %.0f %% of the window's published batches had not landed", share*100)
	if estimated {
		reason = fmt.Sprintf("not measured: at least %.0f %% of the window's probes had not landed (a floor estimate from the poll cadence; the closed-loop read was missing)", share*100)
	}
	return &LagGap{Reason: reason, Unlanded: &share, Estimated: estimated}
}
