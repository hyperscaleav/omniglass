// Package shipped builds the simulator over the shipped catalog for tests
// (#2845): the embedded YAML profiles and every Go device family, a product
// resolving to a family when one claims it. A test that builds an engine from
// the YAML profile store alone (sim.NewEngine over profile.LoadStoreFS) cannot
// see a product once its profile became a family, so every test outside
// sim/profile reaches the catalog through here, and a family's conversion
// needs no per-test churn. internal/build's guard keeps it so.
//
// A test inside package sim cannot import this (it imports sim); it uses the
// in-package shippedEngine helper over the same catalog.
package shipped

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/device"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Catalog is the shipped catalog: the YAML profiles and the Go families, and
// any extra family a test serves beside them.
func Catalog(t testing.TB, extra ...spec.Spec) *spec.Catalog {
	t.Helper()
	cat, err := device.CatalogWith(extra...)
	if err != nil {
		t.Fatalf("the shipped simulator catalog: %v", err)
	}
	return cat
}

// CatalogFrom is the catalog of a store a test shaped first (a profile copied
// and altered, a variant added), served beside every shipped Go family and any
// extra family the test brings. The store is the test's own; the families are
// the shipped ones.
func CatalogFrom(t testing.TB, store *profile.Store, extra ...spec.Spec) *spec.Catalog {
	t.Helper()
	cat, err := spec.NewCatalog(store, append(device.Families(), extra...)...)
	if err != nil {
		t.Fatalf("a catalog of the shaped store and the shipped families: %v", err)
	}
	return cat
}

// Engine is an engine over the shipped catalog, with the shipped templates.
func Engine(t testing.TB) *sim.Engine {
	t.Helper()
	return sim.NewEngineOver(Catalog(t), profiles.Templates())
}
