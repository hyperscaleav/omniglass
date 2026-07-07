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
	IssueBearerCredential(ctx context.Context, username string, hash []byte, prefix string) (bool, error)
	// AuthenticateBearer resolves a bearer credential by its sha256 hash to the
	// principal, its kind profile, and its grants. Returns ErrCredentialNotFound
	// if no credential matches.
	AuthenticateBearer(ctx context.Context, hash []byte) (*Principal, error)
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
	// a non-all scope is ErrPrincipalForbidden. No credential secret is loaded.
	ListPrincipals(ctx context.Context, read scope.Set) ([]Principal, error)
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
	// SetPrincipalActive enables or disables a principal (soft), audited. Requires
	// an all-scope grant. Disabling the last active owner is ErrLastOwner; a
	// disabled principal cannot authenticate.
	SetPrincipalActive(ctx context.Context, actorID, id string, active bool, action scope.Set) error
	// RevokeBearer deletes the bearer credential with the given sha256 hash
	// (session logout). A no-op if none matches.
	RevokeBearer(ctx context.Context, hash []byte) error
	// AnyHuman reports whether any human principal exists (drives the login
	// screen's bootstrap hint).
	AnyHuman(ctx context.Context) (bool, error)
	// ListRoles returns every role, for building the in-process role index.
	ListRoles(ctx context.Context) ([]Role, error)
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

	// The collection registries: estate-wide reference data (no scope.Set),
	// seeded official and operator-extensible at org/template scope later.
	UpsertDatapointType(ctx context.Context, dt DatapointType) error
	ListDatapointTypes(ctx context.Context) ([]DatapointType, error)
	UpsertInterfaceType(ctx context.Context, it InterfaceType) error
	ListInterfaceTypes(ctx context.Context) ([]InterfaceType, error)

	// The observed-metric sink. reject-not-project is applied by the caller
	// (collection.Registry) before the write.
	InsertMetricDatapoints(ctx context.Context, evs []MetricDatapointEvent) error
	LatestMetric(ctx context.Context, componentName, key string) (*MetricDatapoint, error)

	// Close releases the underlying connection pool. Idempotent at the pool
	// level; call once on shutdown.
	Close()
}

// PG is the Postgres-backed Gateway implementation over a pgx connection pool.
type PG struct {
	pool *pgxpool.Pool
}

// NewPG opens a pgx pool against dsn and verifies connectivity once before
// returning, so a bad DSN or an unreachable database fails fast at boot rather
// than on the first query.
func NewPG(ctx context.Context, dsn string) (*PG, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	return &PG{pool: pool}, nil
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
