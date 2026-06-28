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

type rolesDoc struct {
	Roles []struct {
		ID          string   `yaml:"id"`
		Permissions []string `yaml:"permissions"`
		Inherits    []string `yaml:"inherits"`
	} `yaml:"roles"`
}

// Run upserts the official roles. Idempotent, so it is safe to call on every
// boot; a release that changes a default role takes effect on the next start.
func Run(ctx context.Context, gw storage.Gateway) error {
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
		}); err != nil {
			return err
		}
	}
	return nil
}
