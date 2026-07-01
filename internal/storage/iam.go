package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	// PasswordHash, when set, installs an argon2id password credential (PHC
	// encoded) so the owner can log in with a username and password. Optional.
	PasswordHash string
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
	if spec.PasswordHash != "" {
		if _, err := tx.Exec(ctx,
			`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'password', $2, '')`,
			pid, []byte(spec.PasswordHash)); err != nil {
			return false, fmt.Errorf("storage: bootstrap password: %w", err)
		}
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

// IssueBearerCredential mints an additional bearer credential for an existing
// principal addressed by its human username, returning false if no such
// username exists. The caller generates and hashes the token and shows the
// cleartext once; this is the same trusted direct-DB lane as BootstrapOwner
// (token reissue, break-glass, and the `make dev` login).
func (p *PG) IssueBearerCredential(ctx context.Context, username string, hash []byte, prefix string) (bool, error) {
	var pid string
	err := p.pool.QueryRow(ctx, `select principal_id from human where username = $1`, username).Scan(&pid)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("storage: lookup human %q: %w", username, err)
	}
	if _, err := p.pool.Exec(ctx,
		`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		pid, hash, prefix); err != nil {
		return false, fmt.Errorf("storage: issue credential: %w", err)
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

// ErrCredentialNotFound is returned by AuthenticateBearer when no credential
// matches the presented token hash. The authn middleware maps it to 401.
var ErrCredentialNotFound = errors.New("storage: credential not found")

// Principal is an authenticated identity with its kind profile and grants.
type Principal struct {
	ID      string
	Kind    string
	Active  bool
	Human   *HumanProfile
	Service *ServiceProfile
	Grants  []Grant
}

// HumanProfile and ServiceProfile carry the kind-specific attributes.
type HumanProfile struct{ Username, Email, DisplayName string }
type ServiceProfile struct{ Label string }

// Grant is one (role x scope) pairing on a principal, addressable by its id (so
// the admin surface can revoke a specific one).
type Grant struct {
	ID        string
	Role      string
	ScopeKind string
	ScopeID   *string
}

// ErrBadCredentials is returned by AuthenticatePassword when the username is
// unknown, has no password set, or the password does not match: one error for all
// three so the handler cannot leak which.
var ErrBadCredentials = errors.New("storage: bad credentials")

// AuthenticateBearer resolves a bearer credential by its sha256 hash to the
// principal, its kind profile, and its grants. ErrCredentialNotFound if none.
func (p *PG) AuthenticateBearer(ctx context.Context, hash []byte) (*Principal, error) {
	var pr Principal
	err := p.pool.QueryRow(ctx, `
		select pr.id, pr.kind
		from credential c
		join principal pr on pr.id = c.principal_id
		where c.kind = 'bearer' and c.secret_hash = $1 and pr.active`, hash).Scan(&pr.ID, &pr.Kind)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrCredentialNotFound
	case err != nil:
		return nil, fmt.Errorf("storage: authenticate: %w", err)
	}
	if err := p.loadPrincipal(ctx, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// AuthenticatePassword verifies a human's password against their argon2id
// credential and resolves the principal, its profile, and its grants.
// ErrBadCredentials for an unknown user, no password set, or a wrong password.
func (p *PG) AuthenticatePassword(ctx context.Context, username, password string) (*Principal, error) {
	pr := Principal{Kind: "human"}
	var encoded []byte
	err := p.pool.QueryRow(ctx, `
		select h.principal_id, c.secret_hash
		from human h
		join principal pr on pr.id = h.principal_id
		join credential c on c.principal_id = h.principal_id and c.kind = 'password'
		where h.username = $1 and pr.active`, username).Scan(&pr.ID, &encoded)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrBadCredentials
	case err != nil:
		return nil, fmt.Errorf("storage: authenticate password: %w", err)
	}
	ok, err := auth.VerifyPassword(password, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("storage: verify password: %w", err)
	}
	if !ok {
		return nil, ErrBadCredentials
	}
	if err := p.loadPrincipal(ctx, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// SetPassword installs or replaces the password credential for a human, returning
// false if no such username exists. The caller passes the PHC-encoded argon2id
// hash; cleartext never reaches storage.
func (p *PG) SetPassword(ctx context.Context, username, encoded string) (bool, error) {
	var pid string
	err := p.pool.QueryRow(ctx, `select principal_id from human where username = $1`, username).Scan(&pid)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("storage: lookup human %q: %w", username, err)
	}
	if _, err := p.pool.Exec(ctx, `
		insert into credential (principal_id, kind, secret_hash, prefix)
		values ($1, 'password', $2, '')
		on conflict (principal_id) where kind = 'password'
			do update set secret_hash = excluded.secret_hash`,
		pid, []byte(encoded)); err != nil {
		return false, fmt.Errorf("storage: set password: %w", err)
	}
	return true, nil
}

// HumanProfilePatch carries the editable self-profile fields. A nil pointer
// leaves the column unchanged; a non-nil pointer sets it, with an empty string
// clearing the nullable column to NULL.
type HumanProfilePatch struct {
	DisplayName *string
	Email       *string
}

// UpdateHumanProfile applies a partial update to a human's own profile, addressed
// by principal id (the authenticated session's own id). Absent fields are left
// as-is; a provided empty string clears the column. The row exists by
// construction (the caller is that principal), so a no-match is not signalled.
func (p *PG) UpdateHumanProfile(ctx context.Context, principalID string, patch HumanProfilePatch) error {
	setDisplay, display := patch.DisplayName != nil, any(nil)
	if patch.DisplayName != nil {
		display = nullize(*patch.DisplayName)
	}
	setEmail, email := patch.Email != nil, any(nil)
	if patch.Email != nil {
		email = nullize(*patch.Email)
	}
	if _, err := p.pool.Exec(ctx, `
		update human set
			display_name = case when $2 then $3 else display_name end,
			email        = case when $4 then $5 else email end
		where principal_id = $1`,
		principalID, setDisplay, display, setEmail, email); err != nil {
		return fmt.Errorf("storage: update human profile: %w", err)
	}
	return nil
}

// AnyHuman reports whether any human principal exists, so the login screen hides
// the bootstrap hint once the system has an owner.
func (p *PG) AnyHuman(ctx context.Context) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `select exists(select 1 from human)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("storage: any human: %w", err)
	}
	return exists, nil
}

