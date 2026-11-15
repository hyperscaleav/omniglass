package labfootprint

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/simfleet"
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

// The sweep plan takes every driver once: a driver with polls, a shipped
// product on it, a simulator profile for that product and a simulated attach
// is swept on its first such product; every other driver is listed with the
// first reason it cannot be, in the order the checks run.
func TestASweepPlanTakesEveryDriverOnceWithAReason(t *testing.T) {
	polls := map[string][]time.Duration{
		"swept":      {5 * time.Minute, 30 * time.Second},
		"listens":    {},
		"no-product": {time.Minute},
		"no-sim":     {time.Minute},
		"no-attach":  {time.Minute},
	}
	products := []seed.ShippedProduct{
		{Name: "unsimulated", Driver: "swept"},
		{Name: "first", Driver: "swept"},
		{Name: "second", Driver: "swept"},
		{Name: "listener", Driver: "listens"},
		{Name: "dark", Driver: "no-sim"},
		{Name: "probed", Driver: "no-attach"},
	}
	simulated := func(p string) bool { return p != "unsimulated" && p != "dark" }
	attachable := func(d string) bool { return d != "no-attach" }
	targets, skips := PlanDriverSweep(polls, products, simulated, attachable)
	if len(targets) != 1 || targets[0].Driver != "swept" || targets[0].Product != "first" || len(targets[0].Polls) != 2 {
		t.Fatalf("targets %+v, want swept on its first simulated product", targets)
	}
	want := map[string]string{
		"listens":    SkipNoPolls,
		"no-product": SkipNoProduct,
		"no-sim":     SkipNoSimulator,
		"no-attach":  SkipNoAttach,
	}
	if len(skips) != len(want) {
		t.Fatalf("skips %+v, want %d", skips, len(want))
	}
	for i, s := range skips {
		if want[s.Driver] != s.Reason {
			t.Errorf("%s: reason %q, want %q", s.Driver, s.Reason, want[s.Driver])
		}
		if i > 0 && skips[i-1].Driver > s.Driver {
			t.Errorf("skips not in driver order: %s before %s", skips[i-1].Driver, s.Driver)
		}
	}
}

// Against the shipped seed and the simulator's own profiles, the plan covers
// every shipped driver exactly once, and each swept product is one the
// simulator serves on that driver.
func TestTheShippedSweepPlanCoversEverySeededDriver(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	polls := seed.ShippedDriverPolls()
	products := seed.ShippedProducts()
	targets, skips := ShippedDriverSweep()
	seen := map[string]bool{}
	for _, tg := range targets {
		if seen[tg.Driver] {
			t.Errorf("%s swept twice", tg.Driver)
		}
		seen[tg.Driver] = true
		if _, _, ok := store.ForProduct(tg.Product); !ok {
			t.Errorf("%s swept on %s, which no profile simulates", tg.Driver, tg.Product)
		}
		if _, ok := simfleet.Attach[tg.Driver]; !ok {
			t.Errorf("%s swept with no simulated attach", tg.Driver)
		}
		if len(tg.Polls) == 0 || len(tg.Polls) != len(polls[tg.Driver]) {
			t.Errorf("%s swept with polls %v, its spec has %v", tg.Driver, tg.Polls, polls[tg.Driver])
		}
		ok := false
		for _, p := range products {
			ok = ok || (p.Name == tg.Product && p.Driver == tg.Driver)
		}
		if !ok {
			t.Errorf("%s swept on %s, which is not a shipped product on it", tg.Driver, tg.Product)
		}
	}
	for _, s := range skips {
		if seen[s.Driver] {
			t.Errorf("%s both swept and skipped", s.Driver)
		}
		seen[s.Driver] = true
		if s.Reason == "" {
			t.Errorf("%s skipped with no reason", s.Driver)
		}
	}
	for d := range polls {
		if !seen[d] {
			t.Errorf("shipped driver %s is neither swept nor listed as not measured", d)
		}
	}
	if len(targets) == 0 {
		t.Fatal("the shipped plan sweeps nothing")
	}
}

