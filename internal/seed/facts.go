package seed

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/rbac"
)

// The generated seed-facts artifact: FactsJSON renders every embedded seed YAML
// into docs/src/generated/seed.json, the file the docs render shipped-set
// claims from (which property types the baseline ships, the role matrix, the
// transports). Same drift discipline as the schema facts (a committed artifact
// plus a test that re-renders), but no database: the YAMLs are the source and
// they are embedded in this package. Roles carry both declared and effective
// permissions; effective resolves inheritance through rbac.Flatten, the same
// path the live authorizer uses, so the docs match the console Roles view.

type factsRole struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description,omitempty"`
	Inherits    []string `json:"inherits,omitempty"`
	Declared    []string `json:"declared"`
	Effective   []string `json:"effective"`
}

type factsProperty struct {
	Name        string `json:"name"`
	DataType    string `json:"data_type"`
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
}

type factsMetricType struct {
	Name        string `json:"name"`
	DataType    string `json:"data_type"`
	Unit        string `json:"unit,omitempty"`
	Precision   *int   `json:"precision,omitempty"`
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
}

type factsNamed struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
}

type factsInterfaceType struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Built       bool   `json:"built"`
}

type factsLocationType struct {
	ID                 string   `json:"id"`
	DisplayName        string   `json:"display_name"`
	Icon               string   `json:"icon,omitempty"`
	AllowedParentTypes []string `json:"allowed_parent_types,omitempty"`
}

type factsStandardRole struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name,omitempty"`
	Quorum       int      `json:"quorum"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type factsStandard struct {
	ID               string              `json:"id"`
	DisplayName      string              `json:"display_name"`
	ParentStandardID string              `json:"parent_standard_id,omitempty"`
	Roles            []factsStandardRole `json:"roles,omitempty"`
}

type factsVendor struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Kind        string `json:"kind,omitempty"`
	Website     string `json:"website,omitempty"`
}

type factsDriver struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version,omitempty"`
}

type factsCapability struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type factsProductProperty struct {
	Name     string `json:"name"`
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type factsProduct struct {
	ID              string                 `json:"id"`
	DisplayName     string                 `json:"display_name"`
	VendorID        string                 `json:"vendor_id,omitempty"`
	DriverID        string                 `json:"driver_id,omitempty"`
	Kind            string                 `json:"kind,omitempty"`
	ParentProductID string                 `json:"parent_product_id,omitempty"`
	Capabilities    []string               `json:"capabilities,omitempty"`
	Properties      []factsProductProperty `json:"properties,omitempty"`
}

type factsSecretTypeField struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Secret bool   `json:"secret"`
	Origin string `json:"origin,omitempty"`
}

type factsSecretType struct {
	ID                    string                 `json:"id"`
	DisplayName           string                 `json:"display_name"`
	DefaultAdminSensitive bool                   `json:"default_admin_sensitive"`
	Fields                []factsSecretTypeField `json:"fields"`
}

type seedFactsDoc struct {
	Comment        string               `json:"//"`
	Roles          []factsRole          `json:"roles"`
	PropertyTypes  []factsProperty      `json:"property_types"`
	MetricTypes    []factsMetricType    `json:"metric_types"`
	EventTypes     []factsNamed         `json:"event_types"`
	CommandTypes   []factsNamed         `json:"command_types"`
	SecretTypes    []factsSecretType    `json:"secret_types"`
	InterfaceTypes []factsInterfaceType `json:"interface_types"`
	LocationTypes  []factsLocationType  `json:"location_types"`
	Standards      []factsStandard      `json:"standards"`
	Vendors        []factsVendor        `json:"vendors"`
	Drivers        []factsDriver        `json:"drivers"`
	Capabilities   []factsCapability    `json:"capabilities"`
	Products       []factsProduct       `json:"products"`
}

// eventCommandDoc covers the shared name/display/description shape of the
// event_types and command_types YAMLs for the facts render; the seeders keep
// their own richer structs.
type eventCommandDoc struct {
	EventTypes []struct {
		Name        string `yaml:"name"`
		DisplayName string `yaml:"display_name"`
		Description string `yaml:"description"`
	} `yaml:"event_types"`
	CommandTypes []struct {
		Name        string `yaml:"name"`
		DisplayName string `yaml:"display_name"`
		Description string `yaml:"description"`
	} `yaml:"command_types"`
}

