package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	DisplayName string // operator-facing name; empty falls back to the id
	Description string // what the role grants, for the Roles view and tooltips
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
		insert into role (id, official, permissions, inherits, display_name, description)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (id) do update
			set official     = excluded.official,
			    permissions  = excluded.permissions,
			    inherits     = excluded.inherits,
			    display_name = excluded.display_name,
			    description  = excluded.description`,
		r.ID, r.Official, r.Permissions, r.Inherits, nullize(r.DisplayName), nullize(r.Description))
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
// (token reissue, break-glass, and the `make dev` login). expiresAt, when non-nil,
// bounds the credential's lifetime (a session cookie set at login); a nil expiry
// never expires (an API token minted from the CLI).
func (p *PG) IssueBearerCredential(ctx context.Context, username string, hash []byte, prefix string, expiresAt *time.Time) (bool, error) {
	var pid string
	err := p.pool.QueryRow(ctx, `select principal_id from human where username = $1`, username).Scan(&pid)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("storage: lookup human %q: %w", username, err)
	}
	if _, err := p.pool.Exec(ctx,
		`insert into credential (principal_id, kind, secret_hash, prefix, expires_at) values ($1, 'bearer', $2, $3, $4)`,
		pid, hash, prefix, expiresAt); err != nil {
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
	// ArchivedAt marks a soft delete (the principal lifecycle, issue #143): a
	// non-nil value means the account is archived (hidden, cannot authenticate,
	// reversible until purged). Nil means live. Distinct from Active, which is the
	// reversible disable (suspend) toggle.
	ArchivedAt *time.Time
	Human         *HumanProfile
	Service       *ServiceProfile
	Grants        []Grant
	// Groups are the principal groups this principal belongs to (id + label), so the
	// admin directory can show membership without a per-row fetch. The grants those
	// groups confer already ride Grants (tagged with GroupID); this names them.
	Groups []PrincipalGroupRef
}

// PrincipalGroupRef is a lightweight reference to a group a principal belongs to.
type PrincipalGroupRef struct{ ID, Name string }

// HumanProfile and ServiceProfile carry the kind-specific attributes.
type HumanProfile struct {
	Username, Email, DisplayName string
	// MustChangePassword is set by an admin reset and cleared by the user's own
	// change-password; while true the account is gated to the change-password lane.
	MustChangePassword bool
}
type ServiceProfile struct{ Label string }

// Grant is one (role x scope) pairing on a principal, addressable by its id (so
// the admin surface can revoke a specific one).
type Grant struct {
	ID        string
	Role      string
	ScopeKind string
	ScopeID   *string
	ScopeOp   string // how ScopeID matches the tree: subtree, subtree_excl_root, or self
	// GroupID is set when this grant is inherited from a group the principal
	// belongs to (nil for a direct grant), so a caller can show inherited access
	// distinctly from direct. A principal's effective grants are its direct grants
	// unioned with its groups' grants; both flatten and scope-resolve the same way.
	GroupID *string
	// GroupName is the source group's label (display name or name), set alongside
	// GroupID, so a caller can name where an inherited grant comes from.
	GroupName *string
}

// ErrBadCredentials is returned by AuthenticatePassword when the username is
// unknown, has no password set, or the password does not match: one error for all
// three so the handler cannot leak which.
var ErrBadCredentials = errors.New("storage: bad credentials")

// ErrAccountDisabled is returned by AuthenticatePassword when the password is
// CORRECT but the principal is disabled. It is a success-of-password signal, not a
// peer of ErrBadCredentials: it is reachable only after the password verifies, so
// a wrong password against a disabled account is still ErrBadCredentials and the
// endpoint cannot be used to enumerate accounts or their state.
var ErrAccountDisabled = errors.New("storage: account disabled")

// ErrAccountLocked is returned by AuthenticatePassword when the account is inside
// its brute-force lockout window, whatever the password. It is decided after the
// argon2 verify (like ErrAccountDisabled) so a locked account is not measurably
// faster to probe, and the handler maps it to the same generic 401 as a bad
// credential so the lock is not an enumeration oracle (only the audit records it).
var ErrAccountLocked = errors.New("storage: account locked")

// dummyPasswordHash is a throwaway argon2id hash the unknown-user / no-password
// path verifies against, so AuthenticatePassword runs one argon2 derivation
// whether or not the user exists (no early return before the compare). It is
// generated via auth.HashPassword so it carries the real argon parameters by
// construction: a hand-written literal with weaker params would make the miss path
// measurably faster and reopen a timing oracle. argon2 dominates the response
// time; the pre-existing delta of a single credential-row fetch between a hit and
// a miss is unchanged (this is not claimed to be perfectly constant-time).
var dummyPasswordHash = mustDummyHash()

func mustDummyHash() string {
	h, err := auth.HashPassword("og-dummy-password-not-a-secret")
	if err != nil {
		panic(fmt.Sprintf("storage: init dummy password hash: %v", err))
	}
	return h
}

// AuthenticateBearer resolves a bearer credential by its sha256 hash to the
// principal, its kind profile, and its grants. ErrCredentialNotFound if none, which
// includes a credential whose expiry has passed (an expired session is treated as
// absent, so it authenticates nothing).
func (p *PG) AuthenticateBearer(ctx context.Context, hash []byte) (*Principal, error) {
	var pr Principal
	err := p.pool.QueryRow(ctx, `
		select pr.id, pr.kind
		from credential c
		join principal pr on pr.id = c.principal_id
		where c.kind = 'bearer' and c.secret_hash = $1 and pr.active
			and (c.expires_at is null or c.expires_at > now())`, hash).Scan(&pr.ID, &pr.Kind)
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
	var active bool
	var failedCount int
	var lockedUntil *time.Time
	// Do NOT filter on pr.active here: the row must be fetched for a disabled user
	// so their real hash is compared, and the active flag must not steer control
	// flow before the password compare (that would leak account state by timing).
	err := p.pool.QueryRow(ctx, `
		select h.principal_id, c.secret_hash, pr.active, h.failed_login_count, h.locked_until
		from human h
		join principal pr on pr.id = h.principal_id
		join credential c on c.principal_id = h.principal_id and c.kind = 'password'
		where h.username = $1`, username).Scan(&pr.ID, &encoded, &active, &failedCount, &lockedUntil)
	found := true
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Unknown user or no password credential: verify against the dummy hash so
		// this path does the same argon2 work as a real user, then fail generically.
		found = false
		encoded = []byte(dummyPasswordHash)
	case err != nil:
		return nil, fmt.Errorf("storage: authenticate password: %w", err)
	}
	// Always verify; branch on found/active only AFTER, so response time does not
	// reveal whether the user exists or is disabled.
	ok, err := auth.VerifyPassword(password, string(encoded))
	if err != nil {
		return nil, fmt.Errorf("storage: verify password: %w", err)
	}
	if !found {
		// Unknown user (or no password credential): nothing to attribute, so the
		// handler must not audit it (a per-attempt row for any random username would
		// let an unauthenticated caller flood the audit log). Client sees the same
		// generic error, so this discloses nothing.
		return nil, ErrBadCredentials
	}
	now := time.Now()
	if isLocked(lockedUntil, now) {
		// Inside the lockout window: refuse whatever the password is (a correct one
		// is still refused until the window passes), decided after the verify so it is
		// not a faster path. Do not extend the lock or bump the counter here; just
		// deny. The handler audits it and returns the same generic 401 as a miss.
		return &Principal{ID: pr.ID, Kind: pr.Kind}, ErrAccountLocked
	}
	if !ok {
		// A real account, wrong password: bump the consecutive-failure counter and,
		// on the threshold-th miss, lock the account. Best effort (the attempt has
		// already failed; a counter write error must not change the response). Return
		// the principal id (server-side only, the client error is unchanged) so the
		// handler can audit the failed attempt, a security signal worth recording.
		d := nextLockout(failedCount, now)
		_, _ = p.pool.Exec(ctx,
			`update human set failed_login_count = $2, locked_until = $3 where principal_id = $1`,
			pr.ID, d.count, d.lockedUntil)
		return &Principal{ID: pr.ID, Kind: pr.Kind}, ErrBadCredentials
	}
	// Correct password: clear any accumulated failures (best effort) so a run of
	// wrong guesses that ended in the right password does not carry toward a lock.
	if failedCount != 0 || lockedUntil != nil {
		_, _ = p.pool.Exec(ctx,
			`update human set failed_login_count = 0, locked_until = null where principal_id = $1`, pr.ID)
	}
	if !active {
		// Correct password against a disabled account: a distinct, disclosable
		// signal, reachable only with the right password (not an enumeration oracle).
		return &Principal{ID: pr.ID, Kind: pr.Kind}, ErrAccountDisabled
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
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("storage: begin set password: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		insert into credential (principal_id, kind, secret_hash, prefix)
		values ($1, 'password', $2, '')
		on conflict (principal_id) where kind = 'password'
			do update set secret_hash = excluded.secret_hash`,
		pid, []byte(encoded)); err != nil {
		return false, fmt.Errorf("storage: set password: %w", err)
	}
	// A user setting their own password clears any force-change flag: this is the
	// lane an admin reset gates the account into, and completing it releases the gate.
	if _, err := tx.Exec(ctx, `update human set must_change_password = false where principal_id = $1`, pid); err != nil {
		return false, fmt.Errorf("storage: clear must-change: %w", err)
	}
	// Audit the credential change (never the secret) so a password change leaves a
	// trail and an impersonated change records the real actor.
	if err := writeAuditRes(ctx, tx, pid, "change_password", "credential", pid, nil, nil); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("storage: commit set password: %w", err)
	}
	return true, nil
}

