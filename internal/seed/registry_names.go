package seed

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

// sharedRegistryNames parses the three ship-with registries (metric_type,
// property_type, event_type) and returns, sorted, every name more than one of
// them holds (#457). Ingest resolves a shared name to nothing on every lane,
// so the seed refuses to ship one (Run).
func sharedRegistryNames(metricYAML, propertyYAML, eventYAML []byte) ([]string, error) {
	type named struct {
		Name string `yaml:"name"`
	}
	var metrics struct {
		Types []named `yaml:"metric_types"`
	}
	var properties struct {
		Types []named `yaml:"property_types"`
	}
	var events struct {
		Types []named `yaml:"event_types"`
	}
	for _, d := range []struct {
		raw  []byte
		into any
		what string
	}{{metricYAML, &metrics, "metric_types"}, {propertyYAML, &properties, "property_types"}, {eventYAML, &events, "event_types"}} {
		if err := yaml.Unmarshal(d.raw, d.into); err != nil {
			return nil, fmt.Errorf("seed: parse %s: %w", d.what, err)
		}
	}
	holders := map[string]int{}
	for _, reg := range [][]named{metrics.Types, properties.Types, events.Types} {
		seen := map[string]bool{}
		for _, n := range reg {
			if !seen[n.Name] {
				seen[n.Name] = true
				holders[n.Name]++
			}
		}
	}
	var shared []string
	for name, n := range holders {
		if n > 1 {
			shared = append(shared, name)
		}
	}
	slices.Sort(shared)
	return shared, nil
}