// RevokeBearer deletes the bearer credential with the given sha256 hash (logout
// of a session token). A no-op if none matches.
func (p *PG) RevokeBearer(ctx context.Context, hash []byte) error {
	if _, err := p.pool.Exec(ctx,
		`delete from credential where kind = 'bearer' and secret_hash = $1`, hash); err != nil {
		return fmt.Errorf("storage: revoke bearer: %w", err)
	}
	return nil
}

// ErrPrincipalForbidden is returned by the principal directory methods when the
// caller's resolved scope is not all-scope. A principal is not a scope-tree
// entity, so a location or system grant confers no principal access; only an
// all-scope grant does. The API maps it to 403.
var ErrPrincipalForbidden = errors.New("storage: principal access requires an all-scope grant")

// ErrPrincipalNotFound is returned by GetPrincipal when no principal has the id.
// The API maps it to 404.
var ErrPrincipalNotFound = errors.New("storage: principal not found")

// ErrUsernameTaken is returned by CreateHumanPrincipal when the username already
// exists. The API maps it to 409.
var ErrUsernameTaken = errors.New("storage: username already exists")

// HumanSpec is the admin create-a-human input. PasswordHash, when set, installs a
// password credential (PHC-encoded argon2id); cleartext never reaches storage.
type HumanSpec struct {
	Username     string
	Email        string
	DisplayName  string
	PasswordHash string
}

