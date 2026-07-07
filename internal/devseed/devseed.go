// Package devseed installs a small example estate (locations, users, grants) for
// a dev environment, applied by `make dev` through the trusted direct-DB lane. It
// is idempotent: rows that already exist are left untouched, so it runs safely on
// every start.
//
// It is deliberately separate from the boot seed (internal/seed), which installs
// ship-with reference data on every server start in every environment. These are
// operator rows, not reference data, and they must NEVER run in production. The
// grants and locations here reference boot-seed reference data (roles, location
// types) by foreign key, so the boot seed must run first.
package devseed

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"gopkg.in/yaml.v3"
)

//go:embed fixtures.yaml
var fixturesYAML []byte

// Doc is the parsed example-data fixture.
type Doc struct {
	Locations []Location `yaml:"locations"`
	Users     []User     `yaml:"users"`
}

// Location is one node of the example tree. Parent names a location declared
// earlier in the document (empty for a root).
type Location struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"display_name"`
	Type        string `yaml:"type"`
	Parent      string `yaml:"parent"`
}

// User is one example human principal with a known password and its grants.
type User struct {
	Username    string  `yaml:"username"`
	Password    string  `yaml:"password"`
	DisplayName string  `yaml:"display_name"`
	Grants      []Grant `yaml:"grants"`
}

// Grant assigns a role at a scope. ScopeRef names a fixture location (resolved to
// its id at seed time) for a location-scoped grant; it is empty for the all scope.
type Grant struct {
	Role      string `yaml:"role"`
	ScopeKind string `yaml:"scope_kind"`
	ScopeRef  string `yaml:"scope_ref"`
	ScopeOp   string `yaml:"scope_op"`
}

// Fixtures parses the embedded example data. Exposed so a pure unit test can
// check the fixture's shape without a database.
func Fixtures() (Doc, error) {
	var doc Doc
	if err := yaml.Unmarshal(fixturesYAML, &doc); err != nil {
		return Doc{}, fmt.Errorf("devseed: parse fixtures: %w", err)
	}
	return doc, nil
}

// Run installs the example estate idempotently through the Storage Gateway.
// actorID is the audit actor for the created rows (empty for a system actor). The
// trusted lane grants the all scope; callers are the direct-DB commands, never a
// request handler.
func Run(ctx context.Context, gw storage.Gateway, actorID string) error {
	doc, err := Fixtures()
	if err != nil {
		return err
	}
	all := scope.Set{All: true}

	// Locations first, parents before children (the fixture is ordered so) so a
	// child's parent resolves. locIDs lets a later grant address a location by name.
	locIDs := map[string]string{}
	for _, l := range doc.Locations {
		if existing, err := gw.GetLocation(ctx, l.Name, all); err == nil {
			locIDs[l.Name] = existing.ID
			continue
		} else if !errors.Is(err, storage.ErrLocationNotFound) {
			return fmt.Errorf("devseed: check location %q: %w", l.Name, err)
		}
		spec := storage.LocationSpec{Name: l.Name, DisplayName: l.DisplayName, LocationType: l.Type}
		if l.Parent != "" {
			spec.ParentName = &l.Parent
		}
		created, err := gw.CreateLocation(ctx, actorID, spec, all)
		if err != nil {
			return fmt.Errorf("devseed: create location %q: %w", l.Name, err)
		}
		locIDs[l.Name] = created.ID
	}

	// Users next. A user that already exists (ErrUsernameTaken) is left as is,
	// grants included: those were created alongside the user on the first run, so a
	// re-run neither re-creates the user nor duplicates its grants. A user and its
	// grants are not one transaction (the Gateway has no cross-entity write), so an
	// infra fault between them could leave a user under-granted, and a re-run would
	// skip it. That is an accepted limit of dev-only fixture data: reset with
	// `docker compose down -v` and re-run `make dev`.
	for _, u := range doc.Users {
		hash, err := auth.HashPassword(u.Password)
		if err != nil {
			return fmt.Errorf("devseed: hash password for %q: %w", u.Username, err)
		}
		pr, err := gw.CreateHumanPrincipal(ctx, actorID, storage.HumanSpec{
			Username:     u.Username,
			DisplayName:  u.DisplayName,
			PasswordHash: hash,
		}, all)
		if errors.Is(err, storage.ErrUsernameTaken) {
			continue
		}
		if err != nil {
			return fmt.Errorf("devseed: create user %q: %w", u.Username, err)
		}
		for _, g := range u.Grants {
			spec := storage.GrantSpec{Role: g.Role, ScopeKind: g.ScopeKind, ScopeOp: g.ScopeOp}
			if g.ScopeKind != "all" {
				id, ok := locIDs[g.ScopeRef]
				if !ok {
					return fmt.Errorf("devseed: user %q grant references unknown location %q", u.Username, g.ScopeRef)
				}
				spec.ScopeID = id
			}
			if _, err := gw.CreateGrant(ctx, actorID, pr.ID, spec, all); err != nil {
				return fmt.Errorf("devseed: grant %s to %q: %w", g.Role, u.Username, err)
			}
		}
	}
	return nil
}
