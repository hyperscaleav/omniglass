package seed

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/health"
	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/template"
	"github.com/hyperscaleav/omniglass/internal/transport"
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
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Inherits    []string `json:"inherits,omitempty"`
	Declared    []string `json:"declared"`
	Effective   []string `json:"effective"`
}

type factsProperty struct {
	Name        string `json:"name"`
	DataType    string `json:"data_type"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type factsMetricType struct {
	Name        string `json:"name"`
	DataType    string `json:"data_type"`
	Unit        string `json:"unit,omitempty"`
	Precision   *int   `json:"precision,omitempty"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type factsNamed struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// factsTransport renders the code registry (internal/transport, ADR-0073) into
// the same facts artifact the seeded YAMLs feed: the transports are build-time
// facts of the binary, so the docs table derives from the registry rather than
// restating it.
type factsTransport struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Held        bool   `json:"held"`
	Built       bool   `json:"built"`
}

// factsLabelRule is one global label rule (#682) as the docs render it: the
// entity kind and the template this release ships for it. An operator's own
// override is fleet data and is deliberately not a shipped fact.
type factsLabelRule struct {
	EntityKind string `json:"entity_kind"`
	Template   string `json:"template"`
}

type factsLocationType struct {
	ID                 string         `json:"id"`
	Label              string         `json:"label"`
	Icon               string         `json:"icon,omitempty"`
	AllowedParentTypes []string       `json:"allowed_parent_types,omitempty"`
	NameRule           *factsNameRule `json:"name_rule,omitempty"`
}

// factsNameRule is a location_type's name rule as the docs render it (#687):
// absent where an operator names every location of that type, and an empty stem
// where the type is positional and the ordinal alone is the name.
type factsNameRule struct {
	Stem      string `json:"stem"`
	BareFirst bool   `json:"bare_first,omitempty"`
}

