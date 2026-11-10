package lab

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// linearShape is a scenario whose sites grow by one every ten systems and
// whose systems hold three components each.
func linearShape(systems int) (int, int, error) { return 1 + systems/10, 3 * systems, nil }

var testCost = CostModel{
	Base:      Footprint{MemMiB: 100, CPU: 1},
	Node:      Footprint{MemMiB: 10, CPU: 0.1},
	Component: Footprint{MemMiB: 1, CPU: 0.01},
}

func TestEstimateAddsANodePerSiteAndACostPerComponent(t *testing.T) {
	got := testCost.Estimate(3, 300)
	if got.MemMiB != 100+30+300 || got.CPU < 4.29 || got.CPU > 4.31 {
		t.Fatalf("estimate %+v, want 430 MiB and 4.3 cores", got)
	}
}

// The host keeps its headroom: a lab may plan on 80% of it, of memory and of
// CPU alike, and one past either does not fit.
func TestFitsLeavesTheHostItsHeadroom(t *testing.T) {
	h := HostResources{MemBytes: 1000 << 20, CPUs: 10}
	for name, tc := range map[string]struct {
		need Footprint
		fits bool
	}{
		"inside both":      {Footprint{MemMiB: 800, CPU: 8}, true},
		"past the memory":  {Footprint{MemMiB: 801, CPU: 1}, false},
		"past the CPUs":    {Footprint{MemMiB: 1, CPU: 8.1}, false},
		"the whole memory": {Footprint{MemMiB: 1000, CPU: 1}, false},
	} {
		if got := h.Fits(tc.need); got != tc.fits {
			t.Errorf("%s: fits %v, want %v", name, got, tc.fits)
		}
	}
}

func TestLargestFitIsTheLargestSizeTheHostCanTake(t *testing.T) {
	h := HostResources{MemBytes: 1000 << 20, CPUs: 100}
	got, ok, err := LargestFit(linearShape, 10, 3000, testCost, h)
	if err != nil || !ok {
		t.Fatalf("got %d %v %v", got, ok, err)
	}
	need := func(n int) Footprint { s, c, _ := linearShape(n); return testCost.Estimate(s, c) }
	if !h.Fits(need(got)) || h.Fits(need(got+1)) {
		t.Fatalf("largest fit %d: it fits %v, the next fits %v", got, h.Fits(need(got)), h.Fits(need(got+1)))
	}
	if _, ok, _ := LargestFit(linearShape, 10, 3000, testCost, HostResources{MemBytes: 1 << 20, CPUs: 1}); ok {
		t.Fatal("a host too small for the smallest size had a fit")
	}
	if got, ok, _ := LargestFit(linearShape, 10, 3000, testCost, HostResources{MemBytes: 1 << 40, CPUs: 1000}); !ok || got != 3000 {
		t.Fatalf("an ample host fits %d %v, want the largest size", got, ok)
	}
	boom := errors.New("boom")
	if _, _, err := LargestFit(func(int) (int, int, error) { return 0, 0, boom }, 10, 3000, testCost, h); !errors.Is(err, boom) {
		t.Fatalf("a shape's error: %v", err)
	}
}

// The assessment --plan prints and bring-up acts on: the estimate against
// what the host can spare, and a refusal naming the largest size that fits.
func TestAssessCapacityDecides(t *testing.T) {
	sc := ScaleRun{Shape: linearShape, MinSystems: 10, MaxSystems: 3000, Cost: testCost}
	host := HostResources{MemBytes: 600 << 20, CPUs: 100}
	fits, err := AssessCapacity(sc, host, 50, 6, 150)
	if err != nil || !fits.Fits || fits.Err() != nil {
		t.Fatalf("50 systems on 480 usable MiB: %+v %v", fits, err)
	}
	if want := "50 systems: 6 sites, 150 components; about 310 MiB and 3.1 cores of the 480 MiB and 80.0 cores this host can spare"; fits.Summary() != want {
		t.Fatalf("summary %q, want %q", fits.Summary(), want)
	}
	big, err := AssessCapacity(sc, host, 1000, 101, 3000)
	if err != nil || big.Fits || big.Largest != 93 {
		t.Fatalf("1000 systems: %+v %v, want refused with 93 the largest", big, err)
	}
	var pe *PreflightError
	if !errors.As(big.Err(), &pe) || !strings.Contains(pe.Fix, "--systems 93, the largest that fits") {
		t.Fatalf("refusal %v", big.Err())
	}
}

