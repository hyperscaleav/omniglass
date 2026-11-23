package labfootprint

import (
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
)

// estimateTolerance is the unlanded share the cadence estimate must pass
// before it refuses the lag: the estimate counts probes from the task
// cadence, so a few percent is its own noise, not a backlog.
const estimateTolerance = 0.05

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
// (#1897). When the closed loop's published-not-landed share is above 0 the
// lag is refused as not measured, with the share as the reason. When the
// closed loop is not read (brackets empty), the share is estimated from the
// poll cadence: the rows landed against the probes the targets should have
// drawn, a floor, and refused only past estimateTolerance. A read at the row
// cap is not a survivor set, so the estimate does not refuse it.
func RefuseBehindLag(lags []float64, gap *LagGap, brackets []closedloop.OutpostBracket, expected int64) ([]float64, *LagGap) {
	if gap != nil || len(lags) == 0 {
		return lags, gap
	}
	if share, ok := ClosedLoopUnlanded(brackets); ok {
		if share > 0 {
			return nil, unlandedGap(share, false)
		}
		return lags, nil
	}
	if expected <= 0 || len(lags) >= lagSampleRows {
		return lags, nil
	}
	if share := 1 - float64(len(lags))/float64(expected); share > estimateTolerance {
		return nil, unlandedGap(share, true)
	}
	return lags, nil
}

func unlandedGap(share float64, estimated bool) *LagGap {
	reason := fmt.Sprintf("not measured: %.0f %% of the window's published batches had not landed", share*100)
	if estimated {
		reason = fmt.Sprintf("not measured: at least %.0f %% of the window's probes had not landed (a floor estimate from the poll cadence; the closed-loop read was missing)", share*100)
	}
	return &LagGap{Reason: reason, Unlanded: &share, Estimated: estimated}
}
