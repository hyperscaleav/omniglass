package lab

import (
	"errors"
	"testing"
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
