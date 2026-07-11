// Package storage is the Storage Gateway: the single seam over the relational
// backend. The interface exists from day one even though there is exactly one
// implementation, so the rest of the binary depends on the contract and never
// on pgx directly. Swapping or wrapping the backend later (scope injection,
// audit, read replicas) happens behind this interface without rippling into
// call sites.
//
// The walking skeleton's surface is intentionally tiny: open a pool, expose a
// health Ping, and close. Domain reads and writes land on this same interface
// in later slices.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Gateway is the only path to the database. Every component that needs durable
// state takes a Gateway, not a *pgxpool.Pool, so the seam stays the single
// chokepoint for cross-cutting concerns (scope, audit) as the system grows.
type Gateway interface {
	// Ping verifies the backend is reachable and responsive. The healthz
	// endpoint reports its result; a non-nil error means the database leg is
	// down even if the process is up.
	Ping(ctx context.Context) error
	// UpsertRole installs or updates an official role by id, the boot-seed
	// phase's write. Idempotent: re-seeding the same role is a no-op update.
	UpsertRole(ctx context.Context, r Role) error
	// BootstrapOwner creates the first owner (a human principal with a bearer
	// credential and an owner@all grant) directly, in one transaction, idempotent
	// per username. Returns whether a new owner was created (false if the
	// username already exists, so re-running mints no second credential).
	BootstrapOwner(ctx context.Context, spec OwnerSpec) (created bool, err error)
	// IssueBearerCredential mints an additional bearer credential for an existing
	// principal by human username (token reissue / break-glass / dev login).
	// Returns false if no such username.
	IssueBearerCredential(ctx context.Context, username string, hash []byte, prefix string, expiresAt *time.Time) (bool, error)
	// AuthenticateBearer resolves a bearer credential by its sha256 hash to the
	// principal, its kind profile, and its grants. Returns ErrCredentialNotFound
	// if no credential matches.
	AuthenticateBearer(ctx context.Context, hash []byte) (*Principal, error)
	// ResolvePrincipalRef maps a principal reference (a uuid or a human username) to
	// the principal id: a uuid passes through, a username is looked up, and an unknown
	// username is ErrPrincipalNotFound. Backs addressing a principal by username.
	ResolvePrincipalRef(ctx context.Context, ref string) (string, error)
	// The impersonation surface: an admin views/acts as another principal, audited
	// with the real actor. BeginImpersonation persists a session (the API enforces
	// the escalation guard first); AuthenticateImpersonation is the authn fallback
	// on a bearer miss, resolving the token to the target principal plus the real
	// actor, mode, and session id; EndImpersonation revokes a session.
	BeginImpersonation(ctx context.Context, realActorID, targetID, mode string, ttl time.Duration) (string, *ImpersonationSession, error)
	AuthenticateImpersonation(ctx context.Context, hash []byte) (pr *Principal, realActorID, mode, sessionID string, err error)
	EndImpersonation(ctx context.Context, sessionID string) error
	// AuthenticatePassword verifies a human's password against their argon2id
	// credential and resolves the principal. Returns ErrBadCredentials for an
	// unknown user, no password set, or a wrong password.
	AuthenticatePassword(ctx context.Context, username, password string) (*Principal, error)
	// SetPassword installs or replaces a human's password credential (the caller
	// passes the PHC-encoded argon2id hash). Returns false if no such username.
	SetPassword(ctx context.Context, username, encoded string) (bool, error)
	// UpdateHumanProfile applies a partial update to a human's own profile by
	// principal id (the authenticated session's own id): a nil patch field is left
	// unchanged, a provided empty string clears the nullable column.
	UpdateHumanProfile(ctx context.Context, principalID string, patch HumanProfilePatch) error
	// ListPrincipals returns every principal with its profile and grants (the admin
	// directory). Requires an all-scope read (a principal is not scope-tree scoped);
	// a non-all scope is ErrPrincipalForbidden. Archived principals are excluded
	// unless includeArchived is set. No credential secret is loaded.
	ListPrincipals(ctx context.Context, read scope.Set, includeArchived bool) ([]Principal, error)
	// GetPrincipal resolves one principal by id with its profile and grants.
	// Requires an all-scope read; an unknown id is ErrPrincipalNotFound.
	GetPrincipal(ctx context.Context, id string, read scope.Set) (*Principal, error)
	// CreateHumanPrincipal creates a human principal (and its password credential
	// when a hash is given) in one audited transaction. Requires an all-scope
	// create; a duplicate username is ErrUsernameTaken.
	CreateHumanPrincipal(ctx context.Context, actorID string, spec HumanSpec, create scope.Set) (*Principal, error)
	// UpdatePrincipalHuman applies an admin profile update (display name, email,
	// username) to a human principal by id, audited. Requires an all-scope grant; a
	// non-human target is ErrPrincipalNotHuman, an unknown id ErrPrincipalNotFound,
	// a username clash ErrUsernameTaken.
	UpdatePrincipalHuman(ctx context.Context, actorID, id string, patch AdminHumanPatch, action scope.Set) (*Principal, error)
	// CreateGrant assigns a role x scope to a principal, audited. Requires an
	// all-scope grant. Bad scope / unknown role / unknown principal / duplicate map
	// to ErrBadScope / ErrUnknownRole / ErrPrincipalNotFound / ErrGrantExists.
	CreateGrant(ctx context.Context, actorID, principalID string, spec GrantSpec, action scope.Set) (*Grant, error)
	// RevokeGrant deletes one grant from a principal, audited. Requires an all-scope
	// grant. Unknown grant is ErrGrantNotFound; revoking the last owner grant is
	// ErrLastOwner (the deferred owner-invariant trigger).
	RevokeGrant(ctx context.Context, actorID, principalID, grantID string, action scope.Set) error
	// Principal groups: a group holds role x scope grants its members inherit. All
	// group management is all-scope admin work (like the principal directory). A
	// member's effective grants are its direct grants unioned with its groups'
	// grants, resolved in the grant loader, so a group grant scopes and flattens
	// exactly like a direct one.
	CreateGroup(ctx context.Context, actorID string, spec GroupSpec, action scope.Set) (*Group, error)
	ListGroups(ctx context.Context, read scope.Set) ([]Group, error)
	GetGroup(ctx context.Context, id string, read scope.Set) (*Group, error)
	UpdateGroup(ctx context.Context, actorID, id string, patch GroupPatch, action scope.Set) (*Group, error)
	DeleteGroup(ctx context.Context, actorID, id string, action scope.Set) error
	AddGroupMember(ctx context.Context, actorID, groupID, principalID string, action scope.Set) error
	RemoveGroupMember(ctx context.Context, actorID, groupID, principalID string, action scope.Set) error
	ListGroupMembers(ctx context.Context, groupID string, read scope.Set) ([]GroupMember, error)
	ListGroupsForPrincipal(ctx context.Context, principalID string, read scope.Set) ([]Group, error)
	CreateGroupGrant(ctx context.Context, actorID, groupID string, spec GrantSpec, action scope.Set) (*Grant, error)
	RevokeGroupGrant(ctx context.Context, actorID, groupID, grantID string, action scope.Set) error
	ListGroupGrants(ctx context.Context, groupID string, read scope.Set) ([]Grant, error)
	// SetPrincipalActive enables or disables a principal (soft), audited. Requires
	// an all-scope grant. Disabling the last active owner is ErrLastOwner; a
	// disabled principal cannot authenticate.
	SetPrincipalActive(ctx context.Context, actorID, id string, active bool, action scope.Set) error
	// ArchivePrincipal soft-deletes a principal (hidden from the directory, cannot
	// authenticate, reversible); RestorePrincipal reverses it; PurgePrincipal
	// hard-deletes an archived one (ErrNotArchived if not archived first, the
	// hard gate). All require an all-scope grant; archiving the last active owner
	// is ErrLastOwner. Purge preserves the audit trail (denormalized actor label).
	ArchivePrincipal(ctx context.Context, actorID, id string, action scope.Set) error
	RestorePrincipal(ctx context.Context, actorID, id string, action scope.Set) error
	PurgePrincipal(ctx context.Context, actorID, id string, action scope.Set) error
	// SetPrincipalPassword resets a human principal's password by id (an admin action,
	// audited as the admin), requiring an all-scope grant. Unknown id is ErrPrincipalNotFound.
	// It also revokes every one of the target's bearer credentials (force logout).
	SetPrincipalPassword(ctx context.Context, actorID, id, encoded string, action scope.Set) error
	// SetOwnAvatar / ClearOwnAvatar write or clear the caller's own profile picture
	// (a normalized base64 JPEG), audited as the caller. No capability is required;
	// the caller resolves from their session.
	SetOwnAvatar(ctx context.Context, principalID, jpegB64 string) error
	ClearOwnAvatar(ctx context.Context, principalID string) error
	// SetPrincipalAvatar / ClearPrincipalAvatar write or clear another principal's
	// profile picture (an admin action, audited as the actor), requiring an all-scope
	// grant. An unknown id is ErrPrincipalNotFound; a non-all scope is ErrPrincipalForbidden.
	SetPrincipalAvatar(ctx context.Context, actorID, id, jpegB64 string, action scope.Set) error
	ClearPrincipalAvatar(ctx context.Context, actorID, id string, action scope.Set) error
	// GetHumanAvatar returns a human's profile picture as a base64 JPEG. The bool is
	// false (with no error) when the human has no picture; an unknown id is
	// ErrPrincipalNotFound. Unscoped: it backs the self read (the caller's own row).
	GetHumanAvatar(ctx context.Context, id string) (string, bool, error)
	// GetPrincipalAvatar is the admin read of another principal's profile picture: it
	// applies the same all-scope invariant as the rest of the directory (a non-all
	// scope is ErrPrincipalForbidden), then delegates to GetHumanAvatar.
	GetPrincipalAvatar(ctx context.Context, id string, action scope.Set) (string, bool, error)
	// RevokePrincipalBearers deletes a principal's bearer credentials (sessions and
	// tokens) except any sha256 hash in keep (empty revokes all); the force-logout on a
	// self-service password change keeps the caller's own session. Returns the count.
	RevokePrincipalBearers(ctx context.Context, principalID string, keep [][]byte) (int, error)
	// RevokeBearer deletes the bearer credential with the given sha256 hash
	// (session logout). A no-op if none matches.
	RevokeBearer(ctx context.Context, hash []byte) error
	// AnyHuman reports whether any human principal exists (drives the login
	// screen's bootstrap hint).
	AnyHuman(ctx context.Context) (bool, error)
	// ListRoles returns every role, for building the in-process role index.
	ListRoles(ctx context.Context) ([]Role, error)
	// ListAuditLog returns recent audit rows (newest first) for the audit read
	// surface, resolving actor and real-actor usernames.
	ListAuditLog(ctx context.Context, f AuditFilter) ([]AuditEntry, error)
	// WriteAuthEvent records an auth event (login, logout) in the audit trail, off
	// the read/no-tx auth paths.
	WriteAuthEvent(ctx context.Context, actorID, verb string) error
	// UpsertLocationType installs or updates an official location type by id, the
	// boot-seed phase's write. Idempotent.
	UpsertLocationType(ctx context.Context, lt LocationType) error
	// ListLocationTypes returns every location type, ranked.
	ListLocationTypes(ctx context.Context) ([]LocationType, error)

	// InScopeIDs reports which of the candidate row ids of a tree resource
	// (location/system/component) are inside a resolved scope, applying the same
	// subtree/exclude-root logic the enforcement uses. It backs per-row UI action
	// gating (one query per action scope answers a whole page).
	InScopeIDs(ctx context.Context, resource string, ids []string, set scope.Set) (map[string]bool, error)

	// The location CRUD surface. Every method takes the caller's resolved scope
	// (a required input, so no path queries unscoped), expands it to a row filter,
	// and writes the audit row in the mutating transaction. The read/action split
	// drives the non-disclosing 404 versus the 403.
	ListLocations(ctx context.Context, read scope.Set) ([]Location, error)
	GetLocation(ctx context.Context, name string, read scope.Set) (*Location, error)
	CreateLocation(ctx context.Context, actorID string, spec LocationSpec, create scope.Set) (*Location, error)
	UpdateLocation(ctx context.Context, actorID, name string, patch LocationPatch, read, action scope.Set) (*Location, error)
	DeleteLocation(ctx context.Context, actorID, name string, read, action scope.Set) error

	// The system tier: a type registry and scoped CRUD, mirroring locations.
	UpsertSystemType(ctx context.Context, st SystemType) error
	ListSystemTypes(ctx context.Context) ([]SystemType, error)
	ListSystems(ctx context.Context, read scope.Set) ([]System, error)
	GetSystem(ctx context.Context, name string, read scope.Set) (*System, error)
	CreateSystem(ctx context.Context, actorID string, spec SystemSpec, create scope.Set) (*System, error)
	UpdateSystem(ctx context.Context, actorID, name string, patch SystemPatch, read, action scope.Set) (*System, error)
	DeleteSystem(ctx context.Context, actorID, name string, read, action scope.Set) error

	// The component tier: a type registry and scoped CRUD, on the same helpers.
	UpsertComponentType(ctx context.Context, ct ComponentType) error
	ListComponentTypes(ctx context.Context) ([]ComponentType, error)
	ListComponents(ctx context.Context, read scope.Set) ([]Component, error)
	GetComponent(ctx context.Context, name string, read scope.Set) (*Component, error)
	CreateComponent(ctx context.Context, actorID string, spec ComponentSpec, create scope.Set) (*Component, error)
	UpdateComponent(ctx context.Context, actorID, name string, patch ComponentPatch, read, action scope.Set) (*Component, error)
	DeleteComponent(ctx context.Context, actorID, name string, read, action scope.Set) error

	// The secret tier: a shape registry, scoped CRUD, an audited reveal, and the
	// cascade resolver. A secret is owned on the exclusive arc (global or one of
	// the three trees) and encrypted at rest; ResolveSecrets is the per-component
	// effective-value view down the structural cascade.
	UpsertSecretType(ctx context.Context, st SecretType) error
	ListSecretTypes(ctx context.Context) ([]SecretType, error)
	GetSecretType(ctx context.Context, id string) (*SecretType, error)
	ListSecrets(ctx context.Context, read scope.Set) ([]Secret, error)
	CreateSecret(ctx context.Context, actorID string, spec SecretSpec, create scope.Set) (*Secret, error)
	UpdateSecret(ctx context.Context, actorID, id string, fields map[string]string, read, action scope.Set) (*Secret, error)
	DeleteSecret(ctx context.Context, actorID, id string, read, action scope.Set) error
	RevealSecret(ctx context.Context, actorID, id string, read, action scope.Set) (map[string]string, error)
	CopySecret(ctx context.Context, actorID, id string, read, action scope.Set) (map[string]string, error)
	ResolveSecrets(ctx context.Context, componentID string, read scope.Set) ([]ResolvedSecret, error)

	// Close releases the underlying connection pool. Idempotent at the pool
	// level; call once on shutdown.
	Close()
}

// PG is the Postgres-backed Gateway implementation over a pgx connection pool.
type PG struct {
	pool   *pgxpool.Pool
	secret secret.Provider
}

// Option configures a PG at construction. The secret provider is optional so
// the CLI lanes that never touch secrets (token, migrate) need not build a KEK;
// a secret write without a provider fails with ErrNoSecretProvider rather than a
// nil panic.
type Option func(*PG)

// WithSecretProvider installs the envelope-encryption provider the secret writes
// seal with. The server and the dev-seed lanes pass one; a nil provider leaves
// secret writes disabled.
func WithSecretProvider(prov secret.Provider) Option {
	return func(p *PG) { p.secret = prov }
}

// NewPG opens a pgx pool against dsn and verifies connectivity once before
// returning, so a bad DSN or an unreachable database fails fast at boot rather
// than on the first query.
func NewPG(ctx context.Context, dsn string, opts ...Option) (*PG, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	p := &PG{pool: pool}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// Ping checks backend reachability through the pool.
func (p *PG) Ping(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("storage: ping: %w", err)
	}
	return nil
}

// Close releases the pool.
func (p *PG) Close() { p.pool.Close() }
