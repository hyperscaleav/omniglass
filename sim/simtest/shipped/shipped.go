// Package shipped builds the simulator over the shipped catalog for tests
// (#2845): every Go device family, a product resolving to the family that
// claims it. Every test outside sim reaches the catalog through here, so a
// family's change needs no per-test churn. internal/build's guard keeps it so.
//
// A test inside package sim cannot import this (it imports sim); it uses the
// in-package shippedEngine helper over the same catalog.
package shipped

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/device"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Catalog is the shipped catalog: the Go families, and any extra family a test
// serves beside them.
func Catalog(t testing.TB, extra ...spec.Spec) *spec.Catalog {
	t.Helper()
	cat, err := device.CatalogWith(extra...)
	if err != nil {
		t.Fatalf("the shipped simulator catalog: %v", err)
	}
	return cat
}

// Engine is an engine over the shipped catalog, with any extra family a test
// serves beside it.
func Engine(t testing.TB, extra ...spec.Spec) *sim.Engine {
	t.Helper()
	return sim.NewEngineOver(Catalog(t, extra...))
}