// SetPrincipalPassword resets a human principal's password by id, on behalf of an
// admin. Requires an all-scope grant (principals are not scope-tree scoped). Unlike
// SetPassword (self-service, keyed on username, audited as the target), this audits
// the acting admin as the actor and does not require the target's current password.
// The target must be a human; an unknown id is ErrPrincipalNotFound. A reset also
// revokes every one of the target's bearer credentials (all sessions and tokens) in
// the same transaction, so the reset takes effect immediately: any live session is
// signed out and must re-authenticate with the new password.
func (p *PG) SetPrincipalPassword(ctx context.Context, actorID, id, encoded string, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin reset password: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Only a human holds a password credential; an unknown id (or a service) is not found.
	var pgErr *pgconn.PgError
	if err := tx.QueryRow(ctx, `select 1 from human where principal_id = $1`, id).Scan(new(int)); err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrPrincipalNotFound
		case errors.As(err, &pgErr) && pgErr.Code == "22P02":
			return ErrPrincipalNotFound
		default:
			return fmt.Errorf("storage: reset password lookup: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		insert into credential (principal_id, kind, secret_hash, prefix)
		values ($1, 'password', $2, '')
		on conflict (principal_id) where kind = 'password'
			do update set secret_hash = excluded.secret_hash`,
		id, []byte(encoded)); err != nil {
		return fmt.Errorf("storage: reset password: %w", err)
	}
	// Force logout: drop every bearer credential (sessions and tokens) for the target,
	// so a reset immediately invalidates any live access.
	if _, err := tx.Exec(ctx, `delete from credential where principal_id = $1 and kind = 'bearer'`, id); err != nil {
		return fmt.Errorf("storage: revoke sessions on reset: %w", err)
	}
	// Force a change on next login: the admin knows the value they just set, so the
	// target must replace it before doing anything else (cleared by their own change).
	if _, err := tx.Exec(ctx, `update human set must_change_password = true where principal_id = $1`, id); err != nil {
		return fmt.Errorf("storage: flag must-change on reset: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "reset_password", "credential", id, nil, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit reset password: %w", err)
	}
	return nil
}

// RevokePrincipalBearers deletes every bearer credential (sessions and tokens) for a
// principal, except any whose sha256 hash is in keep. It backs the force-logout on a
// self-service password change: pass the caller's current session hash in keep so the
// change signs out their OTHER sessions and tokens but not the one making the request.
// An empty keep revokes them all. Returns the number revoked.
func (p *PG) RevokePrincipalBearers(ctx context.Context, principalID string, keep [][]byte) (int, error) {
	var tag pgconn.CommandTag
	var err error
	if len(keep) == 0 {
		tag, err = p.pool.Exec(ctx, `delete from credential where principal_id = $1 and kind = 'bearer'`, principalID)
	} else {
		tag, err = p.pool.Exec(ctx,
			`delete from credential where principal_id = $1 and kind = 'bearer' and secret_hash <> all($2::bytea[])`,
			principalID, keep)
	}
	if err != nil {
		return 0, fmt.Errorf("storage: revoke bearers: %w", err)
	}
	return int(tag.RowsAffected()), nil
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
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin update human profile: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		update human set
			display_name = case when $2 then $3 else display_name end,
			email        = case when $4 then $5 else email end
		where principal_id = $1`,
		principalID, setDisplay, display, setEmail, email); err != nil {
		return fmt.Errorf("storage: update human profile: %w", err)
	}
	// Audit the self-profile change so an impersonated (act-as) edit records the
	// real actor, and every profile edit leaves a trail. Log the changed fields.
	summary := map[string]any{}
	if patch.DisplayName != nil {
		summary["display_name"] = *patch.DisplayName
	}
	if patch.Email != nil {
		summary["email"] = *patch.Email
	}
	if err := writeAuditRes(ctx, tx, principalID, "update", "principal", principalID, nil, summary); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit update human profile: %w", err)
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

// ErrNotArchived is returned by PurgePrincipal when the target has not been
// archived first (the hard gate between the soft and hard delete).
var ErrNotArchived = errors.New("storage: principal must be archived before purge")

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
// list. Archived (soft-deleted) principals are excluded unless
// includeArchived is set (the directory's "show archived" view, so a hidden
// account can be restored or purged). Credentials are never loaded.
func (p *PG) ListPrincipals(ctx context.Context, read scope.Set, includeArchived bool) ([]Principal, error) {
	if !read.All {
		return nil, ErrPrincipalForbidden
	}
	rows, err := p.pool.Query(ctx, `select id, kind, active, archived_at from principal where ($1 or archived_at is null) order by created_at`, includeArchived)
	if err != nil {
		return nil, fmt.Errorf("storage: list principals: %w", err)
	}
	type base struct {
		id, kind   string
		active     bool
		archivedAt *time.Time
	}
	var bases []base
	for rows.Next() {
		var b base
		if err := rows.Scan(&b.id, &b.kind, &b.active, &b.archivedAt); err != nil {
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
		pr := Principal{ID: b.id, Kind: b.kind, Active: b.active, ArchivedAt: b.archivedAt}
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
	err := p.pool.QueryRow(ctx, `select id, kind, active, archived_at from principal where id = $1`, id).Scan(&pr.ID, &pr.Kind, &pr.Active, &pr.ArchivedAt)
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
	ScopeOp   string // subtree (default), subtree_excl_root, or self; empty means subtree
}

// CreateGrant assigns a role x scope to a principal, audited. Requires an
// all-scope grant. A non-all scope with no scope id is ErrBadScope; an unknown
// role ErrUnknownRole; an unknown principal ErrPrincipalNotFound; a duplicate
// ErrGrantExists.
func (p *PG) CreateGrant(ctx context.Context, actorID, principalID string, spec GrantSpec, action scope.Set) (*Grant, error) {
	if !action.All {
		return nil, ErrPrincipalForbidden
	}
	if spec.ScopeOp == "" {
		spec.ScopeOp = scope.OpSubtree // the default: root + descendants
	}
	if spec.ScopeOp != scope.OpSubtree && spec.ScopeOp != scope.OpSubtreeExclRoot && spec.ScopeOp != scope.OpSelf {
		return nil, ErrBadScope // an unknown operator
	}
	if spec.ScopeKind == "all" {
		spec.ScopeID = ""              // the all scope has no id
		spec.ScopeOp = scope.OpSubtree // and no root to narrow: the operator is moot
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
		`insert into principal_grant (principal_id, role_id, scope_kind, scope_id, scope_op) values ($1, $2, $3, $4, $5) returning id`,
		principalID, spec.Role, spec.ScopeKind, nullize(spec.ScopeID), spec.ScopeOp).Scan(&gid)
	if err != nil {
		return nil, mapGrantWriteErr(err)
	}
	g := Grant{ID: gid, Role: spec.Role, ScopeKind: spec.ScopeKind, ScopeOp: spec.ScopeOp}
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
		`select id, role_id, scope_kind, scope_id, scope_op from principal_grant where id = $1 and principal_id = $2`,
		grantID, principalID).Scan(&g.ID, &g.Role, &g.ScopeKind, &g.ScopeID, &g.ScopeOp)
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
		remains, err := activeOwnerRemains(ctx, tx)
		if err != nil {
			return fmt.Errorf("storage: owner check: %w", err)
		}
		if !remains {
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

// activeOwnerRemains reports whether at least one active owner@all grant still
// exists within the transaction: the invariant that guards disabling or
// archiving the last owner (mirroring the deferred grant trigger, but for the
// principal's active flag rather than the grant's existence).
func activeOwnerRemains(ctx context.Context, tx pgx.Tx) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `
		select exists(
			select 1 from principal_grant g
			join principal pr on pr.id = g.principal_id
			where g.role_id = 'owner' and g.scope_kind = 'all' and pr.active
		)`).Scan(&ok)
	return ok, err
}

// ArchivePrincipal soft-deletes a principal (the lifecycle, issue #143): it sets
// archived_at and forces the account inactive, so it is hidden from the
// directory and cannot authenticate, reversibly (RestorePrincipal restores it)
// until PurgePrincipal hard-deletes it. Requires an all-scope grant; archiving
// the last active owner is refused (ErrLastOwner).
func (p *PG) ArchivePrincipal(ctx context.Context, actorID, id string, action scope.Set) error {
	return p.setArchived(ctx, actorID, id, true, action)
}

// RestorePrincipal reverses an archive: it clears archived_at and
// restores the active flag, so the account is live and can authenticate again.
// Requires an all-scope grant.
func (p *PG) RestorePrincipal(ctx context.Context, actorID, id string, action scope.Set) error {
	return p.setArchived(ctx, actorID, id, false, action)
}

func (p *PG) setArchived(ctx context.Context, actorID, id string, archive bool, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin archive: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pgErr *pgconn.PgError
	if err := tx.QueryRow(ctx, `select 1 from principal where id = $1`, id).Scan(new(int)); err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrPrincipalNotFound
		case errors.As(err, &pgErr) && pgErr.Code == "22P02":
			return ErrPrincipalNotFound
		default:
			return fmt.Errorf("storage: archive lookup: %w", err)
		}
	}

	if archive {
		if _, err := tx.Exec(ctx, `update principal set archived_at = now(), active = false where id = $1`, id); err != nil {
			return fmt.Errorf("storage: archive: %w", err)
		}
		remains, err := activeOwnerRemains(ctx, tx)
		if err != nil {
			return fmt.Errorf("storage: owner check: %w", err)
		}
		if !remains {
			return ErrLastOwner // rolls back the archive
		}
	} else if _, err := tx.Exec(ctx, `update principal set archived_at = null, active = true where id = $1`, id); err != nil {
		return fmt.Errorf("storage: restore: %w", err)
	}

	verb := "restore"
	if archive {
		verb = "archive"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "principal", id, nil, map[string]any{"archived": archive}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit archive: %w", err)
	}
	return nil
}

