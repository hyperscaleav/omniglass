// Package seed installs ship-with reference data idempotently on every server
// start: the boot-seed bucket. It upserts authoritative rows (the official
// roles) through the Storage Gateway without touching operator-created data.
// This is distinct from dbmate schema migrations (pure DDL, run once) and from
// one-time data backfills.
package seed

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"time"

	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"gopkg.in/yaml.v3"
)

//go:embed roles.yaml
var rolesYAML []byte

//go:embed severity_levels.yaml
var severityLevelsYAML []byte

//go:embed event_rules.yaml
var eventRulesYAML []byte

//go:embed location_types.yaml
var locationTypesYAML []byte

//go:embed property_types.yaml
var propertyTypesYAML []byte

//go:embed metric_types.yaml
var metricTypesYAML []byte

//go:embed event_types.yaml
var eventTypesYAML []byte

//go:embed notification_types.yaml
var notificationTypesYAML []byte

//go:embed command_types.yaml
var commandTypesYAML []byte

//go:embed vendors.yaml
var vendorsYAML []byte

//go:embed drivers.yaml
var driversYAML []byte

//go:embed component_types.yaml
var componentTypesYAML []byte

//go:embed system_types.yaml
var systemTypesYAML []byte

//go:embed products.yaml
var productsYAML []byte

//go:embed secret_types.yaml
var secretTypesYAML []byte

//go:embed label_rules.yaml
var labelRulesYAML []byte

type rolesDoc struct {
	Roles []struct {
		ID          string   `yaml:"id"`
		Label       string   `yaml:"label"`
		Description string   `yaml:"description"`
		Permissions []string `yaml:"permissions"`
		Inherits    []string `yaml:"inherits"`
	} `yaml:"roles"`
}

type labelRulesDoc struct {
	LabelRules []struct {
		EntityKind string `yaml:"entity_kind"`
		Template   string `yaml:"template"`
	} `yaml:"label_rules"`
}

type locationTypesDoc struct {
	LocationTypes []struct {
		ID                 string   `yaml:"id"`
		Label              string   `yaml:"label"`
		Icon               string   `yaml:"icon"`
		AllowedParentTypes []string `yaml:"allowed_parent_types"`
		// NameRule is the type's opt-in to generating the names of the
		// locations it classifies (#687). A pointer, so absent in the YAML is
		// absent in the column: no rule means an operator names them, which is
		// where three of the four shipped types stay.
		NameRule *struct {
			Stem      string `yaml:"stem"`
			BareFirst bool   `yaml:"bare_first"`
		} `yaml:"name_rule"`
	} `yaml:"location_types"`
}

type propertyTypesDoc struct {
	PropertyTypes []struct {
		Name        string         `yaml:"name"`
		DataType    string         `yaml:"data_type"`
		Validation  map[string]any `yaml:"validation"`
		Label       string         `yaml:"label"`
		Description string         `yaml:"description"`
	} `yaml:"property_types"`
}

type metricTypesDoc struct {
	MetricTypes []struct {
		Name        string `yaml:"name"`
		DataType    string `yaml:"data_type"`
		Unit        string `yaml:"unit"`
		Precision   *int   `yaml:"precision"`
		Label       string `yaml:"label"`
		Description string `yaml:"description"`
	} `yaml:"metric_types"`
}

type vendorsDoc struct {
	Vendors []struct {
		ID      string `yaml:"id"`
		Label   string `yaml:"label"`
		Kind    string `yaml:"kind"`
		Icon    string `yaml:"icon"`
		Website string `yaml:"website"`
	} `yaml:"vendors"`
}

type driversDoc struct {
	Drivers []struct {
		ID      string `yaml:"id"`
		Label   string `yaml:"label"`
		Version string `yaml:"version"`
		// Spec is the declarative body (#813), authored as YAML here and
		// stored as jsonb; absent on a stub that has not been authored yet.
		Spec map[string]any `yaml:"spec"`
	} `yaml:"drivers"`
}

type componentTypesDoc struct {
	ComponentTypes []struct {
		ID          string   `yaml:"id"`
		Label       string   `yaml:"label"`
		Stem        string   `yaml:"stem"`
		Icon        string   `yaml:"icon"`
		Abbrev      string   `yaml:"abbrev"`
		DefaultTags []string `yaml:"default_tags"`
		ParentID    string   `yaml:"parent_id"`
	} `yaml:"component_types"`
}

