package api

import (
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The simulation routes describe a Go family's product as the console lists a
// device (#2829): its surface (each protocol, port, route and command with its
// summary and example), its behaviors, its products and its profile listing
// entry.
func TestSimulationRoutesDescribeAFamily(t *testing.T) {
	t.Parallel()
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, product := range []string{"tf-display-55", "tf-display-65"} {
		g, ok := cat.ProfileFor(product)
		if !ok {
			t.Fatalf("%s: no definition", product)
		}
		gs := simSurface(g, 10000)
		if len(gs) != 3 || len(gs[0].Routes) != 2 || len(gs[1].Commands) != 1 {
			t.Fatalf("%s: surface = %+v, want the http routes and the telnet command listed", product, gs)
		}
		if gs[0].Routes[0].Summary == "" {
			t.Errorf("%s: the first route lists no summary", product)
		}
		// A family lists its model products only: its name is its wire API,
		// not a product a component could be.
		if gv := variantsOf(g); len(gv) == 0 || slices.Contains(gv, spectest.Name) {
			t.Errorf("%s: variants = %v, want the models, never the family's name", product, gv)
		}
	}
	byName := map[string]simProfileBody{}
	for _, b := range simProfileBodies(cat) {
		byName[b.Name] = b
	}
	g := byName[spectest.Name]
	if g.Name == "" || g.Manufacturer == "" || g.DeviceType == "" || g.Summary == "" || len(g.Protocols) == 0 {
		t.Fatalf("profile listing = %+v", g)
	}
}
