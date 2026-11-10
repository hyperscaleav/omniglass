package seed

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestPlatformViewKeysAreSeeded proves the Platform health node table's key
// set (#1644) names only metric types the boot seed ships, each once: the
// page renders a column per key and reads its tooltip off the catalog, so a
// key the seed does not carry would be a column with no description and no
// figure ever.
func TestPlatformViewKeysAreSeeded(t *testing.T) {
	var doc metricTypesDoc
	if err := yaml.Unmarshal(metricTypesYAML, &doc); err != nil {
		t.Fatal(err)
	}
	seeded := map[string]bool{}
	for _, m := range doc.MetricTypes {
		seeded[m.Name] = true
	}
	if len(storage.PlatformViewGroups) == 0 {
		t.Fatal("the platform view declares no groups")
	}
	seen := map[string]string{}
	for _, g := range storage.PlatformViewGroups {
		if g.Name == "" || g.Label == "" || len(g.Keys) == 0 {
			t.Errorf("group %+v needs a name, a label and at least one key", g)
		}
		for _, key := range g.Keys {
			if !seeded[key] {
				t.Errorf("group %s names %s, which metric_types.yaml does not seed", g.Name, key)
			}
			if prev, dup := seen[key]; dup {
				t.Errorf("%s appears in group %s and again in %s", key, prev, g.Name)
			}
			seen[key] = g.Name
		}
	}
}

// TestPlatformWindowKeysAreTheNonzeroCounts proves the platform view ages out
// exactly the keys a writer reports only when nonzero (#1644): those are the
// window counts whose last sample stops being current once a later window
// counted nothing, and every other key is a gauge written whether or not it
// moved. A key the catalog describes one way and the view treats the other
// would either show a long-gone count as current or blink a gauge out.
func TestPlatformWindowKeysAreTheNonzeroCounts(t *testing.T) {
	var doc metricTypesDoc
	if err := yaml.Unmarshal(metricTypesYAML, &doc); err != nil {
		t.Fatal(err)
	}
	nonzero := map[string]bool{}
	for _, m := range doc.MetricTypes {
		nonzero[m.Name] = strings.Contains(m.Description, "reported only when nonzero")
	}
	for _, g := range storage.PlatformViewGroups {
		for _, key := range g.Keys {
			if got, want := storage.PlatformWindowKeys[key], nonzero[key]; got != want {
				t.Errorf("%s: window key = %v, catalog says reported only when nonzero = %v", key, got, want)
			}
		}
	}
	for key := range storage.PlatformWindowKeys {
		if !nonzero[key] {
			t.Errorf("%s is a window key the catalog does not describe as reported only when nonzero", key)
		}
	}
}
