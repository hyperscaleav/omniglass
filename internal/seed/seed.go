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

//go:embed system_types.yaml
var systemTypesYAML []byte

//go:embed component_types.yaml
var componentTypesYAML []byte

//go:embed datapoint_types.yaml
var datapointTypesYAML []byte

//go:embed interface_types.yaml
var interfaceTypesYAML []byte

//go:embed component_makes.yaml
var componentMakesYAML []byte

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

type systemTypesDoc struct {
	SystemTypes []struct {
		ID          string `yaml:"id"`
		DisplayName string `yaml:"display_name"`
	} `yaml:"system_types"`
}

type componentTypesDoc struct {
	ComponentTypes []struct {
		ID          string `yaml:"id"`
		DisplayName string `yaml:"display_name"`
	} `yaml:"component_types"`
}

type datapointTypesDoc struct {
	DatapointTypes []struct {
		Name        string         `yaml:"name"`
		Kind        string         `yaml:"kind"`
		ValueType   string         `yaml:"value_type"`
		Unit        string         `yaml:"unit"`
		Validation  map[string]any `yaml:"validation"`
		DisplayName string         `yaml:"display_name"`
		Description string         `yaml:"description"`
	} `yaml:"datapoint_types"`
}

type interfaceTypesDoc struct {
	InterfaceTypes []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Built       bool   `yaml:"built"`
	} `yaml:"interface_types"`
}

type componentMakesDoc struct {
	ComponentMakes []struct {
		ID          string `yaml:"id"`
		DisplayName string `yaml:"display_name"`
		Icon        string `yaml:"icon"`
		Website     string `yaml:"website"`
	} `yaml:"component_makes"`
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
	if err := seedSystemTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedComponentTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedInterfaceTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedDatapointTypes(ctx, gw); err != nil {
		return err
	}
	if err := seedComponentMakes(ctx, gw); err != nil {
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

func seedDatapointTypes(ctx context.Context, gw storage.Gateway) error {
	var doc datapointTypesDoc
	if err := yaml.Unmarshal(datapointTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse datapoint_types: %w", err)
	}
	for _, dt := range doc.DatapointTypes {
		var unit *string
		if dt.Unit != "" {
			u := dt.Unit
			unit = &u
		}
		var validation []byte
		if len(dt.Validation) > 0 {
			b, err := json.Marshal(dt.Validation)
			if err != nil {
				return fmt.Errorf("seed: marshal validation for %q: %w", dt.Name, err)
			}
			validation = b
		}
		if err := gw.UpsertDatapointType(ctx, storage.DatapointType{
			Scope: "official", Name: dt.Name, DisplayName: dt.DisplayName,
			Kind: dt.Kind, ValueType: dt.ValueType, Unit: unit,
			Validation: validation, Description: dt.Description,
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
			ID:                    st.ID,
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

func seedComponentTypes(ctx context.Context, gw storage.Gateway) error {
	var doc componentTypesDoc
	if err := yaml.Unmarshal(componentTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse component_types: %w", err)
	}
	for _, ct := range doc.ComponentTypes {
		if err := gw.UpsertComponentType(ctx, storage.ComponentType{
			ID:          ct.ID,
			Official:    true,
			DisplayName: ct.DisplayName,
		}); err != nil {
			return err
		}
	}
	return nil
}

func seedComponentMakes(ctx context.Context, gw storage.Gateway) error {
	var doc componentMakesDoc
	if err := yaml.Unmarshal(componentMakesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse component_makes: %w", err)
	}
	for _, cm := range doc.ComponentMakes {
		if err := gw.UpsertComponentMake(ctx, storage.ComponentMake{
			ID:          cm.ID,
			Official:    true,
			DisplayName: cm.DisplayName,
			Icon:        cm.Icon,
			Website:     cm.Website,
		}); err != nil {
			return err
		}
	}
	return nil
}

func seedSystemTypes(ctx context.Context, gw storage.Gateway) error {
	var doc systemTypesDoc
	if err := yaml.Unmarshal(systemTypesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse system_types: %w", err)
	}
	for _, st := range doc.SystemTypes {
		if err := gw.UpsertSystemType(ctx, storage.SystemType{
			ID:          st.ID,
			Official:    true,
			DisplayName: st.DisplayName,
		}); err != nil {
			return err
		}
	}
	return nil
}

func seedRoles(ctx context.Context, gw storage.Gateway) error {
	var doc rolesDoc
	if err := yaml.Unmarshal(rolesYAML, &doc); err != nil {
		return fmt.Errorf("seed: parse roles: %w", err)
	}
	for _, r := range doc.Roles {
		if err := gw.UpsertRole(ctx, storage.Role{
			ID:          r.ID,
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
		if err := gw.UpsertLocationType(ctx, storage.LocationType{
			ID:                 lt.ID,
			Official:           true,
			DisplayName:        lt.DisplayName,
			Icon:               lt.Icon,
			AllowedParentTypes: lt.AllowedParentTypes,
		}); err != nil {
			return err
		}
	}
	return nil
}
