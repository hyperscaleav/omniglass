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

	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"gopkg.in/yaml.v3"
)

//go:embed roles.yaml
var rolesYAML []byte

//go:embed location_types.yaml
var locationTypesYAML []byte

//go:embed standards.yaml
var standardsYAML []byte

//go:embed property_types.yaml
var propertyTypesYAML []byte

//go:embed metric_types.yaml
var metricTypesYAML []byte

//go:embed event_types.yaml
var eventTypesYAML []byte

//go:embed command_types.yaml
var commandTypesYAML []byte

//go:embed interface_types.yaml
var interfaceTypesYAML []byte

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

type standardsDoc struct {
	Standards []struct {
		ID               string `yaml:"id"`
		Label            string `yaml:"label"`
		ParentStandardID string `yaml:"parent_standard_id"`
		// Choices are exclusive-or groups a conforming system can satisfy
		// through exactly one of their named alternates (#626): an
		// all-in-one video bar, or a component-built codec+camera+dsp+
		// amp+mic. Seeded before Roles, since a role's Choice/Alternate
		// fields point into one.
		Choices []struct {
			Name  string `yaml:"name"`
			Label string `yaml:"label"`
			// Alternates is in the order a tie between two
			// equally-satisfied alternates breaks by
			// (internal/health.Choice.Active).
			Alternates []struct {
				Name  string `yaml:"name"`
				Label string `yaml:"label"`
			} `yaml:"alternates"`
		} `yaml:"choices"`
		// The roles a conforming system needs filled, inherited live by every
		// system on this standard. The typed-slot guard checks AcceptedTypes
		// and PinnedProducts; capability retired with the family (#626).
		// Choice and Alternate are both empty for an unconditional role;
		// naming one without the other is a seed authoring error, refused
		// at boot (see seedStandardRoles).
		Roles []struct {
			Name           string   `yaml:"name"`
			Label          string   `yaml:"label"`
			Quorum         int      `yaml:"quorum"`
			AcceptedTypes  []string `yaml:"accepted_types"`
			PinnedProducts []string `yaml:"pinned_products"`
			Choice         string   `yaml:"choice"`
			Alternate      string   `yaml:"alternate"`
		} `yaml:"roles"`
	} `yaml:"standards"`
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

type interfaceTypesDoc struct {
	InterfaceTypes []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Built       bool   `yaml:"built"`
	} `yaml:"interface_types"`
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
	// The global label rules before any classifier: they are the outermost tier
	// of a ladder every other seeded row can override, and the order a
	// resolver reads them in is the reverse of the order they are written.
	if err := seedLabelRules(ctx, gw); err != nil {
		return err
	}
	if err := seedLocationTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedStandards(ctx, gw); err != nil {
		return err
	}
	if err := seedInterfaceTypes(ctx, gw); err != nil {
		return err
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
	if err := seedVendors(ctx, gw); err != nil {
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
	// Products before either role step: a pinned product is a role's
	// typed-slot requirement the same way an accepted type is, and a choice
	// role can pin one too (#626).
	if err := seedProducts(ctx, gw); err != nil {
		return err
	}
	// Choices before roles: a role's alternate_id points into a choice's
	// alternate, so the choice must exist first, and roles before either of
	// those (the old order) would have nothing to resolve against.
	choiceAlts, err := seedStandardChoices(ctx, gw)
	if err != nil {
		return err
	}
	// After the component_type taxonomy, products, and choices: a declared
	// role's typed-slot requirement points into the first two, and its
	// optional alternate_id into the third.
	if err := seedStandardRoles(ctx, gw, choiceAlts); err != nil {
		return err
	}
	return seedSecretTypes(ctx, gw)
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

func seedInterfaceTypes(ctx context.Context, gw storage.Gateway) error {
	var doc interfaceTypesDoc
	if err := yaml.Unmarshal(interfaceTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse interface_types: %w", err)
	}
	for _, it := range doc.InterfaceTypes {
		if err := gw.UpsertInterfaceType(ctx, storage.InterfaceType{
			Name: it.Name, Official: true, Description: it.Description, Built: it.Built,
		}); err != nil {
			return err
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
		if err := gw.UpsertDriver(ctx, storage.Driver{
			Name: d.ID, Official: true, Label: d.Label, Version: d.Version,
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

func seedStandards(ctx context.Context, gw storage.Gateway) error {
	var doc standardsDoc
	if err := yaml.Unmarshal(standardsYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse standards: %w", err)
	}
	for _, st := range doc.Standards {
		var parent *string
		if st.ParentStandardID != "" {
			parent = &st.ParentStandardID
		}
		// Shipped standards are example content the operator owns once it lands,
		// not authoritative reference data: seeded if absent, never reasserted.
		if err := gw.SeedStandard(ctx, storage.Standard{
			Name:             st.ID,
			Official:         false,
			Label:            st.Label,
			ParentStandardID: parent,
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedStandardChoices installs the exclusive-or role groups the shipped
// standards declare (#626), on the same seed-if-absent lane as the standards
// and their roles: example content the operator owns once it lands, never
// reasserted over an edit. Its own step, before seedStandardRoles, because a
// role's alternate_id points into one of these. Returns each seeded
// standard's alternate ids keyed by "choice/alternate", so seedStandardRoles
// can resolve a role's Choice and Alternate fields without a second parse of
// standards.yaml.
func seedStandardChoices(ctx context.Context, gw storage.Gateway) (map[string]map[string]string, error) {
	var doc standardsDoc
	if err := yaml.Unmarshal(standardsYAML, &doc); err != nil {
		return nil, fmt.Errorf("seed: parse standards: %w", err)
	}
	out := make(map[string]map[string]string, len(doc.Standards))
	for _, st := range doc.Standards {
		if len(st.Choices) == 0 {
			continue
		}
		alts := make(map[string]string)
		for _, c := range st.Choices {
			spec := storage.RoleChoiceSpec{Name: c.Name, Label: c.Label}
			for _, a := range c.Alternates {
				spec.Alternates = append(spec.Alternates, storage.AlternateSpec{Name: a.Name, Label: a.Label})
			}
			ids, err := gw.SeedRoleChoice(ctx, "standard", st.ID, spec)
			if err != nil {
				return nil, fmt.Errorf("seed: standard %s choice %s: %w", st.ID, c.Name, err)
			}
			for name, id := range ids {
				alts[c.Name+"/"+name] = id
			}
		}
		out[st.ID] = alts
	}
	return out, nil
}

// seedStandardRoles installs the roles the shipped standards declare, on the
// same seed-if-absent lane as the standards themselves: example content the
// operator owns once it lands, never reasserted over an edit. Its own step
// rather than part of seedStandards because a role's accepted types and
// pinned products are foreign keys into registries that seed later (the
// component_type taxonomy and the product catalog), and a choice role's
// alternate_id into a third (choiceAlts, from seedStandardChoices).
func seedStandardRoles(ctx context.Context, gw storage.Gateway, choiceAlts map[string]map[string]string) error {
	var doc standardsDoc
	if err := yaml.Unmarshal(standardsYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse standards: %w", err)
	}
	for _, st := range doc.Standards {
		for _, r := range st.Roles {
			// SeedSystemRole is ON CONFLICT DO NOTHING (never reasserts over
			// an operator's edit), so unlike the API layer this never needs
			// to distinguish "omitted" from "explicitly unconditional": a
			// fresh role either names its alternate or does not, and an
			// already-seeded one is untouched either way. A nil pointer
			// (unconditional) is still correct here, not just convenient.
			var altID *string
			if r.Choice != "" || r.Alternate != "" {
				if r.Choice == "" || r.Alternate == "" {
					return fmt.Errorf("seed: standard %s role %s names choice %q and alternate %q, want both or neither",
						st.ID, r.Name, r.Choice, r.Alternate)
				}
				id := choiceAlts[st.ID][r.Choice+"/"+r.Alternate]
				if id == "" {
					return fmt.Errorf("seed: standard %s role %s names unknown choice/alternate %s/%s",
						st.ID, r.Name, r.Choice, r.Alternate)
				}
				altID = &id
			}
			if err := gw.SeedSystemRole(ctx, "standard", st.ID, storage.SystemRoleSpec{
				Name:           r.Name,
				Label:          r.Label,
				Quorum:         r.Quorum,
				AcceptedTypes:  r.AcceptedTypes,
				PinnedProducts: r.PinnedProducts,
				AlternateID:    altID,
			}); err != nil {
				return fmt.Errorf("seed: standard %s role %s: %w", st.ID, r.Name, err)
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
