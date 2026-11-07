package seed

import "gopkg.in/yaml.v3"

// The shipped catalog read without a database: the same embedded YAMLs Run
// upserts, exposed so a pure caller (the scale lab's scenario manifests,
// internal/lab/scenario) can check a name against what every fleet is seeded
// with. A parse failure is a build-time bug in an embedded asset, so these
// panic rather than return an error, like SeededRoles.

// ShippedProduct is one official product: its name and the component type it
// is classified under.
type ShippedProduct struct {
	Name          string
	ComponentType string
}

// ShippedProducts returns the official products, in products.yaml order.
func ShippedProducts() []ShippedProduct {
	var doc productsDoc
	if err := yaml.Unmarshal(productsYAML, &doc); err != nil {
		panic("seed: parse products.yaml: " + err.Error())
	}
	out := make([]ShippedProduct, 0, len(doc.Products))
	for _, p := range doc.Products {
		out = append(out, ShippedProduct{Name: p.ID, ComponentType: p.ComponentType})
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
