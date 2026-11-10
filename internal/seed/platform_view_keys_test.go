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

// TestPlatformServerGroupKeysAreSeeded proves the Platform health server
// panels (#1645) name only metric types the boot seed ships: each panel is a
// group the view returns per server, with a tooltip read off the catalog, so
// a key the seed does not carry would be a row with no description and no
// figure ever. Every group has a name, a label and a key; a key appears in
// one group only; a pair's denominator (Of) is a seeded key of the same
// group; and only a split key is ranked.
func TestPlatformServerGroupKeysAreSeeded(t *testing.T) {
	var doc metricTypesDoc
	if err := yaml.Unmarshal(metricTypesYAML, &doc); err != nil {
		t.Fatal(err)
	}
	seeded := map[string]bool{}
	for _, m := range doc.MetricTypes {
		seeded[m.Name] = true
	}
	if len(storage.PlatformServerGroups) == 0 {
		t.Fatal("the platform view declares no server groups")
	}
	seen := map[string]string{}
	for _, g := range storage.PlatformServerGroups {
		if g.Name == "" || g.Label == "" || len(g.Keys) == 0 {
			t.Errorf("server group %+v needs a name, a label and at least one key", g)
		}
		inGroup := map[string]bool{}
		for _, k := range g.Keys {
			inGroup[k.Name] = true
		}
		for _, k := range g.Keys {
			if !seeded[k.Name] {
				t.Errorf("server group %s names %s, which metric_types.yaml does not seed", g.Name, k.Name)
			}
			if prev, dup := seen[k.Name]; dup {
				t.Errorf("%s appears in server group %s and again in %s", k.Name, prev, g.Name)
			}
			seen[k.Name] = g.Name
			if k.Of != "" && !inGroup[k.Of] {
				t.Errorf("%s in server group %s is read against %s, which the group does not carry", k.Name, g.Name, k.Of)
			}
			if k.Rank < 0 {
				t.Errorf("%s in server group %s has a negative rank %d", k.Name, g.Name, k.Rank)
			}
		}
	}
	for _, g := range storage.PlatformViewGroups {
		for _, key := range g.Keys {
			if prev, dup := seen[key]; dup {
				t.Errorf("%s is a node column and also in server group %s", key, prev)
			}
		}
	}
}