// ListPrincipals returns every principal with its profile and grants, oldest
// first. Reads require an all-scope grant (a principal is not scope-tree scoped),
// so a non-all read scope is ErrPrincipalForbidden rather than a silent empty
// list. Credentials are never loaded, so no secret leaves the gateway.
func (p *PG) ListPrincipals(ctx context.Context, read scope.Set) ([]Principal, error) {
	if !read.All {
		return nil, ErrPrincipalForbidden
	}
	rows, err := p.pool.Query(ctx, `select id, kind, active from principal order by created_at`)
	if err != nil {
		return nil, fmt.Errorf("storage: list principals: %w", err)
	}
	type base struct {
		id, kind string
		active   bool
	}
	var bases []base
	for rows.Next() {
		var b base
		if err := rows.Scan(&b.id, &b.kind, &b.active); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan principal: %w", err)
		}
		bases = append(bases, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: list principals: %w", err)
	}
	// loadPrincipal runs its own queries, so the row cursor is drained first.
	out := make([]Principal, 0, len(bases))
	for _, b := range bases {
		pr := Principal{ID: b.id, Kind: b.kind, Active: b.active}
		if err := p.loadPrincipal(ctx, &pr); err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, nil
}

// GetPrincipal resolves one principal by id, with its profile and grants. Reads
// require an all-scope grant; an unknown id is ErrPrincipalNotFound.
func (p *PG) GetPrincipal(ctx context.Context, id string, read scope.Set) (*Principal, error) {
	if !read.All {
		return nil, ErrPrincipalForbidden
	}
	pr := Principal{ID: id}
	err := p.pool.QueryRow(ctx, `select id, kind, active from principal where id = $1`, id).Scan(&pr.ID, &pr.Kind, &pr.Active)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrPrincipalNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "22P02":
		// A malformed id (invalid uuid text) identifies no principal: a clean 404,
		// not a 500.
		return nil, ErrPrincipalNotFound
	case err != nil:
		return nil, fmt.Errorf("storage: get principal: %w", err)
	}
	if err := p.loadPrincipal(ctx, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// CreateHumanPrincipal creates a human principal (and, when a password hash is
// given, its password credential) in one audited transaction. Creates require an
// all-scope grant; a duplicate username is ErrUsernameTaken. The new principal
// holds no grants (role assignment is a later admin surface).
func (p *PG) CreateHumanPrincipal(ctx context.Context, actorID string, spec HumanSpec, create scope.Set) (*Principal, error) {
	if !create.All {
		return nil, ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create principal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pid string
	if err := tx.QueryRow(ctx, `insert into principal (kind) values ('human') returning id`).Scan(&pid); err != nil {
		return nil, fmt.Errorf("storage: create principal: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`insert into human (principal_id, username, email, display_name) values ($1, $2, $3, $4)`,
		pid, spec.Username, nullize(spec.Email), nullize(spec.DisplayName)); err != nil {
		return nil, mapPrincipalWriteErr(err)
	}
	if spec.PasswordHash != "" {
		if _, err := tx.Exec(ctx,
			`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'password', $2, '')`,
			pid, []byte(spec.PasswordHash)); err != nil {
			return nil, fmt.Errorf("storage: create password: %w", err)
		}
	}
	summary := map[string]any{"username": spec.Username, "kind": "human"}
	if err := writeAuditRes(ctx, tx, actorID, "create", "principal", pid, nil, summary); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create principal: %w", err)
	}

	pr := Principal{ID: pid, Kind: "human"}
	if err := p.loadPrincipal(ctx, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// ErrPrincipalNotHuman is returned when an admin profile update targets a
// non-human principal (only humans have username / email / display name). The API
// maps it to 422.
var ErrPrincipalNotHuman = errors.New("storage: principal is not a human")

// AdminHumanPatch carries the admin-editable fields of a human principal. A nil
// pointer leaves the field unchanged; a provided empty string clears the nullable
// display name or email. Username is not nullable: a provided empty string is a
// request fault the API rejects before the gateway.
type AdminHumanPatch struct {
	DisplayName *string
	Email       *string
	Username    *string
}

// UpdatePrincipalHuman applies an admin profile update to a human principal by id,
// in one audited transaction. Requires an all-scope grant. A non-human target is
// ErrPrincipalNotHuman, an unknown id ErrPrincipalNotFound, a username clash
// ErrUsernameTaken. Renaming is safe: nothing keys on the username (credentials
// and grants reference the principal id), so a rename follows the identity.
func (p *PG) UpdatePrincipalHuman(ctx context.Context, actorID, id string, patch AdminHumanPatch, action scope.Set) (*Principal, error) {
	if !action.All {
		return nil, ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update principal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var kind string
	err = tx.QueryRow(ctx, `select kind from principal where id = $1`, id).Scan(&kind)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrPrincipalNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "22P02":
		return nil, ErrPrincipalNotFound
	case err != nil:
		return nil, fmt.Errorf("storage: update principal lookup: %w", err)
	}
	if kind != "human" {
		return nil, ErrPrincipalNotHuman
	}

	// The audit "before" is the current human row.
	var before HumanProfile
	if err := tx.QueryRow(ctx,
		`select username, coalesce(email, ''), coalesce(display_name, '') from human where principal_id = $1`,
		id).Scan(&before.Username, &before.Email, &before.DisplayName); err != nil {
		return nil, fmt.Errorf("storage: update principal before: %w", err)
	}

	setDisplay, display := patch.DisplayName != nil, any(nil)
	if patch.DisplayName != nil {
		display = nullize(*patch.DisplayName)
	}
	setEmail, email := patch.Email != nil, any(nil)
	if patch.Email != nil {
		email = nullize(*patch.Email)
	}
	setUsername := patch.Username != nil
	var username any
	if patch.Username != nil {
		username = *patch.Username
	}
	if _, err := tx.Exec(ctx, `
		update human set
			display_name = case when $2 then $3 else display_name end,
			email        = case when $4 then $5 else email end,
			username     = case when $6 then $7 else username end
		where principal_id = $1`,
		id, setDisplay, display, setEmail, email, setUsername, username); err != nil {
		return nil, mapPrincipalWriteErr(err)
	}

	after := before
	if patch.DisplayName != nil {
		after.DisplayName = *patch.DisplayName
	}
	if patch.Email != nil {
		after.Email = *patch.Email
	}
	if patch.Username != nil {
		after.Username = *patch.Username
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "principal", id, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update principal: %w", err)
	}

	pr := Principal{ID: id, Kind: "human"}
	if err := p.loadPrincipal(ctx, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// Grant-management sentinels. The API maps them to 409 (last owner / duplicate),
// 404 (unknown grant), and 422 (unknown role / bad scope).
var (
	ErrLastOwner     = errors.New("storage: at least one owner grant must remain")
	ErrGrantNotFound = errors.New("storage: grant not found")
	ErrGrantExists   = errors.New("storage: grant already exists")
	ErrUnknownRole   = errors.New("storage: unknown role")
	ErrBadScope      = errors.New("storage: invalid scope for a grant")
)

// GrantSpec is a role x scope to assign to a principal. ScopeID is empty for the
// "all" scope; for any other kind it names the scope root.
type GrantSpec struct {
	Role      string
	ScopeKind string
	ScopeID   string
}

// CreateGrant assigns a role x scope to a principal, audited. Requires an
// all-scope grant. A non-all scope with no scope id is ErrBadScope; an unknown
// role ErrUnknownRole; an unknown principal ErrPrincipalNotFound; a duplicate
// ErrGrantExists.
func (p *PG) CreateGrant(ctx context.Context, actorID, principalID string, spec GrantSpec, action scope.Set) (*Grant, error) {
	if !action.All {
		return nil, ErrPrincipalForbidden
	}
	if spec.ScopeKind == "all" {
		spec.ScopeID = "" // the all scope has no id
	} else if spec.ScopeID == "" {
		return nil, ErrBadScope // a scoped grant must name its root
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A scoped grant must target a real entity of that kind by id, so the scope
	// filter (which matches entity ids) resolves. This rejects a grant scoped to a
	// name or a non-existent id up front, rather than 500-ing the scoped list later.
	if spec.ScopeKind != "all" {
		tbl, ok := scopeKindTable(spec.ScopeKind)
		if !ok {
			return nil, ErrBadScope // "group" and any other non-tree kind are unsupported
		}
		if _, perr := uuid.Parse(spec.ScopeID); perr != nil {
			return nil, ErrBadScope
		}
		var exists bool
		if err := tx.QueryRow(ctx,
			`select exists(select 1 from `+string(tbl)+` where id = $1)`, spec.ScopeID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("storage: scope target check: %w", err)
		}
		if !exists {
			return nil, ErrBadScope
		}
	}

	var gid string
	err = tx.QueryRow(ctx,
		`insert into principal_grant (principal_id, role_id, scope_kind, scope_id) values ($1, $2, $3, $4) returning id`,
		principalID, spec.Role, spec.ScopeKind, nullize(spec.ScopeID)).Scan(&gid)
	if err != nil {
		return nil, mapGrantWriteErr(err)
	}
	g := Grant{ID: gid, Role: spec.Role, ScopeKind: spec.ScopeKind}
	if spec.ScopeID != "" {
		g.ScopeID = &spec.ScopeID
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "principal_grant", gid, nil, g); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create grant: %w", err)
	}
	return &g, nil
}

// RevokeGrant deletes one grant from a principal, audited. Requires an all-scope
// grant. An unknown grant (for that principal) is ErrGrantNotFound. Revoking the
// last owner grant is refused by the deferred owner-invariant trigger, surfaced at
// COMMIT as ErrLastOwner.
func (p *PG) RevokeGrant(ctx context.Context, actorID, principalID, grantID string, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin revoke grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var g Grant
	err = tx.QueryRow(ctx,
		`select id, role_id, scope_kind, scope_id from principal_grant where id = $1 and principal_id = $2`,
		grantID, principalID).Scan(&g.ID, &g.Role, &g.ScopeKind, &g.ScopeID)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrGrantNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "22P02":
		return ErrGrantNotFound
	case err != nil:
		return fmt.Errorf("storage: revoke grant lookup: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from principal_grant where id = $1`, grantID); err != nil {
		return fmt.Errorf("storage: revoke grant: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "principal_grant", grantID, g, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		// The deferred owner-invariant trigger raises OG001 at COMMIT.
		if errors.As(err, &pgErr) && pgErr.Code == "OG001" {
			return ErrLastOwner
		}
		return fmt.Errorf("storage: commit revoke grant: %w", err)
	}
	return nil
}

// SetPrincipalActive enables or disables a principal (soft), audited. Requires an
// all-scope grant. Disabling the last active owner is refused (ErrLastOwner); a
// disabled principal cannot authenticate, and enabling restores access.
func (p *PG) SetPrincipalActive(ctx context.Context, actorID, id string, active bool, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin set active: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var kind string
	var wasActive bool
	err = tx.QueryRow(ctx, `select kind, active from principal where id = $1`, id).Scan(&kind, &wasActive)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrPrincipalNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "22P02":
		return ErrPrincipalNotFound
	case err != nil:
		return fmt.Errorf("storage: set active lookup: %w", err)
	}
	if _, err := tx.Exec(ctx, `update principal set active = $2 where id = $1`, id, active); err != nil {
		return fmt.Errorf("storage: set active: %w", err)
	}
	if !active {
		// Refuse if disabling this principal leaves no active owner@all grant (the
		// owner invariant for the active flag, mirroring the grant trigger).
		var ownerRemains bool
		if err := tx.QueryRow(ctx, `
			select exists(
				select 1 from principal_grant g
				join principal pr on pr.id = g.principal_id
				where g.role_id = 'owner' and g.scope_kind = 'all' and pr.active
			)`).Scan(&ownerRemains); err != nil {
			return fmt.Errorf("storage: owner check: %w", err)
		}
		if !ownerRemains {
			return ErrLastOwner // rolls back the disable
		}
	}
	verb := "enable"
	if !active {
		verb = "disable"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "principal", id,
		map[string]any{"active": wasActive}, map[string]any{"active": active}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit set active: %w", err)
	}
	return nil
}

// mapGrantWriteErr translates the principal_grant constraint violations into the
// grant sentinels the API reports.
func mapGrantWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrGrantExists
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "principal_grant_role_id_fkey" {
				return ErrUnknownRole
			}
			return ErrPrincipalNotFound
		case "23514": // check_violation (scope_kind)
			return ErrBadScope
		case "22P02": // invalid uuid (principal id)
			return ErrPrincipalNotFound
		}
	}
	return fmt.Errorf("storage: create grant: %w", err)
}

// mapPrincipalWriteErr translates the human.username unique violation into
// ErrUsernameTaken (the API's 409); other errors pass through wrapped.
func mapPrincipalWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrUsernameTaken
	}
	return fmt.Errorf("storage: create human: %w", err)
}

// loadPrincipal fills a principal's kind profile (human or service) and its
// grants, given its id and kind already set.
func (p *PG) loadPrincipal(ctx context.Context, pr *Principal) error {
	switch pr.Kind {
	case "human":
		var h HumanProfile
		if err := p.pool.QueryRow(ctx,
			`select username, coalesce(email, ''), coalesce(display_name, '') from human where principal_id = $1`,
			pr.ID).Scan(&h.Username, &h.Email, &h.DisplayName); err != nil {
			return fmt.Errorf("storage: load human: %w", err)
		}
		pr.Human = &h
	case "service":
		var s ServiceProfile
		if err := p.pool.QueryRow(ctx,
			`select label from service where principal_id = $1`, pr.ID).Scan(&s.Label); err != nil {
			return fmt.Errorf("storage: load service: %w", err)
		}
		pr.Service = &s
	}

	rows, err := p.pool.Query(ctx,
		`select id, role_id, scope_kind, scope_id from principal_grant where principal_id = $1 order by created_at`, pr.ID)
	if err != nil {
		return fmt.Errorf("storage: load grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.ID, &g.Role, &g.ScopeKind, &g.ScopeID); err != nil {
			return fmt.Errorf("storage: scan grant: %w", err)
		}
		pr.Grants = append(pr.Grants, g)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: grants: %w", err)
	}
	return nil
}

// ListRoles returns every role, for building the in-process role index.
func (p *PG) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := p.pool.Query(ctx, `select id, official, permissions, inherits from role`)
	if err != nil {
		return nil, fmt.Errorf("storage: list roles: %w", err)
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Official, &r.Permissions, &r.Inherits); err != nil {
			return nil, fmt.Errorf("storage: scan role: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
