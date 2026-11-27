package device

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
)

// The shipped catalog holds the embedded YAML profiles and every registered
// family, and a registered family's products resolve to it.
func TestCatalogHoldsTheShippedProfilesAndEveryFamily(t *testing.T) {
	cat, err := Catalog()
	if err != nil {
		t.Fatal(err)
	}
	r, ok := cat.ForProduct("polaris-dsp-16")
	if !ok || r.Definition.Kind() != spec.KindYAML {
		t.Fatalf("polaris-dsp-16 resolved to %+v (ok=%v), want its YAML profile", r, ok)
	}
	for _, fam := range Families() {
		d, ok := cat.Lookup(fam.Name)
		if !ok || d.Kind() != spec.KindGo {
			t.Errorf("family %s is not in the catalog as a Go definition", fam.Name)
		}
		for _, m := range fam.Models {
			if r, ok := cat.ForProduct(m.Product); !ok || r.Definition.ID() != fam.Name {
				t.Errorf("product %s did not resolve to family %s", m.Product, fam.Name)
			}
		}
	}
}
