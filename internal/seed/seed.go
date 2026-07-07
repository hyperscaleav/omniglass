// Package seed installs ship-with reference data idempotently on every server
// start: the boot-seed bucket. It upserts authoritative rows (the official
// roles) through the Storage Gateway without touching operator-created data.
// This is distinct from dbmate schema migrations (pure DDL, run once) and from
// one-time data backfills.
package seed

import (
	"context"
	_ "embed"
	"fmt"

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
		ID          string `yaml:"id"`
		DisplayName string `yaml:"display_name"`
		Rank        int    `yaml:"rank"`
		Icon        string `yaml:"icon"`
	} `yaml:"location_types"`
}

type systemTypesDoc struct {
	SystemTypes []struct {
		ID          string `yaml:"id"`
		DisplayName string `yaml:"display_name"`
		Rank        int    `yaml:"rank"`
	} `yaml:"system_types"`
}

type componentTypesDoc struct {
	ComponentTypes []struct {
		ID          string `yaml:"id"`
		DisplayName string `yaml:"display_name"`
		Rank        int    `yaml:"rank"`
	} `yaml:"component_types"`
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
	return seedComponentTypes(ctx, gw)
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
			Rank:        ct.Rank,
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
			Rank:        st.Rank,
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
			ID:          lt.ID,
			Official:    true,
			DisplayName: lt.DisplayName,
			Rank:        lt.Rank,
			Icon:        lt.Icon,
		}); err != nil {
			return err
		}
	}
	return nil
}
