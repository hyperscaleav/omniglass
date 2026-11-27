package sim

import (
	"testing"

	simdevice "github.com/hyperscaleav/omniglass/sim/device"
	"github.com/hyperscaleav/omniglass/sim/profiles"
)

// shippedEngine is an engine over the shipped catalog: the embedded YAML
// profiles and every Go family.
func shippedEngine(t testing.TB) *Engine {
	t.Helper()
	cat, err := simdevice.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	return NewEngineOver(cat, profiles.Templates())
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