type systemTypesDoc struct {
	SystemTypes []struct {
		ID       string `yaml:"id"`
		Label    string `yaml:"label"`
		Stem     string `yaml:"stem"`
		Icon     string `yaml:"icon"`
		Abbrev   string `yaml:"abbrev"`
		ParentID string `yaml:"parent_id"`
	} `yaml:"system_types"`
}

type productsDoc struct {
	Products []struct {
		ID              string `yaml:"id"`
		Label           string `yaml:"label"`
		VendorID        string `yaml:"vendor_id"`
		DriverID        string `yaml:"driver_id"`
		Kind            string `yaml:"kind"`
		ComponentType   string `yaml:"component_type"`
		Icon            string `yaml:"icon"`
		ParentProductID string `yaml:"parent_product_id"`
		// The declared-property contract this product ships. `default` is raw JSON
		// (quoted in the YAML) so it round-trips into the jsonb column verbatim.
		Properties []struct {
			Name     string `yaml:"name"`
			Default  string `yaml:"default"`
			Required bool   `yaml:"required"`
		} `yaml:"properties"`
	} `yaml:"products"`
}

type secretTypesDoc struct {
	SecretTypes []struct {
		ID                    string `yaml:"id"`
		Label                 string `yaml:"label"`
		DefaultAdminSensitive bool   `yaml:"default_admin_sensitive"`
		Fields                []struct {
			Name   string `yaml:"name"`
			Type   string `yaml:"type"`
			Secret bool   `yaml:"secret"`
			Origin string `yaml:"origin"`
		} `yaml:"fields"`
	} `yaml:"secret_types"`
}

// Run upserts the ship-with reference data: the official roles and location
// types. Idempotent, so it is safe to call on every boot; a release that changes
// a default takes effect on the next start.
func Run(ctx context.Context, gw storage.Gateway) error {
	if err := seedRoles(ctx, gw); err != nil {
		return err
	}
	// The severity levels before anything that names one: an alarm references
	// a level by name, so the registry lands first.
	if err := seedSeverityLevels(ctx, gw); err != nil {
		return err
	}
	// The global label rules before any classifier: they are the outermost tier
	// of a ladder every other seeded row can override, and the order a
	// resolver reads them in is the reverse of the order they are written.
	if err := seedLabelRules(ctx, gw); err != nil {
		return err
	}
	if err := seedLocationTypes(ctx, gw); err != nil {
		return err
	}
	// The three registries are disjoint by name, or a shared name would be
	// unwritable on every lane (#457): refuse to seed a collision rather than
	// ship it.
	if shared, err := sharedRegistryNames(metricTypesYAML, propertyTypesYAML, eventTypesYAML); err != nil {
		return err
	} else if len(shared) > 0 {
		return fmt.Errorf("seed: the ship-with metric, property and event registries share names %v", shared)
	}
	if err := seedPropertyTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedMetricTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedEventTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedCommandTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedNotificationTypes(ctx, gw); err != nil {
		return err
	}
	// The official event rules after the registries their keys and levels
	// name.
	if err := seedEventRules(ctx, gw); err != nil {
		return err
	}
	if err := seedVendors(ctx, gw); err != nil {
		return err
	}
	// Secret types before drivers: a driver spec's secret input names a
	// secret_type, and the spec validates at write (#813), so on a first boot
	// the type must already exist.
	if err := seedSecretTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedDrivers(ctx, gw); err != nil {
		return err
	}
	if err := seedComponentTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedSystemTypes(ctx, gw); err != nil {
		return err
	}
	// No standards: a standard is example content an operator forks from
	// the template catalog (ADR-0827), never planted by a boot.
	return seedProducts(ctx, gw)
}

// seedLabelRules writes the rule this release ships for each labelled entity
// kind (#682). Authoritative on default_template only: an operator's own rule
// is a separate column, so re-seeding restates what we ship and never reaches
// what they chose.
func seedLabelRules(ctx context.Context, gw storage.Gateway) error {
	var doc labelRulesDoc
	if err := yaml.Unmarshal(labelRulesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse label_rules: %w", err)
	}
	for _, r := range doc.LabelRules {
		if err := gw.UpsertLabelRuleDefault(ctx, r.EntityKind, r.Template); err != nil {
			return fmt.Errorf("seed: label_rule %s: %w", r.EntityKind, err)
		}
	}
	return nil
}

