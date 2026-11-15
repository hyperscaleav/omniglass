package lab

import (
	"errors"
	"math"
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
	need := func(n int) Footprint { s, c, _ := linearShape(n); n2, _ := testCost.Need(h, s, c); return n2 }
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

// A scale lab's Postgres takes its shared buffers of the host on top of the
// fleet's estimate (#1930): a quarter of the memory it is sized to, which is
// never more than the host reported, however high a forced limit, and never
// more than the estimate leaves of what the host can spare. Below that sits
// Postgres's own 128 MB, the size the estimate was measured with, so it is
// not counted again: a fleet that fits without the buffers still fits.
func TestNeedCountsPostgresSharedBuffers(t *testing.T) {
	for name, tc := range map[string]struct {
		host             HostResources
		buffers, counted int64
	}{
		"the host's own":         {HostResources{MemBytes: 1000 << 20}, 250, 250},
		"a forced limit below":   {HostResources{MemBytes: 1000 << 20, ReportedMemBytes: 4000 << 20}, 250, 250},
		"a forced limit above":   {HostResources{MemBytes: 4000 << 20, ReportedMemBytes: 1000 << 20}, 250, 250},
		"a large host's ceiling": {HostResources{MemBytes: 64 << 30}, 8 << 10, 8 << 10},
		// 560 usable MiB less the 430 MiB estimate.
		"what the fleet leaves spare": {HostResources{MemBytes: 700 << 20}, 130, 130},
		// 480 usable MiB leave 50: the floor, already in the estimate.
		"the floor": {HostResources{MemBytes: 600 << 20}, 128, 0},
	} {
		need, buffers := testCost.Need(tc.host, 3, 300)
		if need.MemMiB != 430+float64(tc.counted) || need.CPU != testCost.Estimate(3, 300).CPU {
			t.Errorf("%s: need %+v, want the 430 MiB estimate and %d MiB of buffers", name, need, tc.counted)
		}
		if buffers != tc.buffers {
			t.Errorf("%s: buffers %d MiB, want %d", name, buffers, tc.buffers)
		}
	}
	// The buffers never refuse a fleet the estimate fits.
	for mib := int64(500); mib <= 1000; mib += 10 {
		h := HostResources{MemBytes: mib << 20, CPUs: 100}
		need, _ := testCost.Need(h, 3, 300)
		if h.Fits(testCost.Estimate(3, 300)) != h.Fits(need) {
			t.Errorf("%d MiB host: the estimate fits %v, the need %v", mib, h.Fits(testCost.Estimate(3, 300)), h.Fits(need))
		}
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
	if want := "50 systems: 6 sites, 150 components; about 460 MiB (Postgres's 150 MiB of shared buffers in it) and 3.1 cores of the 480 MiB and 80.0 cores this host can spare"; fits.Summary() != want {
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
	if _, err := PerfSizeNamed("ci"); err == nil || !strings.Contains(err.Error(), "300, 1000") {
		t.Errorf("PerfSizeNamed(ci) = %v, want a refusal naming the rungs", err)
	}
}

// Each size times enough fleet view reads that its nearest-rank p95 has five
// reads above it, so a slow read or two (a collection pause, a neighbour's
// burst) cannot set the gated figure. At 20 reads the p95 was the second
// slowest, and the ci baseline's runs spread 37 percent on it (#1751).
func TestPerfSizesReadTheFleetViewEnoughForItsP95(t *testing.T) {
	for _, sz := range PerfSizes() {
		n := sz.FleetReads
		rank := int(math.Ceil(0.95 * float64(n)))
		if above := n - rank; above < 5 {
			t.Errorf("size %s times %d fleet view reads: its p95 has %d above it, want at least 5 (100 reads)", sz.Name, n, above)
		}
	}
}
