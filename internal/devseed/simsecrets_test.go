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