// A driver's cost comes out of its steps: memory and CPU per task are the
// slopes, and CPU per run divides CPU per task by how often one task runs.
// The thread clock's figure per run sits beside the cgroup's, with their
// ratio, so the floor (#1870) can be read against the whole cost.
func TestADriversCostIsItsSlopesPerTaskAndPerRun(t *testing.T) {
	// Two polls, one every 30 s and one every 5 m: a task runs on average
	// (1/30 + 1/300) / 2 times a second.
	target := DriverTarget{Driver: "d", Product: "p", Polls: []time.Duration{30 * time.Second, 5 * time.Minute}}
	rate := (1.0/30 + 1.0/300) / 2
	// 0.4 millicores per task is 0.4 ms of CPU a second, so 0.4 / rate ms
	// per run.
	steps := []DriverStep{
		{Components: 0, Tasks: 0, WorkingSetBytes: 30e6, Millicores: 5},
		{Components: 100, Tasks: 200, WorkingSetBytes: 34e6, Millicores: 5 + 0.4*200, PollCPUMillis: 3},
		{Components: 200, Tasks: 400, WorkingSetBytes: 38e6, Millicores: 5 + 0.4*400, PollCPUMillis: 5},
		{Components: 300, Tasks: 600, WorkingSetBytes: 42e6, Millicores: 5 + 0.4*600, PollCPUMillis: 4},
	}
	cost, err := AssembleDriverCost(target, steps, 0.1, 0.2)
	if err != nil {
		t.Fatal(err)
	}
	if !near(rate, target.RunsPerSecondPerTask()) {
		t.Fatalf("runs per second per task %v, want %v", target.RunsPerSecondPerTask(), rate)
	}
	perRun := 0.4 / rate
	// The thread clock's mean is over runs, so the step with more tasks
	// weighs more: (3*200 + 5*400 + 4*600) / 1200.
	thread := (3.0*200 + 5.0*400 + 4.0*600) / 1200
	if !near(cost.MemoryPerTaskBytes, 20_000) || !near(cost.MillicoresPerTask, 0.4) ||
		!near(cost.CPUPerRunMillis, perRun) || !near(cost.ThreadCPUPerRunMillis, thread) ||
		!near(cost.ThreadShare, thread/perRun) || cost.Tasks != 600 {
		t.Fatalf("cost %+v, want 20,000 B and 0.4 mc per task, %.3f ms per run (thread %.3f ms)", cost, perRun, thread)
	}
	if cost.MemoryWarmBytes != 0 || cost.MemoryNotMeasured != "" || cost.CPUNotMeasured != "" {
		t.Fatalf("a straight sweep has no warm cost and both figures: %+v", cost)
	}
	if strings.Join(cost.Polls, ",") != "30s,5m0s" {
		t.Fatalf("polls %v", cost.Polls)
	}
}

// A node's memory jumps when its first tasks land (its lanes start, its heap
// grows to its working size) and then rises by a slope. The slope is fitted
// over the loaded steps only, and the jump is the warm cost: the loaded
// line at zero tasks over the idle node. CPU has no such jump and is fitted
// over every step. The read that found it: an SNMP driver's working set
// went 15.8, 25.9, 27.4 MiB at 0, 400, 800 tasks.
func TestAWarmUpJumpIsAFixedCostNotPartOfTheSlope(t *testing.T) {
	target := DriverTarget{Driver: "d", Product: "p", Polls: []time.Duration{time.Minute}}
	const mb = 1 << 20
	steps := []DriverStep{
		{Tasks: 0, WorkingSetBytes: 15.8 * mb, Millicores: 0.5},
		{Tasks: 400, WorkingSetBytes: 25.9 * mb, Millicores: 21.2, PollCPUMillis: 2},
		{Tasks: 800, WorkingSetBytes: 27.4 * mb, Millicores: 41.4, PollCPUMillis: 2},
		{Tasks: 1200, WorkingSetBytes: 28.9 * mb, Millicores: 61.6, PollCPUMillis: 2},
	}
	cost, err := AssembleDriverCost(target, steps, 0.1, 0.25)
	if err != nil {
		t.Fatal(err)
	}
	if cost.MemoryNotMeasured != "" || !near(cost.MemoryPerTaskBytes, 1.5*mb/400) {
		t.Fatalf("memory %+v, want the loaded steps' slope of %.0f B a task", cost, 1.5*mb/400)
	}
	if !near(cost.MemoryWarmBytes, 24.4*mb-15.8*mb) {
		t.Fatalf("warm %.0f B, want the loaded line at zero (24.4 MiB) over idle (15.8 MiB)", cost.MemoryWarmBytes)
	}
	// Least squares over all four steps: 40,700 / 800,000.
	if cost.CPUNotMeasured != "" || !near(cost.MillicoresPerTask, 0.050875) {
		t.Fatalf("cpu %+v, want 0.050875 mc a task over every step", cost)
	}
}

