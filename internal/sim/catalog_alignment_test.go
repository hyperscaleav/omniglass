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
			ID            string `json:"id"`
			DriverID      string `json:"driver_id"`
			ComponentType string `json:"component_type"`
		} `json:"products"`
	}
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	seeded := map[string]bool{}
	typeOf, driverOf := map[string]string{}, map[string]string{}
	for _, p := range facts.Products {
		seeded[p.ID] = true
		typeOf[p.ID] = p.ComponentType
		driverOf[p.ID] = p.DriverID
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
			// A shared profile is one device in variants (a display line in
			// sizes), never a different device: a touch panel's surface is a
			// subset of a control processor's, so one cannot stand in for
			// the other.
			if driverOf[v.Name] != driverOf[name] {
				t.Errorf("device profile %q (driver %s) lists %q (driver %s); a variant is collected the same way", name, driverOf[name], v.Name, driverOf[v.Name])
			}
			if typeOf[v.Name] != typeOf[name] {
				t.Errorf("device profile %q (component type %s) lists %q (component type %s); a profile serves variants of one kind of device", name, typeOf[name], v.Name, typeOf[v.Name])
			}
		}
	}
	if checked == 0 {
		t.Fatal("checked no device profiles")
	}

	// Coverage: every seeded product with a driver is simulated, or listed as
	// not yet with its issue.
	cov, err := profiles.LoadCoverage()
	if err != nil {
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
		// Whatever its driver: a service on the network can have none.
		if _, _, simulated := store.ForProduct(id); simulated {
			t.Errorf("%s is simulated now; drop it from coverage.yaml", id)
		}
	}
	// A passive product has no network interface: no driver, and no profile.
	drivers := map[string]string{}
	for _, p := range facts.Products {
		drivers[p.ID] = p.DriverID
	}
	for id := range cov.Passive {
		_, _, simulated := store.ForProduct(id)
		switch {
		case !seeded[id]:
			t.Errorf("coverage.yaml lists %q as passive, which the seed does not ship", id)
		case drivers[id] != "":
			t.Errorf("coverage.yaml lists %q as passive, but it has a driver (%s): it is on the network", id, drivers[id])
		case simulated:
			t.Errorf("coverage.yaml lists %q as passive, but a simulator profile serves it", id)
		}
	}
}
