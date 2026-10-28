package devseed_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// productlessComponents are the dev seed's components with no product, which
// nothing can simulate (the simulator serves products), each with why the fleet
// keeps it. A product's own coverage is the catalog's coverage.yaml.
var productlessComponents = map[string]string{
	"power": "the fleet's example of a component with no catalog product",
}

// TestEverySeededComponentIsSimulatedOrAccountedFor holds the lab to "every
// seeded component is collected" (#1359, #1266). Every component the dev fleet
// places (its sim_components included) has a product a simulator profile
// serves, or one coverage.yaml lists:
//   - unsimulated, with the issue that adds its profile;
//   - passive, with no network interface and how it is watched instead.
//
// A component with no product is listed above. The catalog test
// (internal/sim) holds coverage.yaml itself true; this holds the fleet the lab
// actually places to it, so a fixture adding a component of an unserved,
// unlisted product fails here, naming the product and its components.
func TestEverySeededComponentIsSimulatedOrAccountedFor(t *testing.T) {
	s, err := devseed.LookupScenario("dev")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := s.Doc()
	if err != nil {
		t.Fatal(err)
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	cov, err := profiles.LoadCoverage()
	if err != nil {
		t.Fatal(err)
	}

	byProduct := map[string][]string{}
	keys := map[string]bool{}
	// The components the seed places, and the ones it places only with the
	// simulator on (sim_components: the UCC service, which only a simulator can
	// answer for).
	for _, c := range append(append([]devseed.Component(nil), doc.Components...), doc.SimComponents...) {
		keys[c.Key] = true
		if c.Product == "" {
			if _, ok := productlessComponents[c.Key]; !ok {
				t.Errorf("component %s has no product, so nothing can simulate it: give it a product, or list it in productlessComponents with why", c.Key)
			}
			continue
		}
		byProduct[c.Product] = append(byProduct[c.Product], c.Key)
	}
	for k := range productlessComponents {
		if !keys[k] {
			t.Errorf("productlessComponents lists %s, which the dev seed no longer places: delete its line", k)
		}
	}

	products := make([]string, 0, len(byProduct))
	for p := range byProduct {
		products = append(products, p)
	}
	sort.Strings(products)
	simulated, pending, passive := 0, 0, 0
	pendingProducts := 0
	for _, p := range products {
		comps := byProduct[p]
		_, _, served := store.ForProduct(p)
		_, isPending := cov.Unsimulated[p]
		_, isPassive := cov.Passive[p]
		switch {
		case served && isPending:
			t.Errorf("%s is simulated now; drop it from coverage.yaml's unsimulated", p)
		case served:
			simulated += len(comps)
		case isPending:
			pending += len(comps)
			pendingProducts++
		case isPassive:
			passive += len(comps)
		default:
			t.Errorf("%s is placed (%s) but no simulator profile serves it and coverage.yaml does not list it: write its profile, or list it as unsimulated with its issue (or passive, if it has no network interface)", p, strings.Join(comps, ", "))
		}
	}
	t.Logf("dev fleet: %d components simulated, %d awaiting a profile (%d products), %d passive, %d productless",
		simulated, pending, pendingProducts, passive, len(productlessComponents))
}
