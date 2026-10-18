package devseed

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/simfleet"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A driver that has a spec and simulated products is one the seed attaches,
// not probes: when a stub driver gains its spec, this fails until simfleet.Attach
// learns its inputs.
func TestSpecdDriversWithSimulatedProductsAreAttached(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	store, err := profile.LoadStoreFS(profiles.FS)
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
		if _, _, ok := store.ForProduct(p.Name); !ok {
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
