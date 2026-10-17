package sim

import (
	"encoding/json"
	"testing"

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
			ID string `json:"id"`
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
	}
	if checked == 0 {
		t.Fatal("checked no device profiles")
	}
}
