package labfootprint

import (
	"fmt"
	"math"
)

// SweepPoint is one step of a driver sweep (#1871): the node's task count
// and a figure read at it (working-set bytes, or millicores).
type SweepPoint struct {
	Tasks int
	Value float64
}

// SlopeFit is the least-squares line through a sweep's steps. PerTask is the
// cost one more task adds; Intercept is the line at zero tasks, the node's
// own floor. Residual is the largest step's distance from the line over the
// line's rise across the sweep, so 0.1 is a step off by a tenth of what the
// sweep's tasks cost.
type SlopeFit struct {
	PerTask   float64
	Intercept float64
	Residual  float64
}

// FitSlope fits a line through a sweep's steps and refuses it when the
// steps are not near one: a residual past maxResidual, or a line that does
// not rise. Two steps always fit exactly and could never show a bend, so a
// fit needs three at distinct task counts.
func FitSlope(points []SweepPoint, maxResidual float64) (SlopeFit, error) {
	distinct := map[int]bool{}
	for _, p := range points {
		distinct[p.Tasks] = true
	}
	if len(distinct) < 3 {
		return SlopeFit{}, fmt.Errorf("a slope needs steps at three distinct task counts, the sweep has %d", len(distinct))
	}
	n := float64(len(points))
	var sx, sy float64
	minT, maxT := points[0].Tasks, points[0].Tasks
	for _, p := range points {
		sx += float64(p.Tasks)
		sy += p.Value
		minT, maxT = min(minT, p.Tasks), max(maxT, p.Tasks)
	}
	mx, my := sx/n, sy/n
	var sxx, sxy float64
	for _, p := range points {
		dx := float64(p.Tasks) - mx
		sxx += dx * dx
		sxy += dx * (p.Value - my)
	}
	fit := SlopeFit{PerTask: sxy / sxx}
	fit.Intercept = my - fit.PerTask*mx
	rise := fit.PerTask * float64(maxT-minT)
	if rise <= 0 {
		return fit, fmt.Errorf("the sweep does not rise (%.4g per task): its cost is inside the noise, so a larger step is needed", fit.PerTask)
	}
	var worst float64
	for _, p := range points {
		worst = math.Max(worst, math.Abs(p.Value-(fit.Intercept+fit.PerTask*float64(p.Tasks))))
	}
	fit.Residual = worst / rise
	if fit.Residual > maxResidual {
		return fit, fmt.Errorf("the sweep is not a line: a step sits %.0f%% of its rise off the fit (residual %.3f, bound %.3f)", fit.Residual*100, fit.Residual, maxResidual)
	}
	return fit, nil
}
