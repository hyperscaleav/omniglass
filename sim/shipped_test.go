package sim

import (
	"testing"

	simdevice "github.com/hyperscaleav/omniglass/sim/device"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// shippedEngine is an engine over the shipped catalog: every Go family.
func shippedEngine(t testing.TB) *Engine {
	t.Helper()
	return NewEngineOver(shippedCatalog(t))
}

// shippedCatalog is the shipped catalog with any extra family beside it. The
// tests outside this package build theirs with sim/simtest/shipped, which
// imports this package and so cannot be imported from here.
func shippedCatalog(t testing.TB, extra ...spec.Spec) *spec.Catalog {
	t.Helper()
	cat, err := simdevice.CatalogWith(extra...)
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

func newShippedEngine(t testing.TB) func() *Engine {
	return func() *Engine { return shippedEngine(t) }
}

// profileOf is the id of the definition that simulates a product, the profile
// name a snapshot of it carries.
func profileOf(t testing.TB, e *Engine, product string) string {
	t.Helper()
	r, ok := e.cat.ForProduct(product)
	if !ok {
		t.Fatalf("the catalog simulates no %s", product)
	}
	return r.Definition.ID()
}
