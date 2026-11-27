package labfootprint

import (
	"reflect"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The driver sweep counts a Go family's product as simulated exactly as the
// YAML twin's (#2829): a driver on either is swept on it.
func TestSweepSimulatesAFamilyProductAsItDoesItsYAMLTwin(t *testing.T) {
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	simulated := simulatedBy(cat)
	for _, p := range spectest.Pairs {
		if !simulated(p.Go) || !simulated(p.YAML) {
			t.Errorf("%s / %s: not both simulated", p.Go, p.YAML)
		}
		plan := func(product string) ([]DriverTarget, []DriverSkip) {
			return PlanDriverSweep(map[string][]time.Duration{"d": {time.Minute}},
				[]seed.ShippedProduct{{Name: product, Driver: "d"}}, simulated, func(string) bool { return true })
		}
		gt, gs := plan(p.Go)
		yt, ys := plan(p.YAML)
		if len(gt) != 1 || len(gs) != 0 || len(yt) != 1 || len(ys) != 0 {
			t.Fatalf("%s: targets %+v skips %+v, twin %+v %+v", p.Go, gt, gs, yt, ys)
		}
		if gt[0].Product != p.Go || yt[0].Product != p.YAML || !reflect.DeepEqual(gt[0].Polls, yt[0].Polls) {
			t.Errorf("%s: target %+v, twin %+v", p.Go, gt[0], yt[0])
		}
	}
	if simulated("nobody") {
		t.Error("an unknown product is simulated")
	}
}
