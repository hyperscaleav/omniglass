package seed

import (
	"encoding/json"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"gopkg.in/yaml.v3"
)

// ShippedDriverSpec is a shipped driver's spec, parsed as the write path
// parses it, read from the embedded catalog the boot seed upserts. A caller
// that plans against a driver before any database (a scale lab plan, #1747)
// reads the same spec the seed stores.
func ShippedDriverSpec(id string) (*driver.Spec, error) {
	var doc driversDoc
	if err := yaml.Unmarshal(driversYAML, &doc); err != nil {
		return nil, fmt.Errorf("seed: parse drivers: %w", err)
	}
	for _, d := range doc.Drivers {
		if d.ID != id {
			continue
		}
		if len(d.Spec) == 0 {
			return nil, fmt.Errorf("seed: driver %s ships no spec", id)
		}
		raw, err := json.Marshal(d.Spec)
		if err != nil {
			return nil, fmt.Errorf("seed: marshal driver %s spec: %w", id, err)
		}
		sp, err := driver.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("seed: parse driver %s spec: %w", id, err)
		}
		return sp, nil
	}
	return nil, fmt.Errorf("seed: no shipped driver %s", id)
}