func seedPropertyTypes(ctx context.Context, gw storage.Gateway) error {
	var doc propertyTypesDoc
	if err := yaml.Unmarshal(propertyTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse property_types: %w", err)
	}
	for _, p := range doc.PropertyTypes {
		var validation []byte
		if len(p.Validation) > 0 {
			b, err := json.Marshal(p.Validation)
			if err != nil {
				return fmt.Errorf("seed: marshal validation for %q: %w", p.Name, err)
			}
			validation = b
		}
		if err := gw.UpsertPropertyType(ctx, storage.PropertyType{
			Name: p.Name, Label: p.Label, DataType: p.DataType,
			Validation: validation, Description: p.Description, Official: true,
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedMetricTypes installs the ship-with metric_type canon (the numeric lane of
// the catalog, #587), authoritative on conflict like the property registry.
func seedMetricTypes(ctx context.Context, gw storage.Gateway) error {
	var doc metricTypesDoc
	if err := yaml.Unmarshal(metricTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse metric_types: %w", err)
	}
	for _, m := range doc.MetricTypes {
		var unit *string
		if m.Unit != "" {
			u := m.Unit
			unit = &u
		}
		if err := gw.UpsertMetricType(ctx, storage.MetricType{
			Name: m.Name, Label: m.Label, DataType: m.DataType,
			Unit: unit, Precision: m.Precision, Description: m.Description, Official: true,
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedEventRules installs the official event rules, authoritative on
// conflict; an operator's own rules are untouched.
func seedEventRules(ctx context.Context, gw storage.Gateway) error {
	var doc eventRulesDoc
	if err := yaml.Unmarshal(eventRulesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse event_rules: %w", err)
	}
	for _, r := range doc.Rules {
		spec := storage.EventRuleSpec{Name: r.Name, Label: r.Label, Description: r.Description, SampleKey: r.SampleKey,
			Fire: rules.Criteria{Expr: r.Fire}, Severity: r.Severity, Message: r.Message,
			FireEventType: r.FireEventType, ClearEventType: r.ClearEventType}
		if r.Clear != "" {
			spec.Clear = &rules.Criteria{Expr: r.Clear}
		}
		var err error
		if spec.FireSustain, err = parseSeedDuration(r.FireSustain); err != nil {
			return fmt.Errorf("seed: event rule %s fire_sustain: %w", r.Name, err)
		}
		if spec.ClearSustain, err = parseSeedDuration(r.ClearSustain); err != nil {
			return fmt.Errorf("seed: event rule %s clear_sustain: %w", r.Name, err)
		}
		if err := gw.UpsertEventRule(ctx, spec); err != nil {
			return err
		}
	}
	return nil
}

// eventRulesDoc is event_rules.yaml, read by the seeder and the facts render.
type eventRulesDoc struct {
	Rules []struct {
		Name           string `yaml:"name"`
		Label          string `yaml:"label"`
		Description    string `yaml:"description"`
		SampleKey      string `yaml:"sample_key"`
		Fire           string `yaml:"fire"`
		Clear          string `yaml:"clear"`
		FireSustain    string `yaml:"fire_sustain"`
		ClearSustain   string `yaml:"clear_sustain"`
		Severity       string `yaml:"severity"`
		Message        string `yaml:"message"`
		FireEventType  string `yaml:"fire_event_type"`
		ClearEventType string `yaml:"clear_event_type"`
	} `yaml:"rules"`
}

func parseSeedDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	return time.ParseDuration(s)
}

// seedSeverityLevels installs the official severity levels, authoritative on
// conflict like the other registries; an operator's own levels are untouched.
func seedSeverityLevels(ctx context.Context, gw storage.Gateway) error {
	var doc struct {
		Levels []struct {
			Name  string `yaml:"name"`
			Label string `yaml:"label"`
			Color string `yaml:"color"`
			Rank  int    `yaml:"rank"`
		} `yaml:"levels"`
	}
	if err := yaml.Unmarshal(severityLevelsYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse severity_levels: %w", err)
	}
	for _, l := range doc.Levels {
		if err := gw.UpsertSeverityLevel(ctx, storage.SeverityLevel{
			Name: l.Name, Label: l.Label, Color: l.Color, Rank: l.Rank, Official: true,
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedEventTypes installs the ship-with event_type registry (the occurrence
// keyspace), authoritative on conflict like the property registry.
func seedEventTypes(ctx context.Context, gw storage.Gateway) error {
	var doc struct {
		EventTypes []struct {
			Name        string `yaml:"name"`
			Label       string `yaml:"label"`
			Description string `yaml:"description"`
		} `yaml:"event_types"`
	}
	if err := yaml.Unmarshal(eventTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse event_types: %w", err)
	}
	for _, e := range doc.EventTypes {
		if err := gw.UpsertEventType(ctx, storage.EventType{
			Name: e.Name, Label: e.Label, Description: e.Description, Official: true,
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedCommandTypes installs the ship-with command_type registry (the "do" catalog),
// authoritative on conflict like the other registries. A settleable command carries
// a settle window and one target arm (a property or a metric, never both); a
// fire-and-forget one carries neither. An unregistered or double target fails the
// seed run with the row named (the upsert refuses rather than silently NULLing).
func seedCommandTypes(ctx context.Context, gw storage.Gateway) error {
	var doc struct {
		CommandTypes []struct {
			Name                string `yaml:"name"`
			Label               string `yaml:"label"`
			Description         string `yaml:"description"`
			TargetPropertyType  string `yaml:"target_property_type"`
			TargetMetricType    string `yaml:"target_metric_type"`
			SettleWindowSeconds int    `yaml:"settle_window_seconds"`
		} `yaml:"command_types"`
	}
	if err := yaml.Unmarshal(commandTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse command_types: %w", err)
	}
	for _, c := range doc.CommandTypes {
		if err := gw.UpsertCommandType(ctx, storage.CommandType{
			Name: c.Name, Label: c.Label, Description: c.Description,
			TargetPropertyType: c.TargetPropertyType, TargetMetricType: c.TargetMetricType,
			SettleWindowSeconds: c.SettleWindowSeconds, Official: true,
		}); err != nil {
			return err
		}
	}
	return nil
}

// notificationTypesDoc is the notification_types.yaml shape, shared by the
// seeder and the facts render.
type notificationTypesDoc struct {
	NotificationTypes []struct {
		Name        string `yaml:"name"`
		Label       string `yaml:"label"`
		Description string `yaml:"description"`
		Subject     string `yaml:"subject"`
		Text        string `yaml:"text"`
	} `yaml:"notification_types"`
}

// seedNotificationTypes installs the ship-with notification types (#618),
// authoritative on conflict; a template the sandbox refuses, or a name an
// operator's type already holds, fails the seed run naming the row.
func seedNotificationTypes(ctx context.Context, gw storage.Gateway) error {
	var doc notificationTypesDoc
	if err := yaml.Unmarshal(notificationTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse notification_types: %w", err)
	}
	for _, t := range doc.NotificationTypes {
		if err := gw.UpsertNotificationType(ctx, storage.NotificationType{
			Name: t.Name, Label: t.Label, Description: t.Description,
			Subject: t.Subject, Text: t.Text, Official: true,
		}); err != nil {
			return err
		}
	}
	return nil
}

func seedSecretTypes(ctx context.Context, gw storage.Gateway) error {
	var doc secretTypesDoc
	if err := yaml.Unmarshal(secretTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse secret_types: %w", err)
	}
	for _, st := range doc.SecretTypes {
		fields := make([]secret.Field, len(st.Fields))
		for i, f := range st.Fields {
			fields[i] = secret.Field{Name: f.Name, Type: f.Type, Secret: f.Secret, Origin: secret.Origin(f.Origin)}
		}
		if err := gw.UpsertSecretType(ctx, storage.SecretType{
			Name:                  st.ID,
			Official:              true,
			Label:                 st.Label,
			DefaultAdminSensitive: st.DefaultAdminSensitive,
			Fields:                fields,
		}); err != nil {
			return err
		}
	}
	return nil
}

func seedVendors(ctx context.Context, gw storage.Gateway) error {
	var doc vendorsDoc
	if err := yaml.Unmarshal(vendorsYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse vendors: %w", err)
	}
	for _, v := range doc.Vendors {
		if err := gw.UpsertVendor(ctx, storage.Vendor{
			// The seed ships name, never uuids: the row's id is the
			// database's to mint and must survive a re-seed.
			Name:     v.ID,
			Official: true,
			Label:    v.Label,
			Kind:     v.Kind,
			Icon:     v.Icon,
			Website:  v.Website,
		}); err != nil {
			return err
		}
	}
	return nil
}

func seedDrivers(ctx context.Context, gw storage.Gateway) error {
	var doc driversDoc
	if err := yaml.Unmarshal(driversYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse drivers: %w", err)
	}
	for _, d := range doc.Drivers {
		var spec []byte
		if len(d.Spec) > 0 {
			var err error
			if spec, err = json.Marshal(d.Spec); err != nil {
				return fmt.Errorf("seed: marshal driver %q spec: %w", d.ID, err)
			}
		}
		if err := gw.UpsertDriver(ctx, storage.Driver{
			Name: d.ID, Official: true, Label: d.Label, Version: d.Version, Spec: spec,
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedComponentTypes installs the ship-with component_type taxonomy,
// authoritative on conflict like the other registries. The YAML lists a
// parent before any child that names it (parent_id), so a single pass works:
// each row upserts, then reloads to learn the uuid the database assigned it
// (Upsert only returns an error, matching the other registries), and that id
// is what a later row's parent_id resolves against, since ComponentType.ParentID
// is a real uuid, not a name reference resolved in SQL like the other trees.
func seedComponentTypes(ctx context.Context, gw storage.Gateway) error {
	var doc componentTypesDoc
	if err := yaml.Unmarshal(componentTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse component_types: %w", err)
	}
	nz := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	resolved := make(map[string]uuid.UUID, len(doc.ComponentTypes))
	for _, ct := range doc.ComponentTypes {
		var parentID *uuid.UUID
		if ct.ParentID != "" {
			pid, ok := resolved[ct.ParentID]
			if !ok {
				return fmt.Errorf("seed: component_type %s: parent %s not seeded yet (list parents before children)", ct.ID, ct.ParentID)
			}
			parentID = &pid
		}
		if err := gw.UpsertComponentType(ctx, storage.ComponentType{
			Name: ct.ID, Official: true, Label: ct.Label,
			Stem: nz(ct.Stem), Icon: nz(ct.Icon), Abbrev: nz(ct.Abbrev),
			DefaultTags: ct.DefaultTags, ParentID: parentID,
		}); err != nil {
			return fmt.Errorf("seed: component_type %s: %w", ct.ID, err)
		}
		got, err := gw.GetComponentType(ctx, ct.ID)
		if err != nil {
			return fmt.Errorf("seed: component_type %s: reload after upsert: %w", ct.ID, err)
		}
		resolved[ct.ID] = got.ID
	}
	return nil
}

// seedSystemTypes installs the ship-with system_type taxonomy, on exactly the
// pass seedComponentTypes runs: authoritative on conflict, parents listed
// before the children that name them, and each row reloaded after its upsert to
// learn the uuid the database assigned it, because SystemType.ParentID is a
// real uuid rather than a name reference resolved in SQL.
func seedSystemTypes(ctx context.Context, gw storage.Gateway) error {
	var doc systemTypesDoc
	if err := yaml.Unmarshal(systemTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse system_types: %w", err)
	}
	nz := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	resolved := make(map[string]uuid.UUID, len(doc.SystemTypes))
	for _, st := range doc.SystemTypes {
		var parentID *uuid.UUID
		if st.ParentID != "" {
			pid, ok := resolved[st.ParentID]
			if !ok {
				return fmt.Errorf("seed: system_type %s: parent %s not seeded yet (list parents before children)", st.ID, st.ParentID)
			}
			parentID = &pid
		}
		if err := gw.UpsertSystemType(ctx, storage.SystemType{
			Name: st.ID, Official: true, Label: st.Label,
			Stem: nz(st.Stem), Icon: nz(st.Icon), Abbrev: nz(st.Abbrev), ParentID: parentID,
		}); err != nil {
			return fmt.Errorf("seed: system_type %s: %w", st.ID, err)
		}
		got, err := gw.GetSystemType(ctx, st.ID)
		if err != nil {
			return fmt.Errorf("seed: system_type %s: reload after upsert: %w", st.ID, err)
		}
		resolved[st.ID] = got.ID
	}
	return nil
}

func seedProducts(ctx context.Context, gw storage.Gateway) error {
	var doc productsDoc
	if err := yaml.Unmarshal(productsYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse products: %w", err)
	}
	nz := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	for _, p := range doc.Products {
		kind := p.Kind
		if kind == "" {
			kind = "device"
		}
		if err := gw.UpsertProduct(ctx, storage.Product{
			Name: p.ID, Official: true, Label: p.Label,
			VendorID: nz(p.VendorID), DriverID: nz(p.DriverID),
			ParentProductID: nz(p.ParentProductID), Kind: kind,
			ComponentType: p.ComponentType, Icon: nz(p.Icon),
		}); err != nil {
			return err
		}
		// The product's declared-property contract. Seeded through the unaudited
		// upsert path (seed owns official rows), so a re-run is a no-op.
		for _, prop := range p.Properties {
			var def json.RawMessage
			if prop.Default != "" {
				def = json.RawMessage(prop.Default)
			}
			if err := gw.UpsertProductProperty(ctx, p.ID, storage.ProductPropertySpec{
				PropertyTypeName: prop.Name, DefaultValue: def, Required: prop.Required,
			}); err != nil {
				return fmt.Errorf("seed: product %s property %s: %w", p.ID, prop.Name, err)
			}
		}
	}
	return nil
}

// SeedRole is one official role as declared in roles.yaml, before the DB upsert.
type SeedRole struct {
	ID          string
	Permissions []string
	Inherits    []string
}

// SeededRoles parses the embedded roles.yaml and returns the official roles as
// declared. It is the same source seedRoles upserts, exposed so a test can assert
// the seed against other invariants (e.g. that every granted permission maps to a
// routed capability) without standing up a database. A parse failure is a
// build-time bug in an embedded asset, so it panics rather than returning an error.
func SeededRoles() []SeedRole {
	var doc rolesDoc
	if err := yaml.Unmarshal(rolesYAML, &doc); err != nil {
		panic("seed: parse roles.yaml: " + err.Error())
	}
	out := make([]SeedRole, 0, len(doc.Roles))
	for _, r := range doc.Roles {
		out = append(out, SeedRole{ID: r.ID, Permissions: r.Permissions, Inherits: r.Inherits})
	}
	return out
}

func seedRoles(ctx context.Context, gw storage.Gateway) error {
	var doc rolesDoc
	if err := yaml.Unmarshal(rolesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse roles: %w", err)
	}
	for _, r := range doc.Roles {
		if err := gw.UpsertRole(ctx, storage.Role{
			Name:        r.ID,
			Official:    true,
			Permissions: r.Permissions,
			Inherits:    r.Inherits,
			Label:       r.Label,
			Description: r.Description,
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedLocationTypes writes the shipped location types AUTHORITATIVELY, as
// official rows the platform owns (#703). It was insert-if-absent, on the
// argument that a fleet shapes its own place vocabulary, and that argument
// is now carried by the fork instead: an operator's edit of a shipped type
// lives in registry_shadow and is resolved over the row, so the seed can
// rewrite the row on every boot without reaching anything they own. What that
// buys is withdrawal: a value dropped from location_types.yaml leaves an
// fleet that already seeded it, which insert-if-absent could never do.
func seedLocationTypes(ctx context.Context, gw storage.Gateway) error {
	var doc locationTypesDoc
	if err := yaml.Unmarshal(locationTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse location_types: %w", err)
	}
	for _, lt := range doc.LocationTypes {
		var rule *storage.NameRule
		if lt.NameRule != nil {
			rule = &storage.NameRule{Stem: lt.NameRule.Stem, BareFirst: lt.NameRule.BareFirst}
		}
		if err := gw.UpsertLocationType(ctx, storage.LocationType{
			Name:               lt.ID,
			Official:           true,
			Label:              lt.Label,
			Icon:               lt.Icon,
			AllowedParentTypes: lt.AllowedParentTypes,
			NameRule:           rule,
		}); err != nil {
			return err
		}
	}
	return nil
}
