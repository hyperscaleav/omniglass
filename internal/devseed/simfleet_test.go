package devseed

import (
	"context"
	simdevice "github.com/hyperscaleav/omniglass/sim/device"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/sim/simfleet"
)

// A driver that has a spec and simulated products is one the seed attaches,
// not probes: when a stub driver gains its spec, this fails until simfleet.Attach
// learns its inputs.
func TestSpecdDriversWithSimulatedProductsAreAttached(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, BootSeededDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	cat, err := simdevice.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	drivers, err := gw.ListDrivers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	specd := map[string]bool{}
	for _, d := range drivers {
		if len(d.Spec) > 0 {
			specd[d.Name] = true
		}
	}
	products, err := gw.ListProducts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, p := range products {
		if p.DriverName == nil || !specd[*p.DriverName] {
			continue
		}
		if _, ok := cat.ForProduct(p.Name); !ok {
			continue
		}
		checked++
		if _, ok := simfleet.Attach[*p.DriverName]; !ok {
			t.Errorf("%s is simulated and its driver %s has a spec, but the dev seed would only probe it", p.Name, *p.DriverName)
		}
	}
	if checked == 0 {
		t.Fatal("no simulated product on a spec'd driver; the check checked nothing")
	}
}
