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
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/blob"
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
	// IssueBearerCredential mints an additional bearer credential (a web-login session
	// or a CLI/API token) for an existing principal, addressed by human username, from a
	// BearerIssue spec: the secret hash and prefix, the purpose, the expiry, and the
	// identifying metadata (a token's description, and the user-agent / client-ip that
	// created it). Returns false if no such username.
	IssueBearerCredential(ctx context.Context, spec BearerIssue) (bool, error)
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
	// RevokeBearer deletes the bearer credential with the given sha256 hash
	// (session logout). A no-op if none matches.
	RevokeBearer(ctx context.Context, hash []byte) error
	// ListBearerCredentials returns a principal's own bearer credentials (login
	// sessions and API tokens) with their non-secret metadata (id, prefix,
	// created_at, expires_at), newest first; the secret hash is never returned.
	// currentHash (the sha256 of the request's own token) is compared only in SQL to
	// flag the current credential. It backs a signed-in user viewing their sessions.
	ListBearerCredentials(ctx context.Context, principalID string, currentHash []byte) ([]BearerCredential, error)
	// RevokeBearerByID deletes one bearer credential by id, scoped to the owning
	// principal (so a caller revokes only their own). Returns false when nothing
	// matched (wrong/already-revoked id, another principal's credential, or a
	// malformed id).
	RevokeBearerByID(ctx context.Context, principalID, credentialID string) (bool, error)
	// RevokeBearersByPurpose deletes every one of a principal's bearer credentials of
	// the given purpose ('session' or 'token'), scoped to the owning principal, and
	// returns the count deleted. It backs the admin "revoke all sessions" / "revoke
	// all tokens" blade actions: end all of one kind at once without touching the other.
	RevokeBearersByPurpose(ctx context.Context, principalID, purpose string) (int, error)
	// RevokeBearersByPurposeExcept is RevokeBearersByPurpose keeping any credential
	// whose sha256 secret hash is in keep (empty keep is the plain bulk revoke). It backs
	// "revoke my OTHER sessions" on a password change and the self-service "revoke all
	// sessions" that keeps the session the caller is on, tokens untouched.
	RevokeBearersByPurposeExcept(ctx context.Context, principalID, purpose string, keep [][]byte) (int, error)
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
	// ListLocationTypes returns every location type, alphabetically by display_name.
	ListLocationTypes(ctx context.Context) ([]LocationType, error)
	// The location_type registry CRUD (capability-only, unscoped). Create writes a
	// custom (official=false) row; update/delete refuse official rows and delete
	// refuses a row still referenced by a location.
	CreateLocationType(ctx context.Context, actorID string, lt LocationType) (*LocationType, error)
	UpdateLocationType(ctx context.Context, actorID, id string, patch LocationTypePatch) (*LocationType, error)
	DeleteLocationType(ctx context.Context, actorID, id string) error

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
	LocationNameTaken(ctx context.Context, name string) (bool, error)
	DeleteLocation(ctx context.Context, actorID, name string, read, action scope.Set) error

	// The system tier: a type registry and scoped CRUD, mirroring locations.
	UpsertSystemType(ctx context.Context, st SystemType) error
	ListSystemTypes(ctx context.Context) ([]SystemType, error)
	CreateSystemType(ctx context.Context, actorID string, st SystemType) (*SystemType, error)
	UpdateSystemType(ctx context.Context, actorID, id string, patch SystemTypePatch) (*SystemType, error)
	DeleteSystemType(ctx context.Context, actorID, id string) error
	ListSystems(ctx context.Context, read scope.Set) ([]System, error)
	GetSystem(ctx context.Context, name string, read scope.Set) (*System, error)
	CreateSystem(ctx context.Context, actorID string, spec SystemSpec, create scope.Set) (*System, error)
	UpdateSystem(ctx context.Context, actorID, name string, patch SystemPatch, read, action scope.Set) (*System, error)
	SystemNameTaken(ctx context.Context, name string) (bool, error)
	DeleteSystem(ctx context.Context, actorID, name string, read, action scope.Set) error

	// The component tier: a type registry and scoped CRUD, on the same helpers.
	UpsertComponentType(ctx context.Context, ct ComponentType) error
	ListComponentTypes(ctx context.Context) ([]ComponentType, error)
	CreateComponentType(ctx context.Context, actorID string, ct ComponentType) (*ComponentType, error)
	UpdateComponentType(ctx context.Context, actorID, id string, patch ComponentTypePatch) (*ComponentType, error)
	DeleteComponentType(ctx context.Context, actorID, id string) error
	ListComponents(ctx context.Context, read scope.Set) ([]Component, error)
	GetComponent(ctx context.Context, name string, read scope.Set) (*Component, error)
	// ListComponentInterfaces returns a component's interfaces (the reachability
	// panel's rows). Not scope-injected: the caller gates on the component being
	// in read scope first, then reads its interfaces by the verified name.
	ListComponentInterfaces(ctx context.Context, componentName string) ([]ComponentInterface, error)
	CreateComponent(ctx context.Context, actorID string, spec ComponentSpec, create scope.Set) (*Component, error)
	UpdateComponent(ctx context.Context, actorID, name string, patch ComponentPatch, read, action scope.Set) (*Component, error)
	ComponentNameTaken(ctx context.Context, name string) (bool, error)
	DeleteComponent(ctx context.Context, actorID, name string, read, action scope.Set) error

	// The component_make registry: a flat manufacturer registry (Cisco,
	// Crestron, ...), same shape and official-read-only guard as the type
	// registries above but with no tree and no in-use delete guard in this
	// slice (component_model will reference it later).
	UpsertComponentMake(ctx context.Context, m ComponentMake) error
	ListComponentMakes(ctx context.Context) ([]ComponentMake, error)
	GetComponentMake(ctx context.Context, id string) (*ComponentMake, error)
	CreateComponentMake(ctx context.Context, actorID string, m ComponentMake) (*ComponentMake, error)
	UpdateComponentMake(ctx context.Context, actorID, id string, patch ComponentMakePatch) (*ComponentMake, error)
	DeleteComponentMake(ctx context.Context, actorID, id string) error

	// The interface tier: operator CRUD over placement-bound connections. An
	// interface is not a scope-tree entity of its own; it hangs off a component
	// (interface.component), so every method's scope cascades THROUGH that component
	// (a component-tier scope.Set, from applicableKinds). A component-less
	// (server-hosted) interface is reachable only under an all scope. The read/action
	// split drives the non-disclosing 404 versus 403.
	ListInterfaces(ctx context.Context, read scope.Set) ([]Interface, error)
	GetInterface(ctx context.Context, id string, read scope.Set) (*Interface, error)
	CreateInterface(ctx context.Context, actorID string, spec InterfaceSpec, create scope.Set) (*Interface, error)
	UpdateInterface(ctx context.Context, actorID, id string, patch InterfacePatch, read, action scope.Set) (*Interface, error)
	DeleteInterface(ctx context.Context, actorID, id string, read, action scope.Set) error

	// The task tier: read-only over DERIVED collection work. A task is not
	// operator-authored: it is derived when an interface is created (the node's
	// unit of work over that connection) and carries no node column (its placement
	// projects from the interface). Reads cascade through the interface's owning
	// component, the same component-tier cascade as the interface itself.
	ListTasks(ctx context.Context, read scope.Set) ([]Task, error)
	GetTask(ctx context.Context, id string, read scope.Set) (*Task, error)

	// The collection registries: estate-wide reference data (no scope.Set),
	// seeded official and operator-extensible at org/template scope later.
	UpsertKey(ctx context.Context, k Key) error
	ListKeys(ctx context.Context) ([]Key, error)
	UpsertInterfaceType(ctx context.Context, it InterfaceType) error
	ListInterfaceTypes(ctx context.Context) ([]InterfaceType, error)

	// The observed-metric sink. reject-not-project is applied by the caller
	// (collection.Registry) before the write.
	InsertMetricDatapoints(ctx context.Context, evs []MetricDatapointEvent) error
	LatestMetric(ctx context.Context, componentName, key string) (*MetricDatapoint, error)
	// LatestMetricInstance is the instance-scoped LatestMetric: the reachability
	// layer signals are per-interface, so each interface resolves its own latest.
	LatestMetricInstance(ctx context.Context, componentName, key, instance string) (*MetricDatapoint, error)

	// The observed-state sink: the mirror of the metric sink for categorical
	// verdicts (interface.reachable). reject-not-project and the transition-only
	// guard are applied by the caller before the write. LatestState backs the
	// ingest-side transition guard; StateTransitions is the ordered flip series
	// the availability strip reads.
	InsertStateDatapoints(ctx context.Context, evs []StateDatapointEvent) error
	LatestState(ctx context.Context, componentName, key, instance string) (*StateDatapoint, error)
	StateTransitions(ctx context.Context, componentName, key, instance string, since time.Time) ([]StateDatapoint, error)

	// The node tier: the edge runtime's enrollment lifecycle and worklist. A node
	// is estate-wide (all-scope create/enroll/read, like a principal). The claim,
	// authenticate, heartbeat, and worklist paths are the node's own lane (gated by
	// the enrollment token or the node's NATS subject grant, not RBAC scope).
	CreateNode(ctx context.Context, actorID string, spec NodeSpec, create scope.Set) (*Node, error)
	UpdateNode(ctx context.Context, actorID, name string, patch NodePatch, read, action scope.Set) (*Node, error)
	DeleteNode(ctx context.Context, actorID, name string, read, action scope.Set) error
	SetEnrollmentToken(ctx context.Context, actorID, name, tokenHashHex string, action scope.Set) (*Node, error)
	ClaimNode(ctx context.Context, name, tokenHashHex string) (*Node, error)
	AuthenticateNode(ctx context.Context, name, tokenHashHex string) (bool, error)
	RecordHeartbeat(ctx context.Context, name string) error
	NodeWorklist(ctx context.Context, name string) (Worklist, error)
	// ResolveTaskOwner binds a task's owner component and confines the node to its
	// own tasks, for the telemetry ingest consumer.
	ResolveTaskOwner(ctx context.Context, taskID, nodeName string) (TaskOwner, bool, error)
	GetNode(ctx context.Context, name string, read scope.Set) (*Node, error)
	ListNodes(ctx context.Context, read scope.Set) ([]Node, error)

	// The secret tier: a shape registry, scoped CRUD, an audited reveal, and the
	// cascade resolver. A secret is owned on the exclusive arc (global or one of
	// the three trees) and encrypted at rest; ResolveSecrets is the per-component
	// effective-value view down the structural cascade.
	UpsertSecretType(ctx context.Context, st SecretType) error
	ListSecretTypes(ctx context.Context) ([]SecretType, error)
	GetSecretType(ctx context.Context, id string) (*SecretType, error)
	// canAdmin reports whether the caller holds the secret action at the :admin
	// tier (e.g. secret:reveal:admin), which admin/owner reach via secret:> / >.
	// It gates admin-sensitive secrets: those are hidden from a lister/resolver
	// without it, refused to a revealer/updater/deleter without it (non-disclosing),
	// and cannot be created by a caller without it. Placement scope still fences
	// where; canAdmin fences the sensitivity tier.
	ListSecrets(ctx context.Context, read scope.Set, canAdmin bool) ([]Secret, error)
	CreateSecret(ctx context.Context, actorID string, spec SecretSpec, create scope.Set, canAdmin bool) (*Secret, error)
	UpdateSecret(ctx context.Context, actorID, id string, fields map[string]string, read, action scope.Set, canAdmin bool) (*Secret, error)
	DeleteSecret(ctx context.Context, actorID, id string, read, action scope.Set, canAdmin bool) error
	RevealSecret(ctx context.Context, actorID, id string, read, action scope.Set, canAdmin bool) (map[string]string, error)
	CopySecret(ctx context.Context, actorID, id string, read, action scope.Set, canAdmin bool) (map[string]string, error)
	ResolveSecrets(ctx context.Context, componentID string, read scope.Set, canAdmin bool) ([]ResolvedSecret, error)

	// The variable tier: a typed, cascade-resolved plaintext value (a macro),
	// owned on the same exclusive arc as a secret but shown in the clear (no
	// registry, no crypto, no reveal). ResolveVariables is the per-component
	// effective-value view down the structural cascade.
	ListVariables(ctx context.Context, read scope.Set) ([]Variable, error)
	CreateVariable(ctx context.Context, actorID string, spec VariableSpec, create scope.Set) (*Variable, error)
	UpdateVariable(ctx context.Context, actorID, id string, value json.RawMessage, read, action scope.Set) (*Variable, error)
	DeleteVariable(ctx context.Context, actorID, id string, read, action scope.Set) error
	ResolveVariables(ctx context.Context, componentID string, read scope.Set) ([]ResolvedVariable, error)

	// The field tier: a typed field declared on a component_type (the schema
	// half of the field primitive), flat and unscoped like the type registries.
	// The value a component carries for it lives in field_value (a later slice).
	ListFieldDefinitions(ctx context.Context) ([]FieldDefinition, error)
	CreateFieldDefinition(ctx context.Context, actorID string, spec FieldDefinitionSpec) (*FieldDefinition, error)
	UpdateFieldDefinition(ctx context.Context, actorID, id, dataType, displayName string, required bool, def json.RawMessage) (*FieldDefinition, error)
	DeleteFieldDefinition(ctx context.Context, actorID, id string) error

	// field values: the literal a component sets for a field defined on its
	// type (field_value, the variable table narrowed to a component owner: no
	// owner arc, no cascade), plus the effective read that coalesces the set
	// value with the definition's default for a component.
	SetFieldValue(ctx context.Context, actorID, componentName, fieldName string, value json.RawMessage, create scope.Set) (*FieldValue, error)
	UpdateFieldValue(ctx context.Context, actorID, id string, value json.RawMessage, read, action scope.Set) (*FieldValue, error)
	DeleteFieldValue(ctx context.Context, actorID, id string, read, action scope.Set) error
	EffectiveFields(ctx context.Context, componentName string, read scope.Set) ([]EffectiveField, error)

	// The tag tier: the governed key vocabulary and the per-entity value
	// bindings. Minting a key (tag:create) is a tenant-wide governance action;
	// binding a value is the owner's own write, so the binding methods take the
	// owner's read/action scopes. ResolveTags is the per-component effective-tags
	// view (union on key, override on value) down the structural cascade.
	ListTags(ctx context.Context) ([]Tag, error)
	DistinctTagValues(ctx context.Context, key string) ([]string, error)
	CreateTag(ctx context.Context, actorID string, spec TagSpec, create scope.Set) (*Tag, error)
	UpdateTag(ctx context.Context, actorID, name string, spec TagSpec, action scope.Set) (*Tag, error)
	DeleteTag(ctx context.Context, actorID, name string, action scope.Set) error
	SetTagBinding(ctx context.Context, actorID, key, ownerKind string, ownerName *string, value string, read, action scope.Set) (*TagBinding, error)
	DeleteTagBinding(ctx context.Context, actorID, key, ownerKind string, ownerName *string, read, action scope.Set) error
	ListEntityTags(ctx context.Context, ownerKind string, ownerName *string, read scope.Set) ([]TagBinding, error)
	ResolveTags(ctx context.Context, componentID string, read scope.Set) ([]ResolvedTag, error)
	// EffectiveTags batch-resolves the winning effective tags (key -> value) for a
	// set of owners of one kind, feeding the directory Tags column. Scopeless: the
	// caller passes ids already in the read scope (the rowActions batch contract).
	EffectiveTags(ctx context.Context, kind string, ownerIDs []string) (map[string]map[string]string, error)

	// The file tier: a searchable metadata handle over the content-addressed blob
	// store. A file has no estate placement (tenant-wide), so these methods take no
	// scope; canAdmin gates the per-file sensitive flag (the :admin tier), mirroring
	// the secret sensitivity axis. CreateFile stores the upload as a deduplicated
	// blob and points the handle at it; DeleteFile drops the handle but leaves the
	// blob (GC is a later slice).
	ListFiles(ctx context.Context, canAdmin bool) ([]File, error)
	GetFile(ctx context.Context, id string, canAdmin bool) (*File, error)
	CreateFile(ctx context.Context, actorID string, spec FileSpec, canAdmin bool) (*File, error)
	DownloadFile(ctx context.Context, id string, canAdmin bool) (*File, []byte, error)
	DeleteFile(ctx context.Context, actorID, id string, canAdmin bool) error

	// The settings engine (unscoped: platform config, not estate data, so no ABAC
	// scope applies; the route gates on settings:<action> only). The single
	// setting_override table holds only what an operator changed at a cascade level;
	// the base layers (code defaults, operator file) live in memory. Slice-0 uses
	// scope "global" (principal_id NULL). Every write is audited.
	GetSettingOverrides(ctx context.Context, scope string) ([]SettingOverride, error)
	UpsertSettingOverride(ctx context.Context, actorID, scope, namespace string, doc map[string]any, locks []string) (*SettingOverride, error)
	// MergePatchSettingOverride applies an RFC 7386 merge patch to the (scope,
	// namespace) global override as one atomic read-modify-write, serialized against
	// concurrent patches to the same namespace so no update is lost.
	MergePatchSettingOverride(ctx context.Context, actorID, scope, namespace string, patch map[string]any) (*SettingOverride, error)
	DeleteSettingOverride(ctx context.Context, actorID, scope, namespace string) error
	DeleteAllSettingOverrides(ctx context.Context, actorID, scope string) error

	// Close releases the underlying connection pool. Idempotent at the pool
	// level; call once on shutdown.
	Close()
}

// PG is the Postgres-backed Gateway implementation over a pgx connection pool.
type PG struct {
	pool   *pgxpool.Pool
	secret secret.Provider
	blob   blob.Store
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

// WithBlobStore overrides the default pgblobs backend the file bytes are stored
// behind (an S3-compatible or disk backend implementing the same blob.Store).
// Unset, the gateway uses the pgblobs backend over its own pool.
func WithBlobStore(store blob.Store) Option {
	return func(p *PG) { p.blob = store }
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
	p := &PG{pool: pool, blob: NewPGBlobStore(pool)}
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
