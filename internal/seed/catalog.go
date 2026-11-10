package seed

import (
	"time"

	"gopkg.in/yaml.v3"
)

// The shipped catalog read without a database: the same embedded YAMLs Run
// upserts, exposed so a pure caller (the scale lab's scenario manifests,
// internal/lab/scenario) can check a name against what every fleet is seeded
// with. A parse failure is a build-time bug in an embedded asset, so these
// panic rather than return an error, like SeededRoles.

// ShippedProduct is one official product: its name, the component type it
// is classified under, and the driver that collects from it ("" for none).
type ShippedProduct struct {
	Name          string
	ComponentType string
	Driver        string
}

// ShippedProducts returns the official products, in products.yaml order.
func ShippedProducts() []ShippedProduct {
	var doc productsDoc
	if err := yaml.Unmarshal(productsYAML, &doc); err != nil {
		panic("seed: parse products.yaml: " + err.Error())
	}
	out := make([]ShippedProduct, 0, len(doc.Products))
	for _, p := range doc.Products {
		out = append(out, ShippedProduct{Name: p.ID, ComponentType: p.ComponentType, Driver: p.DriverID})
	}
	return out
}

// ShippedComponentTypeParents returns each official component type's parent,
// "" for a root: the tree a type's "within" walks.
func ShippedComponentTypeParents() map[string]string {
	var doc componentTypesDoc
	if err := yaml.Unmarshal(componentTypesYAML, &doc); err != nil {
		panic("seed: parse component_types.yaml: " + err.Error())
	}
	out := make(map[string]string, len(doc.ComponentTypes))
	for _, ct := range doc.ComponentTypes {
		out[ct.ID] = ct.ParentID
	}
	return out
}

// ShippedSystemTypes returns the official system type names, in
// system_types.yaml order.
func ShippedSystemTypes() []string {
	var doc systemTypesDoc
	if err := yaml.Unmarshal(systemTypesYAML, &doc); err != nil {
		panic("seed: parse system_types.yaml: " + err.Error())
	}
	out := make([]string, 0, len(doc.SystemTypes))
	for _, st := range doc.SystemTypes {
		out = append(out, st.ID)
	}
	return out
}

// ShippedDriverPolls returns each official driver's poll intervals, in the
// order its spec lists its polls: every `schedule: { every: D }`. A driver
// with no polls (a stub, or one that only listens) maps to none. The perf
// gate's window is held to a whole multiple of each (#1640).
func ShippedDriverPolls() map[string][]time.Duration {
	var doc struct {
		Drivers []struct {
			ID   string `yaml:"id"`
			Spec struct {
				Polls []struct {
					Schedule struct {
						Every string `yaml:"every"`
					} `yaml:"schedule"`
				} `yaml:"polls"`
			} `yaml:"spec"`
		} `yaml:"drivers"`
	}
	if err := yaml.Unmarshal(driversYAML, &doc); err != nil {
		panic("seed: parse drivers.yaml: " + err.Error())
	}
	out := make(map[string][]time.Duration, len(doc.Drivers))
	for _, d := range doc.Drivers {
		every := []time.Duration{}
		for _, p := range d.Spec.Polls {
			if p.Schedule.Every == "" {
				continue
			}
			v, err := time.ParseDuration(p.Schedule.Every)
			if err != nil {
				panic("seed: drivers.yaml: driver " + d.ID + ": schedule every " + p.Schedule.Every + ": " + err.Error())
			}
			every = append(every, v)
		}
		out[d.ID] = every
	}
	return out
}