// FactsJSON renders the seed facts document from the embedded YAMLs, no
// database required. Deterministic: collections keep their YAML order, and
// effective permissions come back sorted from rbac's Set. The trailing newline
// makes the committed file POSIX-clean.
func FactsJSON() ([]byte, error) {
	doc := seedFactsDoc{Comment: "generated by make gen (cmd/seedgen); do not edit"}

	var roles rolesDoc
	if err := yaml.Unmarshal(rolesYAML, &roles); err != nil {
		return nil, fmt.Errorf("seed facts: roles: %w", err)
	}
	rbacRoles := make([]rbac.Role, 0, len(roles.Roles))
	for _, r := range roles.Roles {
		rbacRoles = append(rbacRoles, rbac.Role{ID: r.ID, Permissions: r.Permissions, Inherits: r.Inherits})
	}
	idx := rbac.NewRoleIndex(rbacRoles)
	for _, r := range roles.Roles {
		doc.Roles = append(doc.Roles, factsRole{
			ID: r.ID, DisplayName: r.DisplayName, Description: r.Description,
			Inherits: r.Inherits, Declared: r.Permissions,
			Effective: idx.Flatten([]string{r.ID}).Strings(),
		})
	}

	var props propertyTypesDoc
	if err := yaml.Unmarshal(propertyTypesYAML, &props); err != nil {
		return nil, fmt.Errorf("seed facts: property types: %w", err)
	}
	for _, p := range props.PropertyTypes {
		doc.PropertyTypes = append(doc.PropertyTypes, factsProperty{
			Name: p.Name, DataType: p.DataType,
			DisplayName: p.DisplayName, Description: p.Description,
		})
	}

	var mts metricTypesDoc
	if err := yaml.Unmarshal(metricTypesYAML, &mts); err != nil {
		return nil, fmt.Errorf("seed facts: metric types: %w", err)
	}
	for _, m := range mts.MetricTypes {
		doc.MetricTypes = append(doc.MetricTypes, factsMetricType{
			Name: m.Name, DataType: m.DataType, Unit: m.Unit, Precision: m.Precision,
			DisplayName: m.DisplayName, Description: m.Description,
		})
	}

	var evcmd eventCommandDoc
	if err := yaml.Unmarshal(eventTypesYAML, &evcmd); err != nil {
		return nil, fmt.Errorf("seed facts: event types: %w", err)
	}
	for _, e := range evcmd.EventTypes {
		doc.EventTypes = append(doc.EventTypes, factsNamed{Name: e.Name, DisplayName: e.DisplayName, Description: e.Description})
	}
	evcmd = eventCommandDoc{}
	if err := yaml.Unmarshal(commandTypesYAML, &evcmd); err != nil {
		return nil, fmt.Errorf("seed facts: command types: %w", err)
	}
	for _, c := range evcmd.CommandTypes {
		doc.CommandTypes = append(doc.CommandTypes, factsNamed{Name: c.Name, DisplayName: c.DisplayName, Description: c.Description})
	}

	var secrets secretTypesDoc
	if err := yaml.Unmarshal(secretTypesYAML, &secrets); err != nil {
		return nil, fmt.Errorf("seed facts: secret types: %w", err)
	}
	for _, s := range secrets.SecretTypes {
		st := factsSecretType{ID: s.ID, DisplayName: s.DisplayName, DefaultAdminSensitive: s.DefaultAdminSensitive}
		for _, f := range s.Fields {
			st.Fields = append(st.Fields, factsSecretTypeField{Name: f.Name, Type: f.Type, Secret: f.Secret, Origin: f.Origin})
		}
		doc.SecretTypes = append(doc.SecretTypes, st)
	}

	var ifts interfaceTypesDoc
	if err := yaml.Unmarshal(interfaceTypesYAML, &ifts); err != nil {
		return nil, fmt.Errorf("seed facts: interface types: %w", err)
	}
	for _, it := range ifts.InterfaceTypes {
		doc.InterfaceTypes = append(doc.InterfaceTypes, factsInterfaceType{Name: it.Name, Description: it.Description, Built: it.Built})
	}

	var lts locationTypesDoc
	if err := yaml.Unmarshal(locationTypesYAML, &lts); err != nil {
		return nil, fmt.Errorf("seed facts: location types: %w", err)
	}
	for _, lt := range lts.LocationTypes {
		doc.LocationTypes = append(doc.LocationTypes, factsLocationType{
			ID: lt.ID, DisplayName: lt.DisplayName, Icon: lt.Icon, AllowedParentTypes: lt.AllowedParentTypes,
		})
	}

	var stds standardsDoc
	if err := yaml.Unmarshal(standardsYAML, &stds); err != nil {
		return nil, fmt.Errorf("seed facts: standards: %w", err)
	}
	for _, s := range stds.Standards {
		fs := factsStandard{ID: s.ID, DisplayName: s.DisplayName, ParentStandardID: s.ParentStandardID}
		for _, r := range s.Roles {
			fs.Roles = append(fs.Roles, factsStandardRole{Name: r.Name, DisplayName: r.DisplayName, Quorum: r.Quorum, Capabilities: r.Capabilities})
		}
		doc.Standards = append(doc.Standards, fs)
	}

	var vs vendorsDoc
	if err := yaml.Unmarshal(vendorsYAML, &vs); err != nil {
		return nil, fmt.Errorf("seed facts: vendors: %w", err)
	}
	for _, v := range vs.Vendors {
		doc.Vendors = append(doc.Vendors, factsVendor{ID: v.ID, DisplayName: v.DisplayName, Kind: v.Kind, Website: v.Website})
	}

	var ds driversDoc
	if err := yaml.Unmarshal(driversYAML, &ds); err != nil {
		return nil, fmt.Errorf("seed facts: drivers: %w", err)
	}
	for _, d := range ds.Drivers {
		doc.Drivers = append(doc.Drivers, factsDriver{ID: d.ID, DisplayName: d.DisplayName, Version: d.Version})
	}

	var cs capabilitiesDoc
	if err := yaml.Unmarshal(capabilitiesYAML, &cs); err != nil {
		return nil, fmt.Errorf("seed facts: capabilities: %w", err)
	}
	for _, c := range cs.Capabilities {
		doc.Capabilities = append(doc.Capabilities, factsCapability{ID: c.ID, DisplayName: c.DisplayName})
	}

	var ps productsDoc
	if err := yaml.Unmarshal(productsYAML, &ps); err != nil {
		return nil, fmt.Errorf("seed facts: products: %w", err)
	}
	for _, p := range ps.Products {
		fp := factsProduct{
			ID: p.ID, DisplayName: p.DisplayName, VendorID: p.VendorID, DriverID: p.DriverID,
			Kind: p.Kind, ParentProductID: p.ParentProductID, Capabilities: p.Capabilities,
		}
		for _, pp := range p.Properties {
			fp.Properties = append(fp.Properties, factsProductProperty{Name: pp.Name, Default: pp.Default, Required: pp.Required})
		}
		doc.Products = append(doc.Products, fp)
	}

	out, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, fmt.Errorf("seed facts: marshal: %w", err)
	}
	return append(out, '\n'), nil
}
