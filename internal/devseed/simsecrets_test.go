package devseed

import (
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/simfleet"
)

// TestSimSecretsHoldWhatTheFacesDemand: each secret a simulated attach
// references holds the credential the face of every profile it is attached
// to demands (the community, the login, the key). The two are written in two
// places; edited in one alone, every simulated component on that driver
// would sit beneath an auth gate while each test that brings its own literal
// stayed green.
func TestSimSecretsHoldWhatTheFacesDemand(t *testing.T) {
	t.Parallel()
	raw, err := seed.FactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var facts struct {
		Products []struct {
			ID       string `json:"id"`
			DriverID string `json:"driver_id"`
		} `json:"products"`
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]map[string]string{}
	types := map[string]string{}
	for _, s := range simSecrets {
		byName[s.Name], types[s.Name] = s.Fields, s.SecretType
	}
	checked := 0
	for driver, secret := range simfleet.AttachSecret {
		fields, ok := byName[secret]
		if !ok {
			t.Errorf("%s attaches with %s, which the dev seed does not create", driver, secret)
			continue
		}
		for _, p := range facts.Products {
			if p.DriverID != driver {
				continue
			}
			name, _, served := store.ForProduct(p.ID)
			if !served {
				continue
			}
			checked++
			if !faceDemands(store.Devices[name], types[secret], fields) {
				t.Errorf("%s (driver %s, profile %s): no face demands what %s holds (%v)", p.ID, driver, name, secret, fields)
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no simulated attach")
	}
}

// faceDemands reports whether one of the profile's faces asks for exactly
// the credential the secret holds.
func faceDemands(p *profile.DeviceProfile, secretType string, fields map[string]string) bool {
	for _, iface := range p.Interfaces {
		a := iface.Auth
		switch secretType {
		case "snmp-community":
			if iface.Protocol == "snmp" && a.Community == fields["community"] {
				return true
			}
		case "basic-auth":
			if a.DefaultUser == fields["username"] && a.DefaultPassword == fields["password"] {
				return true
			}
		case "api-key":
			if a.APIKey != "" && a.APIKey == fields["key"] {
				return true
			}
		}
	}
	return false
}

// TestPlantedSecretsAreNotReferencedBeneathTheirOwner: a node refuses a secret
// name with more than one copy cascading onto a component (#820, it does not
// rank them as the effective view does), so a name planted below the platform
// must be referenced by no endpoint on a component the extra copy covers: the
// location's whole subtree, or the one component. Every owner a fixture secret
// names must also resolve, and sit over a platform copy to shadow.
func TestPlantedSecretsAreNotReferencedBeneathTheirOwner(t *testing.T) {
	t.Parallel()
	doc, err := Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	locs, comps := map[string]string{}, map[string]string{}
	parent, placed := map[string]string{}, map[string]string{}
	for _, l := range doc.Locations {
		locs[l.Key], parent[l.Key] = l.Key, l.Parent
	}
	for _, c := range append(append([]Component(nil), doc.Components...), doc.SimComponents...) {
		comps[c.Key], placed[c.Key] = c.Key, c.Location
	}
	// covers: the component sits in the location (or is the component) the copy is owned by.
	covers := func(s SimSecret, comp string) bool {
		if s.OwnerKind == "component" {
			return comp == s.Owner
		}
		for l := placed[comp]; l != ""; l = parent[l] {
			if l == s.Owner {
				return true
			}
		}
		return false
	}
	platform := map[string]bool{}
	for _, s := range doc.SimSecrets {
		if s.OwnerKind == "" {
			platform[s.Name] = true
		}
	}
	attached := map[string]bool{}
	for _, name := range simfleet.AttachSecret {
		attached[name] = true
	}
	planted := 0
	for _, s := range doc.SimSecrets {
		if _, err := s.secretSpec(locs, comps); err != nil {
			t.Error(err)
			continue
		}
		if s.OwnerKind == "" {
			continue
		}
		planted++
		if !platform[s.Name] {
			t.Errorf("%s is planted at %s %s with no platform copy beneath it", s.Name, s.OwnerKind, s.Owner)
		}
		if attached[s.Name] {
			t.Errorf("%s is planted at %s %s, and the plan's attaches reference it on every component", s.Name, s.OwnerKind, s.Owner)
		}
		for _, e := range doc.SimEndpoints {
			for _, ref := range e.Inputs {
				if ref == s.Name && covers(s, e.Component) {
					t.Errorf("%s is planted at %s %s, and the endpoint %q on %s references it: the node would refuse the ambiguous name",
						s.Name, s.OwnerKind, s.Owner, e.Label, e.Component)
				}
			}
		}
	}
	if planted == 0 {
		t.Fatal("the fixture plants no secret below the platform")
	}
}

// TestSecretSpecRejectsABadOwner: an owner the fixture does not declare, a
// platform secret naming one, and an unknown kind each fail the seed loudly.
func TestSecretSpecRejectsABadOwner(t *testing.T) {
	t.Parallel()
	ids := map[string]string{"room": "room-id"}
	for name, s := range map[string]SimSecret{
		"unknown key":      {Name: "x", OwnerKind: "location", Owner: "nope"},
		"platform + owner": {Name: "x", Owner: "room"},
		"unknown kind":     {Name: "x", OwnerKind: "system", Owner: "room"},
	} {
		if _, err := s.secretSpec(ids, ids); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	spec, err := SimSecret{Name: "x", OwnerKind: "component", Owner: "room"}.secretSpec(nil, ids)
	if err != nil || spec.OwnerKind != "component" || spec.OwnerName == nil || *spec.OwnerName != "room-id" {
		t.Errorf("component owner = %+v, %v", spec, err)
	}
}
