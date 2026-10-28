package template_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/template"
)

func TestShippedCatalogLoads(t *testing.T) {
	cat := template.Shipped()
	for _, k := range template.Kinds {
		if len(cat.List(k)) == 0 {
			t.Errorf("the shipped catalog holds no %s", k)
		}
	}
	if got, want := len(cat.List("")), 0; got == want {
		t.Fatal("the shipped catalog is empty")
	}
	// Every variant names a parent the catalog ships, and lists after the
	// kinds ahead of it: the list is ordered by kind then name.
	prev := -1
	for _, tm := range cat.List("") {
		k := indexOf(template.Kinds, tm.Kind)
		if k < prev {
			t.Errorf("%s (%s) lists after a later kind", tm.Name, tm.Kind)
		}
		prev = k
	}
}

func indexOf(ks []template.Kind, k template.Kind) int {
	for i, x := range ks {
		if x == k {
			return i
		}
	}
	return -1
}

func TestGetKindRefusesAnotherKind(t *testing.T) {
	cat := template.Shipped()
	std := cat.List(template.KindStandard)[0]
	if _, err := cat.GetKind(std.Name, template.KindStandard); err != nil {
		t.Fatalf("GetKind(%s, standard): %v", std.Name, err)
	}
	if _, err := cat.GetKind(std.Name, template.KindSystem); !errors.Is(err, template.ErrNotFound) {
		t.Fatalf("GetKind(%s, system) = %v, want ErrNotFound", std.Name, err)
	}
	if _, err := cat.Get("no-such-template"); !errors.Is(err, template.ErrNotFound) {
		t.Fatalf("Get(absent) = %v, want ErrNotFound", err)
	}
}

const stdOK = `
  - kind: standard_template
    name: base
    label: Base
    standard:
      choices:
        - name: conf
          label: Conf
          alternates:
            - { name: bar, label: Bar }
            - { name: kit, label: Kit }
      roles:
        - { name: display, label: Display, accepted_types: [display] }
        - { name: bar, label: Bar, choice: conf, alternate: bar }
        - { name: codec, label: Codec, choice: conf, alternate: kit }
        - { name: camera, label: Camera, choice: conf, alternate: kit }
      metrics:
        - { name: room-temperature, default: 21.5 }
`

func load(t *testing.T, body string) error {
	t.Helper()
	_, err := template.Load(map[string][]byte{"t.yaml": []byte("templates:\n" + body)})
	return err
}

func TestLoadAcceptsAWellFormedCatalog(t *testing.T) {
	cat, err := template.Load(map[string][]byte{"t.yaml": []byte("templates:\n" + stdOK + `
  - kind: system_template
    name: base-system
    label: Base, staffed
    system:
      standard: base
      roles:
        display: { component: panel }
        bar: { product: some-bar }
  - kind: component_template
    name: panel
    label: Panel
    component:
      product: some-panel
      attach: { inputs: { port: "161" } }
      properties: { video-input: hdmi1, power: { on: true } }
  - kind: place_template
    name: floor
    label: Floor
    place:
      type: floor
      systems: [{ template: base-system }]
      places:
        - { type: room, name: a, systems: [{ template: base-system }] }
        - { type: room, name: b }
`)})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	std, _ := cat.GetKind("base", template.KindStandard)
	if got := string(std.Standard.Metrics[0].Default); got != "21.5" {
		t.Errorf("metric default = %s, want 21.5", got)
	}
	comp, _ := cat.GetKind("panel", template.KindComponent)
	if got := string(comp.Component.Properties["video-input"]); got != `"hdmi1"` {
		t.Errorf("declared value = %s, want the JSON string", got)
	}
	if got := comp.Component.Properties.Names(); strings.Join(got, ",") != "power,video-input" {
		t.Errorf("Names() = %v, want sorted", got)
	}
}

func TestLoadRefusesASecondDocument(t *testing.T) {
	_, err := template.Load(map[string][]byte{"t.yaml": []byte("templates:\n" + stdOK + "---\ntemplates: []\n")})
	if err == nil || !strings.Contains(err.Error(), "more than one YAML document") {
		t.Fatalf("Load = %v, want the second document refused", err)
	}
}

