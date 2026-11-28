package api

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The simulation routes describe a Go family's product exactly as the YAML
// twin's (#2829): the same surface (each protocol, port, route and command
// with its summary and example), the same behaviors, the same products and
// profile listing entry, bar the names that tell the two apart.
func TestSimulationRoutesDescribeAFamilyAsTheyDescribeItsYAMLTwin(t *testing.T) {
	t.Parallel()
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range spectest.Pairs {
		g, ok := cat.ProfileFor(p.Go)
		if !ok {
			t.Fatalf("%s: no definition", p.Go)
		}
		y, ok := cat.ProfileFor(p.YAML)
		if !ok {
			t.Fatalf("%s: no definition", p.YAML)
		}
		gs, ys := simSurface(g, 10000), simSurface(y, 10000)
		if len(gs) != 3 || len(gs[0].Routes) != 2 || len(gs[1].Commands) != 1 {
			t.Fatalf("%s: surface = %+v, want the http routes and the telnet command listed", p.Go, gs)
		}
		if !reflect.DeepEqual(gs, ys) {
			t.Errorf("%s: surface = %+v, twin %+v", p.Go, gs, ys)
		}
		if !reflect.DeepEqual(simBehaviors(g), simBehaviors(y)) {
			t.Errorf("%s: behaviors differ", p.Go)
		}
		// A YAML profile leads with the product it is named for; a family lists
		// its model products only (its name is its wire API, not a product), so
		// the family's variants are the twin's variants after that lead.
		gv, yv := variantsOf(g), variantsOf(y)
		if len(gv) != len(yv)-1 || yv[0] != spectest.TwinName || slices.Contains(gv, spectest.Name) {
			t.Errorf("%s: variants = %v, twin %v", p.Go, gv, yv)
		}
	}

	byName := map[string]simProfileBody{}
	for _, b := range simProfileBodies(cat) {
		byName[b.Name] = b
	}
	g, y := byName[spectest.Name], byName[spectest.TwinName]
	if g.Name == "" || y.Name == "" {
		t.Fatalf("profiles = %+v", byName)
	}
	if g.Manufacturer != y.Manufacturer || g.DeviceType != y.DeviceType || g.Summary != y.Summary || !reflect.DeepEqual(g.Protocols, y.Protocols) {
		t.Errorf("profile listing = %+v, twin %+v", g, y)
	}
	for _, p := range g.Products {
		if strings.HasPrefix(p, "tf-yaml") {
			t.Errorf("the family lists the twin's product %s", p)
		}
	}
}