// The perf gate compares rates read over a window, so a window that cuts a
// poll's cycle short reads a different share of it run to run (#1640). Each
// gated size's window is held to a whole multiple of every interval its lab
// runs on: each driver poll of each product its scenario places, and the
// lab cadence's own timers.
func TestPerfWindowIsAWholeMultipleOfEveryPollInterval(t *testing.T) {
	sizes := PerfSizes()
	if len(sizes) == 0 {
		t.Fatal("PerfSizes is empty")
	}
	for _, sz := range sizes {
		every, err := ScenarioPollIntervals(sz.Scenario, sz.Systems)
		if err != nil {
			t.Fatalf("%s: %v", sz.Name, err)
		}
		if len(every) == 0 {
			t.Fatalf("%s: no poll intervals read for %s at %d systems", sz.Name, sz.Scenario, sz.Systems)
		}
		for _, d := range every {
			if d <= 0 || sz.Window%d != 0 {
				t.Errorf("%s: the %s window is not a whole multiple of the %s interval", sz.Name, sz.Window, d)
			}
		}
	}
}

func TestScenarioPollIntervalsHoldTheDriverPollsAndTheCadence(t *testing.T) {
	every, err := ScenarioPollIntervals("corporate", 3)
	if err != nil {
		t.Fatal(err)
	}
	cad, err := LookupCadence(CadenceLab, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[time.Duration]bool{}
	for _, s := range []string{cad.Heartbeat, cad.ProbeEvery, cad.WorklistEvery, cad.SimPoll, cad.ClosedLoopBracket} {
		d, _ := time.ParseDuration(s)
		want[d] = true
	}
	got := map[time.Duration]bool{}
	for _, d := range every {
		got[d] = true
	}
	for d := range want {
		if !got[d] {
			t.Errorf("ScenarioPollIntervals = %v, want the lab cadence's %s among them", every, d)
		}
	}
	if !got[time.Minute] {
		t.Errorf("ScenarioPollIntervals = %v, want the platform's 1m report window among them", every)
	}
	if _, err := ScenarioPollIntervals("nope", 3); err == nil {
		t.Error("ScenarioPollIntervals(nope) = nil error, want the unknown scenario refused")
	}
}

// A baseline is only comparable to a run of the same fleet. A fixed scenario
// and size resolve to the same device set every time, so two builds of the
// same catalog place the same devices, and the plan digest a baseline records
// says when a catalog change has moved them.
func TestPerfSizeResolvesToTheSameDeviceSet(t *testing.T) {
	for _, sz := range PerfSizes() {
		a, err := PerfPlanDigest(sz.Scenario, sz.Systems)
		if err != nil {
			t.Fatalf("%s: %v", sz.Name, err)
		}
		b, err := PerfPlanDigest(sz.Scenario, sz.Systems)
		if err != nil {
			t.Fatal(err)
		}
		if a == "" || a != b {
			t.Errorf("%s: two digests of %s at %d systems: %q and %q, want one", sz.Name, sz.Scenario, sz.Systems, a, b)
		}
	}
	small, _ := PerfPlanDigest("corporate", 3)
	large, _ := PerfPlanDigest("corporate", 20)
	if small == large {
		t.Error("corporate at 3 and at 20 digest alike, want the device set to tell them apart")
	}
}

func TestPerfSizeNamed(t *testing.T) {
	for _, sz := range PerfSizes() {
		got, err := PerfSizeNamed(sz.Name)
		if err != nil || !reflect.DeepEqual(got, sz) {
			t.Errorf("PerfSizeNamed(%s) = %+v, %v", sz.Name, got, err)
		}
		if sz.Warmup <= 0 || sz.FleetReads < 1 || sz.Systems < 1 || !strings.HasSuffix(sz.BaselineFile(), ".json") {
			t.Errorf("size %s is not a whole size: %+v", sz.Name, sz)
		}
	}
	if _, err := PerfSizeNamed("huge"); err == nil || !strings.Contains(err.Error(), "ci") {
		t.Errorf("PerfSizeNamed(huge) = %v, want a refusal naming the sizes", err)
	}
}