func TestLoadRefuses(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"unknown kind", `
  - { kind: room_template, name: x, label: X }`, `unknown kind "room_template"`},
		{"no body", `
  - { kind: standard_template, name: x, label: X }`, "needs its body"},
		{"another kind's body", `
  - kind: standard_template
    name: x
    label: X
    standard: { roles: [{ name: a, label: A }] }
    component: { product: p }`, "carries a component_template body"},
		{"no label", `
  - kind: component_template
    name: x
    component: { product: p }`, "no label"},
		{"illegal name", `
  - kind: component_template
    name: Big X
    label: X
    component: { product: p }`, "not a legal name"},
		{"unknown field", `
  - kind: component_template
    name: x
    label: X
    component: { product: p, colour: red }`, "colour"},
		{"duplicate name", stdOK + stdOK, `"base" is named twice`},
		{"role half a choice", `
  - kind: standard_template
    name: x
    label: X
    standard: { roles: [{ name: a, label: A, choice: c }] }`, "want both or neither"},
		{"role names an unknown alternate", `
  - kind: standard_template
    name: x
    label: X
    standard: { roles: [{ name: a, label: A, choice: c, alternate: d }] }`, "unknown choice/alternate c/d"},
		{"capacity below quorum", `
  - kind: standard_template
    name: x
    label: X
    standard: { roles: [{ name: a, label: A, quorum: 3, capacity: 2 }] }`, "capacity below its quorum"},
		{"map places an undeclared role", `
  - kind: standard_template
    name: x
    label: X
    standard:
      roles: [{ name: a, label: A }]
      map: { aspect: 1, positions: [{ role: b, position: 1, x: 0.5, y: 0.5 }] }`, `places role "b"`},
		{"unknown parent", `
  - kind: standard_template
    name: x
    label: X
    standard: { parent: nobody, roles: [{ name: a, label: A }] }`, `"nobody"`},
		{"preset sets the address", `
  - kind: component_template
    name: x
    label: X
    component: { product: p, attach: { inputs: { host: 10.0.0.1 } } }`, `sets "host"`},
		{"fill names both", stdOK + `
  - kind: system_template
    name: s
    label: S
    system: { standard: base, roles: { display: { component: c, product: p } } }`, "names both"},
		{"fill names an unknown component template", stdOK + `
  - kind: system_template
    name: s
    label: S
    system: { standard: base, roles: { display: { component: nobody }, bar: { product: p } } }`, `"nobody"`},
		{"leaves an unconditional role unfilled", stdOK + `
  - kind: system_template
    name: s
    label: S
    system: { standard: base, roles: { bar: { product: p } } }`, `leaves role "display" unfilled`},
		{"fills an undeclared role", stdOK + `
  - kind: system_template
    name: s
    label: S
    system: { standard: base, roles: { display: { product: p }, bar: { product: p }, mixer: { product: p } } }`, `role "mixer"`},
		{"answers a choice twice", stdOK + `
  - kind: system_template
    name: s
    label: S
    system: { standard: base, roles: { display: { product: p }, bar: { product: p }, codec: { product: p }, camera: { product: p } } }`, "with both"},
		{"answers a choice in part", stdOK + `
  - kind: system_template
    name: s
    label: S
    system: { standard: base, roles: { display: { product: p }, codec: { product: p } } }`, `leaves its role "camera" unfilled`},
		{"answers no alternate", stdOK + `
  - kind: system_template
    name: s
    label: S
    system: { standard: base, roles: { display: { product: p } } }`, "with no alternate"},
		{"place names a standard as a system", stdOK + `
  - kind: place_template
    name: p
    label: P
    place: { type: floor, systems: [{ template: base }] }`, "is a standard_template, not a system_template"},
		{"map places one position twice", `
  - kind: standard_template
    name: x
    label: X
    standard:
      roles: [{ name: a, label: A }]
      map: { aspect: 1, positions: [{ role: a, position: 1, x: 0.5, y: 0.5 }, { role: a, position: 1, x: 0.2, y: 0.2 }] }`, "duplicate position a/1"},
		{"system builds to a standard the catalog lacks", `
  - kind: system_template
    name: s
    label: S
    system: { standard: mr-55, roles: { whatever: { product: p } } }`, `builds to template: not in the catalog: "mr-55"`},
		{"place holds two places of one name", `
  - kind: place_template
    name: p
    label: P
    place: { type: floor, places: [{ type: room, name: a }, { type: room, name: a }] }`, `two places named "a"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := load(t, tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}