// Memory and CPU are judged apart: a figure that is not a line is not
// written down, with its reason, and the other still is. Only a driver with
// neither is refused.
func TestMemoryAndCPUAreJudgedApart(t *testing.T) {
	target := DriverTarget{Driver: "d", Product: "p", Polls: []time.Duration{time.Minute}}
	line := func() []DriverStep {
		return []DriverStep{
			{Tasks: 0, WorkingSetBytes: 30e6, Millicores: 5},
			{Tasks: 200, WorkingSetBytes: 34e6, Millicores: 85, PollCPUMillis: 3},
			{Tasks: 400, WorkingSetBytes: 38e6, Millicores: 165, PollCPUMillis: 3},
			{Tasks: 600, WorkingSetBytes: 42e6, Millicores: 245, PollCPUMillis: 3},
		}
	}
	bentMem := line()
	bentMem[2].WorkingSetBytes = 50e6
	cost, err := AssembleDriverCost(target, bentMem, 0.1, 0.2)
	if err != nil {
		t.Fatalf("a bent memory sweep refused the driver: %v", err)
	}
	if !strings.Contains(cost.MemoryNotMeasured, "not a line") || cost.MemoryPerTaskBytes != 0 || cost.MemoryWarmBytes != 0 {
		t.Fatalf("bent memory %+v, want no memory figure and its reason", cost)
	}
	if cost.CPUNotMeasured != "" || !near(cost.MillicoresPerTask, 0.4) {
		t.Fatalf("bent memory %+v, want the cpu figure kept", cost)
	}

	bentCPU := line()
	bentCPU[1].Millicores = 400
	cost, err = AssembleDriverCost(target, bentCPU, 0.1, 0.2)
	if err != nil {
		t.Fatalf("a bent cpu sweep refused the driver: %v", err)
	}
	if !strings.Contains(cost.CPUNotMeasured, "not a line") || cost.MillicoresPerTask != 0 || cost.CPUPerRunMillis != 0 || cost.ThreadShare != 0 {
		t.Fatalf("bent cpu %+v, want no cgroup cpu figure and its reason", cost)
	}
	if cost.MemoryNotMeasured != "" || !near(cost.MemoryPerTaskBytes, 20_000) || !near(cost.ThreadCPUPerRunMillis, 3) {
		t.Fatalf("bent cpu %+v, want memory and the thread clock kept", cost)
	}

	both := line()
	both[2].WorkingSetBytes = 50e6
	both[1].Millicores = 400
	if _, err := AssembleDriverCost(target, both, 0.1, 0.2); err == nil ||
		!strings.Contains(err.Error(), "memory") || !strings.Contains(err.Error(), "cpu") {
		t.Fatalf("both bent: %v, want a refusal naming memory and cpu", err)
	}
}

// A loaded line that starts below the idle node is no warm cost: the first
// tasks filled heap the idle node already held, so the jump does not show.
// The memory slope still stands; the warm figure is left out, with why. The
// read that found it: a session driver's working set went 30.3, 83.8, 144.8,
// 206 MiB at 0, 400, 800, 1200 tasks, a line that meets zero tasks 18 MiB
// under the idle node.
func TestAWarmBelowIdleIsNotAWarmCost(t *testing.T) {
	target := DriverTarget{Driver: "d", Product: "p", Polls: []time.Duration{time.Minute}}
	const mb = 1 << 20
	steps := []DriverStep{
		{Tasks: 0, WorkingSetBytes: 30.3 * mb, Millicores: 1},
		{Tasks: 400, WorkingSetBytes: 83.8 * mb, Millicores: 41, PollCPUMillis: 2},
		{Tasks: 800, WorkingSetBytes: 144.8 * mb, Millicores: 81, PollCPUMillis: 2},
		{Tasks: 1200, WorkingSetBytes: 206 * mb, Millicores: 121, PollCPUMillis: 2},
	}
	cost, err := AssembleDriverCost(target, steps, 0.1, 0.25)
	if err != nil {
		t.Fatal(err)
	}
	if cost.MemoryNotMeasured != "" || cost.MemoryPerTaskBytes <= 0 {
		t.Fatalf("memory %+v, want the slope kept", cost)
	}
	if cost.MemoryWarmBytes != 0 || !strings.Contains(cost.WarmNotMeasured, "below the idle node") {
		t.Fatalf("warm %.0f B, %q; want no warm figure and why", cost.MemoryWarmBytes, cost.WarmNotMeasured)
	}
}
