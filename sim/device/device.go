// Package device is the registry of the simulator's Go device families
// (#2829). A family is a package under sim/device/<family> that exports
// `func Spec() spec.Spec`: one wire API, its models as a typed table. The list
// of them is generated (families_gen.go, by `make gen`: cmd/familygen scans
// this directory) and never hand-kept.
//
// Catalog is what the simulator and its consumers read: the embedded YAML
// profiles and these families together, a product resolving to a family when
// one claims it, else to its YAML profile.
package device

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Families is every Go device family, as generated.
func Families() []spec.Spec { return families() }

// Catalog is the shipped simulator catalog: the embedded YAML profiles and
// every Go family.
func Catalog() (*spec.Catalog, error) { return CatalogWith() }

// CatalogWith is the shipped catalog with extra families beside the shipped
// ones: a test's own family, served the way a shipped one is.
func CatalogWith(extra ...spec.Spec) (*spec.Catalog, error) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		return nil, fmt.Errorf("load the simulator profiles: %w", err)
	}
	return CatalogOver(store, extra...)
}

// CatalogOver is a YAML store beside every shipped family and any extra: the
// catalog of a test that shaped the store first (a profile's face altered).
func CatalogOver(store *profile.Store, extra ...spec.Spec) (*spec.Catalog, error) {
	cat, err := spec.NewCatalog(store, append(Families(), extra...)...)
	if err != nil {
		return nil, fmt.Errorf("register the simulator families: %w", err)
	}
	return cat, nil
}