// PurgePrincipal hard-deletes a principal (issue #143), gated on prior archival
// (ErrNotArchived otherwise, the hard gate between the soft and hard delete).
// The delete cascades the principal's owned rows (profile, credentials, grants,
// group memberships, impersonation sessions); the audit trail survives, because the
// audit foreign keys are ON DELETE SET NULL and every row keeps a denormalized
// actor label. Requires an all-scope grant; refused (ErrLastOwner) if it would
// remove the last owner grant.
func (p *PG) PurgePrincipal(ctx context.Context, actorID, id string, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin purge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var archivedAt *time.Time
	var pgErr *pgconn.PgError
	if err := tx.QueryRow(ctx, `select archived_at from principal where id = $1`, id).Scan(&archivedAt); err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrPrincipalNotFound
		case errors.As(err, &pgErr) && pgErr.Code == "22P02":
			return ErrPrincipalNotFound
		default:
			return fmt.Errorf("storage: purge lookup: %w", err)
		}
	}
	if archivedAt == nil {
		return ErrNotArchived
	}
	// Audit the purge before the row is gone; the actor is the purger (unaffected by
	// the delete), and the resource id is a plain text value, not a foreign key.
	if err := writeAuditRes(ctx, tx, actorID, "purge", "principal", id, nil, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from principal where id = $1`, id); err != nil {
		return fmt.Errorf("storage: purge principal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		// The deferred owner trigger raises OG001 at COMMIT if the cascade removed
		// the last owner grant.
		if errors.As(err, &pgErr) && pgErr.Code == "OG001" {
			return ErrLastOwner
		}
		return fmt.Errorf("storage: commit purge: %w", err)
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
			`select username, coalesce(email, ''), coalesce(display_name, ''), must_change_password from human where principal_id = $1`,
			pr.ID).Scan(&h.Username, &h.Email, &h.DisplayName, &h.MustChangePassword); err != nil {
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

	// A principal's effective grants are its direct grants unioned with the grants
	// of every group it belongs to. This is the one seam group access rides: the
	// permission flatten (roleIDs) and the gateway's per-action scope resolution
	// both read pr.Grants, so a member inherits a group's role and scope here and
	// nowhere else. group_id tags an inherited grant so callers can tell it apart.
	rows, err := p.pool.Query(ctx,
		`select g.id, g.role_id, g.scope_kind, g.scope_id, g.scope_op, g.group_id, coalesce(pg.display_name, pg.name)
		   from principal_grant g
		   left join principal_group pg on pg.id = g.group_id
		  where g.principal_id = $1
		     or g.group_id in (select group_id from principal_group_member where principal_id = $1)
		  order by g.group_id nulls first, g.created_at`, pr.ID)
	if err != nil {
		return fmt.Errorf("storage: load grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.ID, &g.Role, &g.ScopeKind, &g.ScopeID, &g.ScopeOp, &g.GroupID, &g.GroupName); err != nil {
			return fmt.Errorf("storage: scan grant: %w", err)
		}
		pr.Grants = append(pr.Grants, g)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: grants: %w", err)
	}

	// The principal's group memberships (id + label), so the admin directory can
	// show where a principal's inherited access comes from.
	grows, err := p.pool.Query(ctx,
		`select g.id, coalesce(g.display_name, g.name)
		   from principal_group_member m join principal_group g on g.id = m.group_id
		  where m.principal_id = $1 order by g.name`, pr.ID)
	if err != nil {
		return fmt.Errorf("storage: load groups: %w", err)
	}
	defer grows.Close()
	for grows.Next() {
		var ref PrincipalGroupRef
		if err := grows.Scan(&ref.ID, &ref.Name); err != nil {
			return fmt.Errorf("storage: scan group ref: %w", err)
		}
		pr.Groups = append(pr.Groups, ref)
	}
	return grows.Err()
}

// ListRoles returns every role, for building the in-process role index.
func (p *PG) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := p.pool.Query(ctx, `select id, official, permissions, inherits, coalesce(display_name, ''), coalesce(description, '') from role order by id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list roles: %w", err)
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Official, &r.Permissions, &r.Inherits, &r.DisplayName, &r.Description); err != nil {
			return nil, fmt.Errorf("storage: scan role: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
