package simoutpost

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The outpost resolves a Go family's product to the family's name (#2829), and
// refuses it as a pusher: a display has no push face.
func TestOutpostResolvesAFamilyProduct(t *testing.T) {
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	idFor := profileIDFor(cat)
	for _, product := range spectest.Products {
		if id, ok := idFor(product); !ok || id != spectest.Name {
			t.Errorf("%s resolved to %q, %v", product, id, ok)
		}
	}
	if _, ok := idFor("nobody"); ok {
		t.Error("an unknown product resolved")
	}
	_, err = startPushers(cat, "http://127.0.0.1:1", []Pusher{{Name: "x", Profile: spectest.Name, Token: "t"}}, nil, sim.Clock{})
	if err == nil || !strings.Contains(err.Error(), "no push face") {
		t.Errorf("%s as a pusher: err = %v, want no push face", spectest.Name, err)
	}
}
