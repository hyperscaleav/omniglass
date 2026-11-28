// Package device is the registry of the simulator's Go device families
// (#2829). A family is a package under sim/device/<family> that exports
// `func Spec() spec.Spec`: one wire API, its models as a typed table. The list
// of them is generated (families_gen.go, by `make gen`: cmd/familygen scans
// this directory) and never hand-kept.
//
// Catalog is what the simulator and its consumers read: every family, a
// product resolving to the family that claims it. There is no other kind of
// device (#2837): nothing is read from a file.
package device

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Families is every Go device family, as generated.
func Families() []spec.Spec { return families() }

// Dirs is the directory of each family under sim/device, in the order Families
// lists them, so what is generated about a family can say where its package is.
func Dirs() []string { return familyDirs() }

// Catalog is the shipped simulator catalog: every Go family.
func Catalog() (*spec.Catalog, error) { return CatalogWith() }

// CatalogWith is the shipped catalog with extra families beside the shipped
// ones: a test's own family, served the way a shipped one is.
func CatalogWith(extra ...spec.Spec) (*spec.Catalog, error) {
	cat, err := spec.NewCatalog(append(Families(), extra...)...)
	if err != nil {
		return nil, fmt.Errorf("register the simulator families: %w", err)
	}
	return cat, nil
}
