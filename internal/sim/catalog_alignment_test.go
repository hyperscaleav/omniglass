package sim

import (
	"encoding/json"
	"io/fs"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// The simulator serves a component by its product's name, so every device
// profile must name a product the boot seed ships, or no seeded component
// could ever be simulated by it. Cloud services are simulated fleet-wide
// rather than as a product, and are exempt.
func TestEveryDeviceProfileNamesASeededProduct(t *testing.T) {
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
	seeded := map[string]bool{}
	for _, p := range facts.Products {
		seeded[p.ID] = true
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for name, p := range store.Devices {
		if p.DeviceType == "cloud-service" {
			continue
		}
		checked++
		if !seeded[name] {
			t.Errorf("device profile %q names no seeded product; rename it to the product it simulates", name)
		}
		for _, v := range p.Products {
			if !seeded[v.Name] {
				t.Errorf("device profile %q lists %q, which the seed does not ship", name, v.Name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no device profiles")
	}

	// Coverage: every seeded product on the network (it has a driver) is
	// simulated, or listed as not yet with its issue.
	var cov struct {
		Unsimulated map[string]string `yaml:"unsimulated"`
	}
	b, err := fs.ReadFile(profiles.FS, "coverage.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(b, &cov); err != nil {
		t.Fatal(err)
	}
	for _, p := range facts.Products {
		if p.DriverID == "" {
			continue
		}
		_, _, simulated := store.ForProduct(p.ID)
		_, pending := cov.Unsimulated[p.ID]
		switch {
		case simulated && pending:
			t.Errorf("%s is simulated now; drop it from coverage.yaml", p.ID)
		case !simulated && !pending:
			t.Errorf("%s (driver %s) has no simulator profile; add it to a family profile or list it in coverage.yaml with its issue", p.ID, p.DriverID)
		}
	}
	for id := range cov.Unsimulated {
		if !seeded[id] {
			t.Errorf("coverage.yaml lists %q, which the seed does not ship", id)
		}
	}
}
