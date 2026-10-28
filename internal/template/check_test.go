package template_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

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
  - kind: component_template
    name: odd-values
    label: Odd
    component:
      product: kestrel-vroom
      attach: { inputs: { port: four-four-three, tls-mode: pinned } }
      properties: { power-state: 42 }
  - kind: standard_template
    name: display-only
    label: Display only
    standard: { roles: [{ name: panel, label: Panel, accepted_types: [display] }] }
  - kind: system_template
    name: odd-fill
    label: Odd
    system: { standard: display-only, roles: { panel: { product: boreal-cast-m1 } } }
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
		`component_template odd-values: presets input "port" as "four-four-three", which is not a number`,
		`component_template odd-values: presets input tls-mode`,
		`component_template odd-values: declares 42 for property "power-state"`,
		`system_template odd-fill: role panel fills with product "boreal-cast-m1": a media-player`,
		`place_template odd-places: names location type "annex"`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("dry run missed %q; got:\n%s", want, all)
		}
	}
}

// TestNoTemplateIsARow pins ADR-0071's rule at the schema: a template is
// never a row, so no table is a template table and no row carries a pointer
// back at one. A later slice persisting templates (#1330) has to take its
// own ruling, and fails here until it does. (A label rule's text template,
// label_rule.template, is a rendering rule, not a template of this catalog.)
func TestNoTemplateIsARow(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	rows, err := conn.Query(ctx, `
		select table_name || coalesce('.' || column_name, '') from (
			select table_name, null::text as column_name from information_schema.tables
			 where table_schema = 'public' and table_name like '%template%'
			union all
			select table_name, column_name from information_schema.columns
			 where table_schema = 'public' and column_name in ('template_id', 'template_name', 'from_template')
		) found order by 1`)
	if err != nil {
		t.Fatalf("read the schema: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var hit string
		if err := rows.Scan(&hit); err != nil {
			t.Fatalf("scan: %v", err)
		}
		t.Errorf("the schema holds %s: a template is never a row and nothing points back at one (ADR-0071)", hit)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the schema: %v", err)
	}
}
