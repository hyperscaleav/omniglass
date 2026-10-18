package profile

import (
	"strings"
	"testing"
	"testing/fstest"
)

const familyProfile = `name: family-a
model: "A"
interfaces: []
products:
  - name: family-b
    defaults:
      configuration: { device: { model: "B" } }
defaults:
  configuration: { device: { model: "A", serial: "" } }
`

// A profile serves its own name and the products it lists, each listed
// product with an overlay on its defaults.
func TestStoreResolvesAProductToItsProfile(t *testing.T) {
	store, err := LoadStoreFS(fstest.MapFS{"devices/family-a.yaml": {Data: []byte(familyProfile)}})
	if err != nil {
		t.Fatal(err)
	}
	if name, overlay, ok := store.ForProduct("family-a"); !ok || name != "family-a" || overlay != nil {
		t.Fatalf("own name: %q %v %v", name, overlay, ok)
	}
	name, overlay, ok := store.ForProduct("family-b")
	if !ok || name != "family-a" {
		t.Fatalf("listed product: %q %v", name, ok)
	}
	if got := overlay["configuration"].(map[string]any)["device"].(map[string]any)["model"]; got != "B" {
		t.Fatalf("overlay model = %v", got)
	}
	if _, _, ok := store.ForProduct("family-c"); ok {
		t.Fatal("an unlisted product resolved")
	}
}

// A product resolves to exactly one profile: listing it twice, or listing a
// name another profile already is, fails to load.
func TestStoreRejectsAnAmbiguousProduct(t *testing.T) {
	other := "name: other\nmodel: O\ninterfaces: []\nproducts:\n  - name: %s\n"
	for _, dup := range []string{"family-b", "family-a"} {
		_, err := LoadStoreFS(fstest.MapFS{
			"devices/family-a.yaml": {Data: []byte(familyProfile)},
			"devices/other.yaml":    {Data: []byte(strings.Replace(other, "%s", dup, 1))},
		})
		if err == nil || !strings.Contains(err.Error(), dup) {
			t.Errorf("product %q claimed twice: err %v", dup, err)
		}
	}
}
