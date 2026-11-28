package sim

import (
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	simdevice "github.com/hyperscaleav/omniglass/sim/device"
	"github.com/hyperscaleav/omniglass/sim/profiles"
)

// The simulator serves a component by its product's name, so every device
// definition (a YAML profile or a Go family) must name products the boot seed
// ships, or no seeded component could ever be simulated by it. Cloud services are simulated fleet-wide
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
	cat, err := simdevice.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, def := range cat.Definitions() {
		if def.Identity().DeviceType == "cloud-service" {
			continue
		}
		checked++
		// A YAML profile is named for the product it leads with; a Go family is
		// named for its wire API and lists only its models, the first leading.
		products := def.Products()
		lead := products[0]
		for _, name := range products {
			if !seeded[name] {
				t.Errorf("%s %q lists %q, which the seed does not ship; name the product it simulates", def.Kind(), def.ID(), name)
			}
			// A shared definition is one device in variants (a display line in
			// sizes), never a different device: a touch panel's surface is a
			// subset of a control processor's, so one cannot stand in for
			// the other.
			if driverOf[name] != driverOf[lead] {
				t.Errorf("%s %q (driver %s) lists %q (driver %s); a variant is collected the same way", def.Kind(), def.ID(), driverOf[lead], name, driverOf[name])
			}
			if typeOf[name] != typeOf[lead] {
				t.Errorf("%s %q (component type %s) lists %q (component type %s); a definition serves variants of one kind of device", def.Kind(), def.ID(), typeOf[lead], name, typeOf[name])
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
		_, simulated := cat.ForProduct(p.ID)
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
		if _, simulated := cat.ForProduct(id); simulated {
			t.Errorf("%s is simulated now; drop it from coverage.yaml", id)
		}
	}
	// A passive product has no network interface: no driver, and no profile.
	drivers := map[string]string{}
	for _, p := range facts.Products {
		drivers[p.ID] = p.DriverID
	}
	for id := range cov.Passive {
		_, simulated := cat.ForProduct(id)
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
