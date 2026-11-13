package labfootprint

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/simfleet"
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

// The reasons a shipped driver is not swept, in the order the plan checks
// them. A driver is swept only where the simulator serves one of its
// products and the collector attaches to it through the driver itself: a
// reachability probe in its place would measure the probe, not the driver.
const (
	SkipNoPolls     = "no polls: the driver only listens, so it runs no task to cost"
	SkipNoProduct   = "no shipped product is on this driver"
	SkipNoSimulator = "no simulator profile serves a product on this driver"
	SkipNoAttach    = "the simulated fleet has no attach for this driver, so its devices would be probed instead"
)

// DriverTarget is a driver the sweep measures: the product its simulated
// components are, and the driver's poll intervals (one task each, per
// component).
type DriverTarget struct {
	Driver  string
	Product string
	Polls   []time.Duration
}

// RunsPerSecondPerTask is how often one of the driver's tasks runs, on
// average across its polls.
func (t DriverTarget) RunsPerSecondPerTask() float64 {
	if len(t.Polls) == 0 {
		return 0
	}
	var sum float64
	for _, p := range t.Polls {
		sum += 1 / p.Seconds()
	}
	return sum / float64(len(t.Polls))
}

// DriverSkip is a shipped driver the sweep does not measure, and why.
type DriverSkip struct {
	Driver string `json:"driver"`
	Reason string `json:"reason"`
}

// PlanDriverSweep decides which shipped drivers the sweep measures, each on
// its first simulated product in products order, and lists the rest with
// the first reason each is not. Both come back in driver order.
func PlanDriverSweep(polls map[string][]time.Duration, products []seed.ShippedProduct,
	simulated func(product string) bool, attachable func(driver string) bool) ([]DriverTarget, []DriverSkip) {
	drivers := make([]string, 0, len(polls))
	for d := range polls {
		drivers = append(drivers, d)
	}
	sort.Strings(drivers)
	var targets []DriverTarget
	var skips []DriverSkip
	for _, d := range drivers {
		if len(polls[d]) == 0 {
			skips = append(skips, DriverSkip{Driver: d, Reason: SkipNoPolls})
			continue
		}
		carried, product := false, ""
		for _, p := range products {
			if p.Driver != d {
				continue
			}
			carried = true
			if simulated(p.Name) {
				product = p.Name
				break
			}
		}
		switch {
		case !carried:
			skips = append(skips, DriverSkip{Driver: d, Reason: SkipNoProduct})
		case product == "":
			skips = append(skips, DriverSkip{Driver: d, Reason: SkipNoSimulator})
		case !attachable(d):
			skips = append(skips, DriverSkip{Driver: d, Reason: SkipNoAttach})
		default:
			targets = append(targets, DriverTarget{Driver: d, Product: product, Polls: polls[d]})
		}
	}
	return targets, skips
}

// ShippedDriverSweep is the plan for the shipped seed: its drivers and
// products, the simulator's own profiles, and the simulated fleet's
// attaches.
func ShippedDriverSweep() ([]DriverTarget, []DriverSkip) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		panic("labfootprint: load simulator profiles: " + err.Error())
	}
	return PlanDriverSweep(seed.ShippedDriverPolls(), seed.ShippedProducts(),
		func(p string) bool { _, _, ok := store.ForProduct(p); return ok },
		func(d string) bool { _, ok := simfleet.Attach[d]; return ok })
}

// DriverStep is what the sweep read at one step: the node's driver task
// count, its cgroup working set and CPU over the window, and the mean
// poll-cpu-time its polls reported over the same window (0 with no tasks).
type DriverStep struct {
	Components      int     `json:"components"`
	Tasks           int     `json:"tasks"`
	WorkingSetBytes float64 `json:"working_set_bytes"`
	Millicores      float64 `json:"millicores"`
	PollCPUMillis   float64 `json:"poll_cpu_ms"`
}

// DriverCost is one measured driver: what a task of it costs the node in
// memory and CPU, and what one run costs by the cgroup and by the polling
// thread's own clock (#1870). ThreadShare is the thread clock over the
// cgroup: the part of a run the clock sees, the rest being work the run
// hands to other goroutines (a held session's reader, the report, the GC).
type DriverCost struct {
	Driver                string       `json:"driver"`
	Product               string       `json:"product"`
	Polls                 []string     `json:"polls"`
	Tasks                 int          `json:"tasks"`
	MemoryPerTaskBytes    float64      `json:"memory_per_task_bytes"`
	MillicoresPerTask     float64      `json:"millicores_per_task"`
	CPUPerRunMillis       float64      `json:"cpu_per_run_ms"`
	ThreadCPUPerRunMillis float64      `json:"thread_cpu_per_run_ms"`
	ThreadShare           float64      `json:"thread_share"`
	MemoryResidual        float64      `json:"memory_residual"`
	CPUResidual           float64      `json:"cpu_residual"`
	Steps                 []DriverStep `json:"steps"`
}

// AssembleDriverCost fits a driver's steps: memory per task within
// memResidual of a line, CPU per task within cpuResidual (CPU is the
// noisier read). A step set that does not fit is refused, naming which
// figure, rather than written down as a cost.
func AssembleDriverCost(t DriverTarget, steps []DriverStep, memResidual, cpuResidual float64) (DriverCost, error) {
	mem := make([]SweepPoint, len(steps))
	cpu := make([]SweepPoint, len(steps))
	var thread float64
	var measured, top int
	for i, s := range steps {
		mem[i] = SweepPoint{Tasks: s.Tasks, Value: s.WorkingSetBytes}
		cpu[i] = SweepPoint{Tasks: s.Tasks, Value: s.Millicores}
		top = max(top, s.Tasks)
		if s.Tasks > 0 {
			thread += s.PollCPUMillis
			measured++
		}
	}
	memFit, err := FitSlope(mem, memResidual)
	if err != nil {
		return DriverCost{}, fmt.Errorf("%s memory: %w", t.Driver, err)
	}
	cpuFit, err := FitSlope(cpu, cpuResidual)
	if err != nil {
		return DriverCost{}, fmt.Errorf("%s cpu: %w", t.Driver, err)
	}
	polls := make([]string, len(t.Polls))
	for i, p := range t.Polls {
		polls[i] = p.String()
	}
	c := DriverCost{
		Driver: t.Driver, Product: t.Product, Polls: polls, Tasks: top,
		MemoryPerTaskBytes: memFit.PerTask, MillicoresPerTask: cpuFit.PerTask,
		CPUPerRunMillis: cpuFit.PerTask / t.RunsPerSecondPerTask(),
		MemoryResidual:  memFit.Residual, CPUResidual: cpuFit.Residual,
		Steps: steps,
	}
	if measured > 0 {
		c.ThreadCPUPerRunMillis = thread / float64(measured)
		c.ThreadShare = c.ThreadCPUPerRunMillis / c.CPUPerRunMillis
	}
	return c, nil
}
