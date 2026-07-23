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

//go:embed properties.yaml
var propertiesYAML []byte

//go:embed interface_types.yaml
var interfaceTypesYAML []byte

//go:embed vendors.yaml
var vendorsYAML []byte

//go:embed drivers.yaml
var driversYAML []byte

//go:embed capabilities.yaml
var capabilitiesYAML []byte

//go:embed products.yaml
var productsYAML []byte

//go:embed secret_types.yaml
var secretTypesYAML []byte

type rolesDoc struct {
	Roles []struct {
		ID          string   `yaml:"id"`
		DisplayName string   `yaml:"display_name"`
		Description string   `yaml:"description"`
		Permissions []string `yaml:"permissions"`
		Inherits    []string `yaml:"inherits"`
	} `yaml:"roles"`
}

type locationTypesDoc struct {
	LocationTypes []struct {
		ID                 string   `yaml:"id"`
		DisplayName        string   `yaml:"display_name"`
		Icon               string   `yaml:"icon"`
		AllowedParentTypes []string `yaml:"allowed_parent_types"`
	} `yaml:"location_types"`
}

type standardsDoc struct {
	Standards []struct {
		ID               string `yaml:"id"`
		DisplayName      string `yaml:"display_name"`
		ParentStandardID string `yaml:"parent_standard_id"`
		// The roles a conforming system needs filled, inherited live by every
		// system on this standard.
		Roles []struct {
			Name         string   `yaml:"name"`
			DisplayName  string   `yaml:"display_name"`
			Quorum       int      `yaml:"quorum"`
			Capabilities []string `yaml:"capabilities"`
		} `yaml:"roles"`
	} `yaml:"standards"`
}

type propertiesDoc struct {
	Properties []struct {
		Name        string         `yaml:"name"`
		Kind        string         `yaml:"kind"`
		DataType    string         `yaml:"data_type"`
		Unit        string         `yaml:"unit"`
		Validation  map[string]any `yaml:"validation"`
		DisplayName string         `yaml:"display_name"`
		Description string         `yaml:"description"`
	} `yaml:"properties"`
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
		ID          string `yaml:"id"`
		DisplayName string `yaml:"display_name"`
		Kind        string `yaml:"kind"`
		Icon        string `yaml:"icon"`
		Website     string `yaml:"website"`
	} `yaml:"vendors"`
}

type driversDoc struct {
	Drivers []struct {
		ID          string `yaml:"id"`
		DisplayName string `yaml:"display_name"`
		Version     string `yaml:"version"`
	} `yaml:"drivers"`
}

type capabilitiesDoc struct {
	Capabilities []struct {
		ID          string `yaml:"id"`
		DisplayName string `yaml:"display_name"`
	} `yaml:"capabilities"`
}

type productsDoc struct {
	Products []struct {
		ID              string   `yaml:"id"`
		DisplayName     string   `yaml:"display_name"`
		VendorID        string   `yaml:"vendor_id"`
		DriverID        string   `yaml:"driver_id"`
		Kind            string   `yaml:"kind"`
		ParentProductID string   `yaml:"parent_product_id"`
		Capabilities    []string `yaml:"capabilities"`
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
		DisplayName           string `yaml:"display_name"`
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
	if err := seedLocationTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedStandards(ctx, gw); err != nil {
		return err
	}
	if err := seedInterfaceTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedProperties(ctx, gw); err != nil {
		return err
	}
	if err := seedVendors(ctx, gw); err != nil {
		return err
	}
	if err := seedDrivers(ctx, gw); err != nil {
		return err
	}
	if err := seedCapabilities(ctx, gw); err != nil {
		return err
	}
	// After the capability registry: a declared role's requirements point into it.
	if err := seedStandardRoles(ctx, gw); err != nil {
		return err
	}
	if err := seedProducts(ctx, gw); err != nil {
		return err
	}
	return seedSecretTypes(ctx, gw)
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

func seedProperties(ctx context.Context, gw storage.Gateway) error {
	var doc propertiesDoc
	if err := yaml.Unmarshal(propertiesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse properties: %w", err)
	}
	for _, p := range doc.Properties {
		var unit *string
		if p.Unit != "" {
			u := p.Unit
			unit = &u
		}
		var kind *string
		if p.Kind != "" {
			kk := p.Kind
			kind = &kk
		}
		var validation []byte
		if len(p.Validation) > 0 {
			b, err := json.Marshal(p.Validation)
			if err != nil {
				return fmt.Errorf("seed: marshal validation for %q: %w", p.Name, err)
			}
			validation = b
		}
		if err := gw.UpsertPropertyType(ctx, storage.PropertyType{
			Name: p.Name, DisplayName: p.DisplayName, Kind: kind, DataType: p.DataType,
			Unit: unit, Validation: validation, Description: p.Description, Official: true,
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
			DisplayName:           st.DisplayName,
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
			// The seed ships kebab handles, never uuids: the row's id is the
			// database's to mint and must survive a re-seed.
			Name:        v.ID,
			Official:    true,
			DisplayName: v.DisplayName,
			Kind:        v.Kind,
			Icon:        v.Icon,
			Website:     v.Website,
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
			Name: d.ID, Official: true, DisplayName: d.DisplayName, Version: d.Version,
		}); err != nil {
			return err
		}
	}
	return nil
}

func seedCapabilities(ctx context.Context, gw storage.Gateway) error {
	var doc capabilitiesDoc
	if err := yaml.Unmarshal(capabilitiesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse capabilities: %w", err)
	}
	for _, c := range doc.Capabilities {
		if err := gw.UpsertCapability(ctx, storage.Capability{
			Name: c.ID, Official: true, DisplayName: c.DisplayName,
		}); err != nil {
			return err
		}
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
			Name: p.ID, Official: true, DisplayName: p.DisplayName,
			VendorID: nz(p.VendorID), DriverID: nz(p.DriverID),
			ParentProductID: nz(p.ParentProductID), Kind: kind, Capabilities: p.Capabilities,
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
			DisplayName:      st.DisplayName,
			ParentStandardID: parent,
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedStandardRoles installs the roles the shipped standards declare, on the
// same seed-if-absent lane as the standards themselves: example content the
// operator owns once it lands, never reasserted over an edit. Its own step
// rather than part of seedStandards because a role's required capabilities are
// foreign keys into the capability registry, which seeds later.
func seedStandardRoles(ctx context.Context, gw storage.Gateway) error {
	var doc standardsDoc
	if err := yaml.Unmarshal(standardsYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse standards: %w", err)
	}
	for _, st := range doc.Standards {
		for _, r := range st.Roles {
			if err := gw.SeedSystemRole(ctx, "standard", st.ID, storage.SystemRoleSpec{
				Name:         r.Name,
				DisplayName:  r.DisplayName,
				Quorum:       r.Quorum,
				Capabilities: r.Capabilities,
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
			DisplayName: r.DisplayName,
			Description: r.Description,
		}); err != nil {
			return err
		}
	}
	return nil
}

func seedLocationTypes(ctx context.Context, gw storage.Gateway) error {
	var doc locationTypesDoc
	if err := yaml.Unmarshal(locationTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse location_types: %w", err)
	}
	for _, lt := range doc.LocationTypes {
		if err := gw.SeedLocationType(ctx, storage.LocationType{
			Name:               lt.ID,
			Official:           false,
			DisplayName:        lt.DisplayName,
			Icon:               lt.Icon,
			AllowedParentTypes: lt.AllowedParentTypes,
		}); err != nil {
			return err
		}
	}
	return nil
}
