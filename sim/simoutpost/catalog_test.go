package simoutpost

import (
	"strings"

	"testing"

	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"

	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/simtest/shipped"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// catalogOf is a profile store as the catalog the outpost reads, beside the
// shipped Go families.
func catalogOf(t testing.TB, store *profile.Store) *spec.Catalog {
	t.Helper()
	return shipped.CatalogFrom(t, store)
}

// The outpost resolves a Go family's product to a profile name exactly as it
// resolves the YAML twin's (#2829), and refuses either as a pusher alike: a
// display has no push face.
func TestOutpostResolvesAFamilyProductAsItResolvesItsYAMLTwin(t *testing.T) {
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	idFor := profileIDFor(cat)
	for _, p := range spectest.Pairs {
		if id, ok := idFor(p.Go); !ok || id != spectest.Name {
			t.Errorf("%s resolved to %q, %v", p.Go, id, ok)
		}
		if id, ok := idFor(p.YAML); !ok || id != spectest.TwinName {
			t.Errorf("%s resolved to %q, %v", p.YAML, id, ok)
		}
	}
	if _, ok := idFor("nobody"); ok {
		t.Error("an unknown product resolved")
	}
	for _, name := range []string{spectest.Name, spectest.TwinName} {
		_, err := startPushers(cat, "http://127.0.0.1:1", []Pusher{{Name: "x", Profile: name, Token: "t"}}, nil, sim.Clock{})
		if err == nil || !strings.Contains(err.Error(), "no push face") {
			t.Errorf("%s as a pusher: err = %v, want no push face", name, err)
		}
	}
}
