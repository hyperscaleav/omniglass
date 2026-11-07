package seed

import (
	"slices"
	"testing"
)

// The accessors read the shipped catalog without a database. The test names
// no seeded product (#804): it holds the catalog to its own shape instead,
// every product a shipped component type and every parent a shipped type.
func TestShippedCatalogAccessors(t *testing.T) {
	products := ShippedProducts()
	if len(products) == 0 {
		t.Fatal("ShippedProducts is empty")
	}
	parents := ShippedComponentTypeParents()
	for _, p := range products {
		if p.Name == "" {
			t.Errorf("a shipped product has no name: %+v", p)
		}
		if _, ok := parents[p.ComponentType]; !ok {
			t.Errorf("product %s is a %s, which is no shipped component type", p.Name, p.ComponentType)
		}
	}
	for ct, parent := range parents {
		if parent == "" {
			continue
		}
		if _, ok := parents[parent]; !ok {
			t.Errorf("component type %s has parent %q, which is no shipped component type", ct, parent)
		}
	}
	if types := ShippedSystemTypes(); !slices.Contains(types, "meeting") || !slices.Contains(types, "sign") {
		t.Errorf("ShippedSystemTypes = %v, want meeting and sign among them", types)
	}
}
