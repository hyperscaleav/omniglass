package devseed_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
)

// The dev fleet breaks some simulated endpoints on purpose, for a learner to
// find and fix (a pinned host key that no longer matches, a community the
// agent refuses). The lab's readiness does not wait on them, and it reads
// them from the seed, never from a list kept beside it.
func TestPlantedFaultsAreReadFromTheSeed(t *testing.T) {
	dev, err := devseed.LookupScenario("dev")
	if err != nil {
		t.Fatal(err)
	}
	faults, err := devseed.PlantedFaults(dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(faults) == 0 {
		t.Fatal("the dev fleet plants no fault; the walkthroughs that fix one have nothing to fix")
	}
	for _, f := range faults {
		if f.Component == "" || f.Driver == "" || f.Label == "" || f.Why == "" {
			t.Errorf("planted fault %+v: want a component, a driver, a label and why", f)
		}
	}
	empty, err := devseed.LookupScenario("empty")
	if err != nil {
		t.Fatal(err)
	}
	if faults, err := devseed.PlantedFaults(empty); err != nil || len(faults) != 0 {
		t.Fatalf("the empty scenario: %v (%v), want no planted fault", faults, err)
	}
}
