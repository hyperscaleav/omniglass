package template_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/template"
)

const locationFixture = `templates:
  - kind: location_template
    name: annex-wing
    label: An annex wing
    location:
      type: building
      locations:
        - { type: annex, name: a1 }
  - kind: location_template
    name: upside-down
    label: Upside down
    location:
      type: room
      locations:
        - { type: floor, name: f1 }
  - kind: location_template
    name: plain-floor
    label: A floor
    location:
      type: floor
`

// TestPlanLocationsRefusesWhatTheFleetCannotHold: a subtree naming a type the
// fleet lacks is refused naming the type, a nesting its types refuse is
// refused naming the pair, and a root with no name and no name rule is
// refused naming the type. Nothing is keyed on any particular type: the
// fixture's types are only what the seed happens to ship.
func TestPlanLocationsRefusesWhatTheFleetCannotHold(t *testing.T) {
	gw := seeded(t)
	ctx := context.Background()
	cat, err := template.Load(map[string][]byte{"p.yaml": []byte(locationFixture)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, tc := range []struct{ tmpl, name, want string }{
		{"annex-wing", "w", "names location type annex, which this fleet does not hold"},
		{"upside-down", "r", "places a floor under a room"},
		{"plain-floor", "", "a floor needs a name"},
	} {
		_, err := template.PlanLocations(ctx, gw, cat, template.LocationRequest{Template: tc.tmpl, Name: tc.name})
		if !errors.Is(err, template.ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want an ErrInvalid containing %q", tc.tmpl, err, tc.want)
		}
	}
	if _, err := template.PlanLocations(ctx, gw, cat, template.LocationRequest{Template: "plain-floor", Name: "f1"}); err != nil {
		t.Errorf("a named floor: %v", err)
	}
}

// TestPlanSystemFillsToQuorumAndCreatesTheChain: a system template on a fresh
// fleet plans the fork of its standard with every ancestor, parents first,
// and a component per role to quorum; a role filled with a product its type
// refuses is refused naming the role.
func TestPlanSystemFillsToQuorumAndCreatesTheChain(t *testing.T) {
	gw := seeded(t)
	ctx := context.Background()
	sp, err := template.PlanSystem(ctx, gw, template.Shipped(), template.SystemRequest{Template: "mr55-system"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var forks []string
	for _, f := range sp.Plan.Standards {
		forks = append(forks, f.Name)
	}
	if strings.Join(forks, ",") != "meeting-room,mr55" || !sp.CreatesStandards || sp.Standard != "mr55" {
		t.Fatalf("forks = %v, want meeting-room then mr55", forks)
	}
	if len(sp.Pieces) != 4 {
		t.Fatalf("pieces = %+v, want one per role", sp.Pieces)
	}

	bad, err := template.Load(map[string][]byte{"s.yaml": []byte(`templates:
  - kind: standard_template
    name: one-screen
    label: One screen
    standard: { roles: [{ name: screen, label: Screen, quorum: 2, accepted_types: [display] }] }
  - kind: system_template
    name: wrong-screen
    label: Wrong
    system: { standard: one-screen, roles: { screen: { product: boreal-cast-m1 } } }
`)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := template.PlanSystem(ctx, gw, bad, template.SystemRequest{Template: "wrong-screen"}); !errors.Is(err, template.ErrInvalid) || !strings.Contains(err.Error(), "screen (") {
		t.Fatalf("a fill of the wrong type: %v, want an ErrInvalid naming the role", err)
	}
}
