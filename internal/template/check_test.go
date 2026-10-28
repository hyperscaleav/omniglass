package template_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/template"
)

func seeded(t *testing.T) storage.Gateway {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	gw := storagetest.NewDB(t)
	if err := seed.Run(context.Background(), gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw
}

// TestShippedTemplatesDryRunClean is the build's guard on the catalog: every
// shipped template names only rows the seeded catalogs hold, so a release
// that withdraws a product, a type, a driver input or a property a template
// still names fails here rather than in an operator's fork.
func TestShippedTemplatesDryRunClean(t *testing.T) {
	gw := seeded(t)
	for _, err := range template.Check(context.Background(), template.Shipped(), gw) {
		t.Error(err)
	}
}

func TestDryRunNamesTheTemplateAndTheWithdrawnRow(t *testing.T) {
	gw := seeded(t)
	cat, err := template.Load(map[string][]byte{"t.yaml": []byte(`templates:
  - kind: component_template
    name: gone-display
    label: Gone
    component: { product: withdrawn-panel }
  - kind: component_template
    name: odd-preset
    label: Odd
    component: { product: boreal-edge-55, attach: { inputs: { baud: "9600" } } }
  - kind: standard_template
    name: odd-standard
    label: Odd
    standard:
      roles: [{ name: a, label: A, accepted_types: [hologram] }]
      metrics: [{ name: no-such-metric }]
  - kind: place_template
    name: odd-places
    label: Odd
    place: { type: floor, places: [{ type: campus, name: c }, { type: annex, name: x }] }
`)})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var got []string
	for _, err := range template.Check(context.Background(), cat, gw) {
		got = append(got, err.Error())
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{
		`component_template gone-display: names product "withdrawn-panel"`,
		`component_template odd-preset: presets input "baud", which driver "snmp-generic" does not declare`,
		`standard_template odd-standard: role a accepts component type "hologram"`,
		`standard_template odd-standard: declares metric "no-such-metric"`,
		`place_template odd-places: places a campus under a floor`,
		`place_template odd-places: names location type "annex"`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("dry run missed %q; got:\n%s", want, all)
		}
	}
}
