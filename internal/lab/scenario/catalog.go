package scenario

import (
	"fmt"
	"sync"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/template"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
)

// Catalog is what a manifest's names are checked against: the standards, the
// products and their component types, which products the simulator serves,
// and the system types. A test hands in a small one; ShippedCatalog is the
// platform's own.
type Catalog struct {
	// Standard returns the standard template of that name.
	Standard func(name string) (*template.Standard, bool)
	// Product returns the component type the product is classified under.
	Product func(name string) (componentType string, ok bool)
	// Simulated says whether a simulator profile serves the product.
	Simulated func(product string) bool
	// Within says whether a component type falls within an accepted type,
	// self-inclusive.
	Within func(componentType, accepted string) bool
	// SystemType says whether a system type of that name is registered.
	SystemType func(name string) bool
	// Pusher says whether an emulator profile of that name has a push face,
	// so a scenario may declare it as a pusher (#1743).
	Pusher func(profile string) bool
	// Sender says whether an emulator profile of that name is a webhook
	// sender a scenario may declare (#1747): it has a webhook face naming
	// the driver that receives it, and a product of the same name the
	// simulator serves, so the lab seeds it as a component.
	Sender func(profile string) bool
	// Stem returns the stem the platform mints a product's components' names
	// from (its component type's, inherited), so a plan can say what a
	// component will be called before it exists. Empty or false: none.
	Stem func(product string) (string, bool)
	// SimProfile returns the simulator profile that serves the product, as
	// the simulator itself picks it.
	SimProfile func(product string) (string, bool)
}

var (
	shippedOnce sync.Once
	shippedCat  Catalog
	shippedErr  error
)

// ShippedCatalog is the catalog every fleet is seeded with: the template
// catalog's standards, the seeded products, component types and system types,
// and the simulator's profiles.
func ShippedCatalog() (Catalog, error) {
	shippedOnce.Do(func() { shippedCat, shippedErr = loadShipped() })
	return shippedCat, shippedErr
}

func loadShipped() (Catalog, error) {
	tc := template.Shipped()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		return Catalog{}, fmt.Errorf("scenario: load the simulator profiles: %w", err)
	}
	products := map[string]string{}
	for _, p := range seed.ShippedProducts() {
		products[p.Name] = p.ComponentType
	}
	parents := seed.ShippedComponentTypeParents()
	stems := seed.ShippedComponentTypeStems()
	systemTypes := map[string]bool{}
	for _, st := range seed.ShippedSystemTypes() {
		systemTypes[st] = true
	}
	return Catalog{
		Standard: func(name string) (*template.Standard, bool) {
			t, err := tc.GetKind(name, template.KindStandard)
			if err != nil {
				return nil, false
			}
			return t.Standard, true
		},
		Product: func(name string) (string, bool) {
			ct, ok := products[name]
			return ct, ok
		},
		Simulated: func(product string) bool {
			_, _, ok := store.ForProduct(product)
			return ok
		},
		Stem: func(product string) (string, bool) {
			ct, ok := products[product]
			stem := stems[ct]
			return stem, ok && stem != ""
		},
		SimProfile: func(product string) (string, bool) {
			name, _, ok := store.ForProduct(product)
			return name, ok
		},
		Within:     func(have, accepted string) bool { return within(parents, have, accepted) },
		SystemType: func(name string) bool { return systemTypes[name] },
		Pusher: func(name string) bool {
			p, ok := store.Devices[name]
			return ok && p.Push != nil
		},
		Sender: func(name string) bool {
			p, ok := store.Devices[name]
			_, product := products[name]
			return ok && product && p.Webhook != nil && p.Webhook.Driver != ""
		},
	}, nil
}

// within walks have's ancestry (have included) looking for accepted. The walk
// is bounded by the tree's size, so a cycle in a broken tree ends.
func within(parents map[string]string, have, accepted string) bool {
	for t, steps := have, 0; t != "" && steps <= len(parents); t, steps = parents[t], steps+1 {
		if t == accepted {
			return true
		}
	}
	return false
}