// factsTypeNode is one row of an INHERITING classification registry
// (component_type, system_type) as the docs render it (#678).
//
// The blank facts are rendered blank, `omitempty` and all: a child that states
// no icon is inheriting its nearest ancestor's (ADR-0095's first-non-null walk),
// and a render that resolved the chain here would publish a tree in which every
// node states everything, which is the opposite of the rule the page teaches.
type factsTypeNode struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	ParentID    string   `json:"parent_id,omitempty"`
	Stem        string   `json:"stem,omitempty"`
	Abbrev      string   `json:"abbrev,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	DefaultTags []string `json:"default_tags,omitempty"`
}

type factsVendor struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Kind    string `json:"kind,omitempty"`
	Website string `json:"website,omitempty"`
}

type factsDriver struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Version string `json:"version,omitempty"`
	// The spec summary (#813): the transport the spec rides and how many
	// functions each family declares. All zero on a stub with no spec.
	Transport string `json:"transport,omitempty"`
	Polls     int    `json:"polls,omitempty"`
	Listeners int    `json:"listeners,omitempty"`
	Commands  int    `json:"commands,omitempty"`
}

type factsProductProperty struct {
	Name     string `json:"name"`
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type factsProduct struct {
	ID              string                 `json:"id"`
	Label           string                 `json:"label"`
	VendorID        string                 `json:"vendor_id,omitempty"`
	DriverID        string                 `json:"driver_id,omitempty"`
	ComponentType   string                 `json:"component_type,omitempty"`
	Kind            string                 `json:"kind,omitempty"`
	ParentProductID string                 `json:"parent_product_id,omitempty"`
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
	Label                 string                 `json:"label"`
	DefaultAdminSensitive bool                   `json:"default_admin_sensitive"`
	Fields                []factsSecretTypeField `json:"fields"`
}

// factsSeverityLevel is one shipped severity level (#917) as the docs render
// it, with the outage flag computed by the health rule's own floor, so the
// table cannot disagree with what a verdict does.
type factsSeverityLevel struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	Color  string `json:"color"`
	Rank   int    `json:"rank"`
	Outage bool   `json:"outage"`
}

// factsEventRule is one official event rule (#920, #948) as the docs render
// it: the criteria as their expressions (#957), the severity absent for an
// informational rule.
type factsEventRule struct {
	Name           string `json:"name"`
	Label          string `json:"label,omitempty"`
	Description    string `json:"description,omitempty"`
	SampleKey      string `json:"sample_key"`
	Fire           string `json:"fire"`
	Clear          string `json:"clear,omitempty"`
	FireSustain    string `json:"fire_sustain,omitempty"`
	ClearSustain   string `json:"clear_sustain,omitempty"`
	Severity       string `json:"severity,omitempty"`
	FireEventType  string `json:"fire_event_type"`
	ClearEventType string `json:"clear_event_type,omitempty"`
}

// factsNotificationType is one shipped notification type (#618) as the docs
// render it: its two templates as written.
type factsNotificationType struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Subject     string `json:"subject"`
	Text        string `json:"text"`
}

type seedFactsDoc struct {
	Comment           string                  `json:"//"`
	Roles             []factsRole             `json:"roles"`
	PropertyTypes     []factsProperty         `json:"property_types"`
	MetricTypes       []factsMetricType       `json:"metric_types"`
	EventTypes        []factsNamed            `json:"event_types"`
	CommandTypes      []factsNamed            `json:"command_types"`
	NotificationTypes []factsNotificationType `json:"notification_types"`
	SecretTypes       []factsSecretType       `json:"secret_types"`
	Transports        []factsTransport        `json:"transports"`
	LocationTypes     []factsLocationType     `json:"location_types"`
	ComponentTypes    []factsTypeNode         `json:"component_types"`
	SystemTypes       []factsTypeNode         `json:"system_types"`
	Templates         []template.Template     `json:"templates"`
	Vendors           []factsVendor           `json:"vendors"`
	Drivers           []factsDriver           `json:"drivers"`
	Products          []factsProduct          `json:"products"`
	LabelRules        []factsLabelRule        `json:"label_rules"`
	SeverityLevels    []factsSeverityLevel    `json:"severity_levels"`
	EventRules        []factsEventRule        `json:"event_rules"`
}

// eventCommandDoc covers the shared name/display/description shape of the
// event_types and command_types YAMLs for the facts render; the seeders keep
// their own richer structs.
type eventCommandDoc struct {
	EventTypes []struct {
		Name        string `yaml:"name"`
		Label       string `yaml:"label"`
		Description string `yaml:"description"`
	} `yaml:"event_types"`
	CommandTypes []struct {
		Name        string `yaml:"name"`
		Label       string `yaml:"label"`
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
			ID: r.ID, Label: r.Label, Description: r.Description,
			Inherits: r.Inherits, Declared: r.Permissions,
			Effective: idx.Flatten([]string{r.ID}).Strings(),
		})
	}

	var rules labelRulesDoc
	if err := yaml.Unmarshal(labelRulesYAML, &rules); err != nil {
		return nil, fmt.Errorf("seed facts: label rules: %w", err)
	}
	for _, r := range rules.LabelRules {
		doc.LabelRules = append(doc.LabelRules, factsLabelRule{EntityKind: r.EntityKind, Template: r.Template})
	}

	var props propertyTypesDoc
	if err := yaml.Unmarshal(propertyTypesYAML, &props); err != nil {
		return nil, fmt.Errorf("seed facts: property types: %w", err)
	}
	for _, p := range props.PropertyTypes {
		doc.PropertyTypes = append(doc.PropertyTypes, factsProperty{
			Name: p.Name, DataType: p.DataType,
			Label: p.Label, Description: p.Description,
		})
	}

	var mts metricTypesDoc
	if err := yaml.Unmarshal(metricTypesYAML, &mts); err != nil {
		return nil, fmt.Errorf("seed facts: metric types: %w", err)
	}
	for _, m := range mts.MetricTypes {
		doc.MetricTypes = append(doc.MetricTypes, factsMetricType{
			Name: m.Name, DataType: m.DataType, Unit: m.Unit, Precision: m.Precision,
			Label: m.Label, Description: m.Description,
		})
	}

	var evcmd eventCommandDoc
	if err := yaml.Unmarshal(eventTypesYAML, &evcmd); err != nil {
		return nil, fmt.Errorf("seed facts: event types: %w", err)
	}
	for _, e := range evcmd.EventTypes {
		doc.EventTypes = append(doc.EventTypes, factsNamed{Name: e.Name, Label: e.Label, Description: e.Description})
	}
	evcmd = eventCommandDoc{}
	if err := yaml.Unmarshal(commandTypesYAML, &evcmd); err != nil {
		return nil, fmt.Errorf("seed facts: command types: %w", err)
	}
	for _, c := range evcmd.CommandTypes {
		doc.CommandTypes = append(doc.CommandTypes, factsNamed{Name: c.Name, Label: c.Label, Description: c.Description})
	}

	var secrets secretTypesDoc
	if err := yaml.Unmarshal(secretTypesYAML, &secrets); err != nil {
		return nil, fmt.Errorf("seed facts: secret types: %w", err)
	}
	for _, s := range secrets.SecretTypes {
		st := factsSecretType{ID: s.ID, Label: s.Label, DefaultAdminSensitive: s.DefaultAdminSensitive}
		for _, f := range s.Fields {
			st.Fields = append(st.Fields, factsSecretTypeField{Name: f.Name, Type: f.Type, Secret: f.Secret, Origin: f.Origin})
		}
		doc.SecretTypes = append(doc.SecretTypes, st)
	}

	for _, tr := range transport.All() {
		doc.Transports = append(doc.Transports, factsTransport{Name: tr.Name, Description: tr.Description, Held: tr.Held, Built: tr.Built})
	}

	var lts locationTypesDoc
	if err := yaml.Unmarshal(locationTypesYAML, &lts); err != nil {
		return nil, fmt.Errorf("seed facts: location types: %w", err)
	}
	for _, lt := range lts.LocationTypes {
		var rule *factsNameRule
		if lt.NameRule != nil {
			rule = &factsNameRule{Stem: lt.NameRule.Stem, BareFirst: lt.NameRule.BareFirst}
		}
		doc.LocationTypes = append(doc.LocationTypes, factsLocationType{
			ID: lt.ID, Label: lt.Label, Icon: lt.Icon, AllowedParentTypes: lt.AllowedParentTypes,
			NameRule: rule,
		})
	}

	var cts componentTypesDoc
	if err := yaml.Unmarshal(componentTypesYAML, &cts); err != nil {
		return nil, fmt.Errorf("seed facts: component types: %w", err)
	}
	for _, ct := range cts.ComponentTypes {
		doc.ComponentTypes = append(doc.ComponentTypes, factsTypeNode{
			ID: ct.ID, Label: ct.Label, ParentID: ct.ParentID,
			Stem: ct.Stem, Abbrev: ct.Abbrev, Icon: ct.Icon, DefaultTags: ct.DefaultTags,
		})
	}

	var sts systemTypesDoc
	if err := yaml.Unmarshal(systemTypesYAML, &sts); err != nil {
		return nil, fmt.Errorf("seed facts: system types: %w", err)
	}
	for _, st := range sts.SystemTypes {
		doc.SystemTypes = append(doc.SystemTypes, factsTypeNode{
			ID: st.ID, Label: st.Label, ParentID: st.ParentID,
			Stem: st.Stem, Abbrev: st.Abbrev, Icon: st.Icon,
		})
	}

	// The template catalog (ADR-0827) renders whole: every kind, its body as
	// the API serves it, in the catalog's order, so a docs page lists the
	// shipped templates without restating one.
	doc.Templates = template.Shipped().List("")

	var vs vendorsDoc
	if err := yaml.Unmarshal(vendorsYAML, &vs); err != nil {
		return nil, fmt.Errorf("seed facts: vendors: %w", err)
	}
	for _, v := range vs.Vendors {
		doc.Vendors = append(doc.Vendors, factsVendor{ID: v.ID, Label: v.Label, Kind: v.Kind, Website: v.Website})
	}

	var ds driversDoc
	if err := yaml.Unmarshal(driversYAML, &ds); err != nil {
		return nil, fmt.Errorf("seed facts: drivers: %w", err)
	}
	for _, d := range ds.Drivers {
		fd := factsDriver{ID: d.ID, Label: d.Label, Version: d.Version}
		if len(d.Spec) > 0 {
			fd.Transport, _ = d.Spec["transport"].(string)
			count := func(key string) int {
				list, _ := d.Spec[key].([]any)
				return len(list)
			}
			fd.Polls, fd.Listeners, fd.Commands = count("polls"), count("listeners"), count("commands")
		}
		doc.Drivers = append(doc.Drivers, fd)
	}

	var ps productsDoc
	if err := yaml.Unmarshal(productsYAML, &ps); err != nil {
		return nil, fmt.Errorf("seed facts: products: %w", err)
	}
	for _, p := range ps.Products {
		fp := factsProduct{
			ID: p.ID, Label: p.Label, VendorID: p.VendorID, DriverID: p.DriverID,
			ComponentType: p.ComponentType, Kind: p.Kind, ParentProductID: p.ParentProductID,
		}
		for _, pp := range p.Properties {
			fp.Properties = append(fp.Properties, factsProductProperty{Name: pp.Name, Default: pp.Default, Required: pp.Required})
		}
		doc.Products = append(doc.Products, fp)
	}

	var levels struct {
		Levels []struct {
			Name  string `yaml:"name"`
			Label string `yaml:"label"`
			Color string `yaml:"color"`
			Rank  int    `yaml:"rank"`
		} `yaml:"levels"`
	}
	if err := yaml.Unmarshal(severityLevelsYAML, &levels); err != nil {
		return nil, fmt.Errorf("seed facts: severity levels: %w", err)
	}
	for _, l := range levels.Levels {
		doc.SeverityLevels = append(doc.SeverityLevels, factsSeverityLevel{
			Name: l.Name, Label: l.Label, Color: l.Color, Rank: l.Rank, Outage: l.Rank >= health.OutageRank,
		})
	}

	var nts notificationTypesDoc
	if err := yaml.Unmarshal(notificationTypesYAML, &nts); err != nil {
		return nil, fmt.Errorf("seed facts: notification types: %w", err)
	}
	for _, t := range nts.NotificationTypes {
		doc.NotificationTypes = append(doc.NotificationTypes, factsNotificationType{
			Name: t.Name, Label: t.Label, Description: t.Description, Subject: t.Subject, Text: t.Text})
	}

	var ers eventRulesDoc
	if err := yaml.Unmarshal(eventRulesYAML, &ers); err != nil {
		return nil, fmt.Errorf("seed facts: event rules: %w", err)
	}
	for _, r := range ers.Rules {
		fr := factsEventRule{Name: r.Name, Label: r.Label, Description: r.Description, SampleKey: r.SampleKey,
			Fire: r.Fire, Clear: r.Clear, FireSustain: r.FireSustain, ClearSustain: r.ClearSustain, Severity: r.Severity,
			FireEventType: r.FireEventType, ClearEventType: r.ClearEventType}
		doc.EventRules = append(doc.EventRules, fr)
	}

	out, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, fmt.Errorf("seed facts: marshal: %w", err)
	}
	return append(out, '\n'), nil
}
