package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Role is a capability set: permissions are <resource>:<action> strings, and
// inherits names parent role ids. official roles ship with the binary and are
// kept authoritative by the boot-seed phase.
type Role struct {
	ID          string
	Official    bool
	Permissions []string
	Inherits    []string
}

// UpsertRole installs or updates a role by id. The boot-seed phase calls it to
// keep the official roles authoritative without disturbing operator-created
// rows. Idempotent.
func (p *PG) UpsertRole(ctx context.Context, r Role) error {
	// Encode empty arrays as '{}', not NULL: a nil slice would violate the
	// not-null array columns.
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	if r.Inherits == nil {
		r.Inherits = []string{}
	}
	_, err := p.pool.Exec(ctx, `
		insert into role (id, official, permissions, inherits)
		values ($1, $2, $3, $4)
		on conflict (id) do update
			set official    = excluded.official,
			    permissions = excluded.permissions,
			    inherits    = excluded.inherits`,
		r.ID, r.Official, r.Permissions, r.Inherits)
	if err != nil {
		return fmt.Errorf("storage: upsert role %q: %w", r.ID, err)
	}
	return nil
}

// OwnerSpec describes the first owner to bootstrap: the human identity plus the
// hashed bearer credential to install. The cleartext token never reaches the
// gateway; the caller generates it, hashes it, and shows it once.
type OwnerSpec struct {
	Username    string
	Email       string
	DisplayName string
	SecretHash  []byte
	Prefix      string
}

// BootstrapOwner creates the first owner in one transaction, idempotent per
// username (the existence check plus the human.username unique constraint).
func (p *PG) BootstrapOwner(ctx context.Context, spec OwnerSpec) (created bool, err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("storage: begin bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existing string
	switch lookupErr := tx.QueryRow(ctx,
		`select principal_id from human where username = $1`, spec.Username).Scan(&existing); {
	case lookupErr == nil:
		return false, nil // already bootstrapped; a no-op that mints no token
	case errors.Is(lookupErr, pgx.ErrNoRows):
		// fall through and create the owner
	default:
		return false, fmt.Errorf("storage: bootstrap lookup: %w", lookupErr)
	}

	var pid string
	if err := tx.QueryRow(ctx,
		`insert into principal (kind) values ('human') returning id`).Scan(&pid); err != nil {
		return false, fmt.Errorf("storage: bootstrap principal: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`insert into human (principal_id, username, email, display_name) values ($1, $2, $3, $4)`,
		pid, spec.Username, nullize(spec.Email), nullize(spec.DisplayName)); err != nil {
		return false, fmt.Errorf("storage: bootstrap human: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		pid, spec.SecretHash, spec.Prefix); err != nil {
		return false, fmt.Errorf("storage: bootstrap credential: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`insert into principal_grant (principal_id, role_id, scope_kind) values ($1, 'owner', 'all')`,
		pid); err != nil {
		return false, fmt.Errorf("storage: bootstrap grant: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("storage: bootstrap commit: %w", err)
	}
	return true, nil
}

// nullize maps an empty string to a SQL NULL so optional text columns stay null
// rather than empty.
func nullize(s string) any {
	if s == "" {
		return nil
	}
	return s
}
