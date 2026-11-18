package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Role is a capability set: permissions are <resource>:<action> strings, and
// inherits names parent role ids. official roles ship with the binary and are
// kept authoritative by the boot-seed phase.
type Role struct {
	ID string
	// Namespace is who named the role (#318): og for the platform's.
	Namespace   string
	Name        string
	Official    bool
	Permissions []string
	Inherits    []string
	Label       string // operator-facing name; empty falls back to the id
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
		insert into role (namespace, name, official, permissions, inherits, label, description)
		values (case when $2 then 'og' else 'local' end, $1, $2, $3, $4, $5, $6)
		on conflict (namespace, name) do update
			set official     = excluded.official,
			    permissions  = excluded.permissions,
			    inherits     = excluded.inherits,
			    label = excluded.label,
			    description  = excluded.description`,
		r.Name, r.Official, r.Permissions, r.Inherits, labelOrNull(r.Label), nullize(r.Description))
	if err != nil {
		return fmt.Errorf("storage: upsert role %q: %w", r.Name, err)
	}
	return nil
}

// OwnerSpec describes the first owner to bootstrap: the person plus the
// hashed bearer credential to install. The cleartext token never reaches the
// gateway; the caller generates it, hashes it, and shows it once.
type OwnerSpec struct {
	Username   string
	Email      string
	Label      string
	SecretHash []byte
	Prefix     string
	// PasswordHash, when set, installs an argon2id password credential (PHC
	// encoded) so the owner can log in with a username and password. Optional.
	PasswordHash string
	// ExpiresAt bounds the bootstrap bearer token's lifetime (a CLI/API token, so it
	// gets the token default of 90 days). Nil leaves it unbounded, so a call site that
	// has not opted into an expiry still compiles; the bootstrap CLI always sets it.
	ExpiresAt *time.Time
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
		`select user_id from person where username = $1`, spec.Username).Scan(&existing); {
	case lookupErr == nil:
		return false, nil // already bootstrapped; a no-op that mints no token
	case errors.Is(lookupErr, pgx.ErrNoRows):
		// fall through and create the owner
	default:
		return false, fmt.Errorf("storage: bootstrap lookup: %w", lookupErr)
	}

	var pid string
	if err := tx.QueryRow(ctx,
		`insert into users (kind) values ('person') returning id`).Scan(&pid); err != nil {
		return false, fmt.Errorf("storage: bootstrap user: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`insert into person (user_id, username, email, label) values ($1, $2, $3, $4)`,
		pid, spec.Username, nullize(spec.Email), labelOrNull(spec.Label)); err != nil {
		return false, fmt.Errorf("storage: bootstrap human: %w", err)
	}
	var credID string
	if err := tx.QueryRow(ctx,
		`insert into credential (user_id, kind, secret_hash, prefix, purpose, expires_at, description) values ($1, 'bearer', $2, $3, 'token', $4, 'bootstrap token') returning id`,
		pid, spec.SecretHash, spec.Prefix, spec.ExpiresAt).Scan(&credID); err != nil {
		return false, fmt.Errorf("storage: bootstrap credential: %w", err)
	}
	// Every token's minting is audited (#1162); the bootstrap is the trusted lane,
	// so its row carries no actor.
	img := credentialAuditImage{UserID: pid, Prefix: spec.Prefix, Purpose: credentialPurposeToken, Description: "bootstrap token", ExpiresAt: spec.ExpiresAt}
	if err := writeAuditRes(ctx, tx, "", "create", "credential", credID, nil, img); err != nil {
		return false, err
	}
	if spec.PasswordHash != "" {
		if _, err := tx.Exec(ctx,
			`insert into credential (user_id, kind, secret_hash, prefix) values ($1, 'password', $2, '')`,
			pid, []byte(spec.PasswordHash)); err != nil {
			return false, fmt.Errorf("storage: bootstrap password: %w", err)
		}
	}
	if _, err := tx.Exec(ctx,
		`insert into user_grant (user_id, role_id, scope_kind) values ($1, (select id from role where namespace = 'og' and name = 'owner'), 'all')`,
		pid); err != nil {
		return false, fmt.Errorf("storage: bootstrap grant: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("storage: bootstrap commit: %w", err)
	}
	return true, nil
}

// IssueBearerCredential mints an additional bearer credential for an existing
// user addressed by its username, returning false if no such
// username exists. The caller generates and hashes the token and shows the
// cleartext once; this is the same trusted direct-DB lane as BootstrapOwner
// (token reissue, break-glass, and the dev login). purpose is 'session' (a
// web login) or 'token' (a CLI/API credential), the discriminator between the two
// kinds of bearer, and expiresAt bounds the credential's lifetime: every credential
// is time-bounded now (a session for 12h, a token for 90 days by default), so a
// leaked bearer is never valid forever.
// BearerIssue is the input to IssueBearerCredential: the credential's secret hash and
// prefix, its purpose ('session' or 'token'), its expiry, and identifying metadata. A
// token carries a Description (what it is for); a session leaves it empty. UserAgent and
// ClientIP record the device and address that created it, empty when unknown (a CLI).
type BearerIssue struct {
	// ActorID is who is minting the credential, recorded as the audit row's actor
	// when a token is issued (#1162): the user itself for self-service, empty
	// for the trusted lane (the direct-database CLI), which has no API actor.
	ActorID     string
	Username    string
	SecretHash  []byte
	Prefix      string
	Purpose     string
	ExpiresAt   *time.Time
	Description string
	UserAgent   string
	ClientIP    string
	// Agent, when set, makes the token an agent token (#1179): the name of the
	// agent it is for. AgentRoles narrows the agent to these roles (intersected
	// with the minting person's capability on every request; nil leaves it at the
	// person's own); AgentChannels lists the entry points it is accepted on (nil:
	// any). The API validates all three; this lane stores what it is given.
	Agent         string
	AgentRoles    []string
	AgentChannels []string
}

func (p *PG) IssueBearerCredential(ctx context.Context, spec BearerIssue) (bool, error) {
	var pid string
	err := p.pool.QueryRow(ctx, `select user_id from person where username = $1`, spec.Username).Scan(&pid)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("storage: lookup human %q: %w", spec.Username, err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("storage: begin issue credential: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var credID string
	if err := tx.QueryRow(ctx,
		`insert into credential (user_id, kind, secret_hash, prefix, purpose, expires_at, description, user_agent, client_ip, agent, agent_roles, agent_channels)
			values ($1, 'bearer', $2, $3, $4, $5, nullif($6,''), nullif($7,''), nullif($8,''), nullif($9,''), $10, $11)
			returning id`,
		pid, spec.SecretHash, spec.Prefix, spec.Purpose, spec.ExpiresAt, spec.Description, spec.UserAgent, spec.ClientIP,
		spec.Agent, emptySliceNil(spec.AgentRoles), emptySliceNil(spec.AgentChannels)).Scan(&credID); err != nil {
		return false, fmt.Errorf("storage: issue credential: %w", err)
	}
	// A token is how an agent or a script is given access, so its minting is
	// audited (#1162). A web-login session is the auth lane's (login, logout).
	if spec.Purpose == credentialPurposeToken {
		img := credentialAuditImage{UserID: pid, Prefix: spec.Prefix, Purpose: spec.Purpose, Description: spec.Description, ExpiresAt: spec.ExpiresAt,
			Agent: spec.Agent, AgentRoles: emptySliceNil(spec.AgentRoles), AgentChannels: emptySliceNil(spec.AgentChannels)}
		if err := writeAuditRes(ctx, tx, spec.ActorID, "create", "credential", credID, nil, img); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("storage: commit issue credential: %w", err)
	}
	return true, nil
}

// credentialPurposeToken is the purpose of a CLI or API token, the credential
// minting and revoking audit (#1162).
const credentialPurposeToken = "token"

// credentialAuditImage is a token's audit image: its non-secret metadata only.
// The secret hash never reaches the trail; the prefix is the non-secret locator
// the session list shows, so a reader can tell which token a row is about.
type credentialAuditImage struct {
	UserID      string     `json:"user_id"`
	Prefix      string     `json:"prefix"`
	Purpose     string     `json:"purpose"`
	Description string     `json:"description,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	// Agent, AgentRoles and AgentChannels describe an agent token (#1179), so
	// the trail says which agent a token was minted for and how far it reaches.
	Agent         string   `json:"agent,omitempty"`
	AgentRoles    []string `json:"agent_roles,omitempty"`
	AgentChannels []string `json:"agent_channels,omitempty"`
}

// emptySliceNil maps an empty slice to nil, so an optional text[] column stays
// null rather than holding an empty array.
func emptySliceNil(v []string) []string {
	if len(v) == 0 {
		return nil
	}
	return v
}

// revokedCredential is one deleted credential's identity and metadata, read
// back from a revoke's delete so a token's end can be audited.
type revokedCredential struct {
	id  string
	img credentialAuditImage
}

// auditRevokedTokens writes a revoke row for each token among the deleted
// credentials, in the delete's transaction.
func auditRevokedTokens(ctx context.Context, tx pgx.Tx, actorID string, revoked []revokedCredential) error {
	for _, r := range revoked {
		if r.img.Purpose != credentialPurposeToken {
			continue
		}
		credID := r.id
		if err := writeAuditRes(ctx, tx, actorID, "revoke", "credential", credID, r.img, nil); err != nil {
			return err
		}
	}
	return nil
}

// scanRevoked reads a revoke delete's returning rows.
func scanRevoked(rows pgx.Rows) ([]revokedCredential, error) {
	defer rows.Close()
	var out []revokedCredential
	for rows.Next() {
		var r revokedCredential
		var purpose, description, agent *string
		if err := rows.Scan(&r.id, &r.img.UserID, &r.img.Prefix, &purpose, &description, &r.img.ExpiresAt,
			&agent, &r.img.AgentRoles, &r.img.AgentChannels); err != nil {
			return nil, fmt.Errorf("storage: scan revoked credential: %w", err)
		}
		if purpose != nil {
			r.img.Purpose = *purpose
		}
		if description != nil {
			r.img.Description = *description
		}
		if agent != nil {
			r.img.Agent = *agent
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// revokedCols is what a revoke's delete returns for its audit rows.
const revokedCols = `returning id, user_id, prefix, purpose, description, expires_at, agent, agent_roles, agent_channels`

// nullize maps an empty string to a SQL NULL so optional text columns stay null
// rather than empty.
//
// It is NOT for a `label`, which has its own pair in label_unset.go. The rule
// there is the same shape and one step stricter: it TRIMS first, so a label of
// " " is unset too, and there is a PATCH variant because a label can be cleared
// by an operator and these columns cannot. Sharing this helper would have meant
// the two drifting the first time one of them learned something.
func nullize(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ErrCredentialNotFound is returned by AuthenticateBearer when no credential
// matches the presented token hash. The authn middleware maps it to 401.
var ErrCredentialNotFound = errors.New("storage: credential not found")

// User is an authenticated identity with its kind profile and grants.
type User struct {
	ID     string
	Kind   string
	Active bool
	// ArchivedAt marks a soft delete (the user lifecycle, issue #143): a
	// non-nil value means the account is archived (hidden, cannot authenticate,
	// reversible until purged). Nil means live. Distinct from Active, which is the
	// reversible disable (suspend) toggle.
	ArchivedAt *time.Time
	Person     *PersonProfile
	Machine    *MachineProfile
	Outpost    *OutpostProfile
	Grants     []Grant
	// Agent is set when the request authenticated with an agent token (#1179):
	// the user is the person who minted it, and the agent acts on their
	// behalf, inside the bounds this names. Nil for any other credential.
	Agent *AgentCredential
	// Groups are the user groups this user belongs to (id + label), so the
	// admin directory can show membership without a per-row fetch. The grants those
	// groups confer already ride Grants (tagged with GroupID); this names them.
	Groups []UserGroupRef
}

// AgentCredential is the agent half of an agent token (#1179): which agent it
// is, the roles it is narrowed to (nil: its person's own), and the entry points
// it is accepted on (nil: any). It never widens the person: the API intersects
// the roles with the person's capability on every request.
type AgentCredential struct {
	CredentialID string
	Name         string
	Roles        []string
	Channels     []string
}

// UserGroupRef is a lightweight reference to a group a user belongs to.
type UserGroupRef struct{ ID, Name string }

// PersonProfile, ServiceProfile, and OutpostProfile carry the kind-specific
// attributes. Each kind's identifier is its own column: a person's username, a
// machine user's name, an outpost's name (the fleet address).
type PersonProfile struct {
	Username, Email, Label string
	// MustChangePassword is set by an admin reset and cleared by the user's own
	// change-password; while true the account is gated to the change-password lane.
	MustChangePassword bool
	// HasAvatar is true when the avatar column is non-null. The avatar bytes are
	// NOT loaded here (this struct is filled on every authenticated request);
	// fetch them with GetPersonAvatar.
	HasAvatar       bool
	AvatarUpdatedAt *time.Time
}
type MachineProfile struct{ Name string }
type OutpostProfile struct{ Name string }

// Grant is one (role x scope) pairing on a user, addressable by its id (so
// the admin surface can revoke a specific one).
type Grant struct {
	ID        string
	Role      string
	ScopeKind string
	ScopeID   *string
	ScopeOp   string // how ScopeID matches the tree: subtree, subtree_excl_root, or self
	// GroupID is set when this grant is inherited from a group the user
	// belongs to (nil for a direct grant), so a caller can show inherited access
	// distinctly from direct. A user's effective grants are its direct grants
	// unioned with its groups' grants; both flatten and scope-resolve the same way.
	GroupID *string
	// GroupName is the source group's name (the identifier a client can type
	// back), set alongside GroupID, so a caller can name where an inherited grant
	// comes from. GroupLabel is its friendly string, nil when the group has none.
	GroupName  *string
	GroupLabel *string
}

// ErrBadCredentials is returned by AuthenticatePassword when the username is
// unknown, has no password set, or the password does not match: one error for all
// three so the handler cannot leak which.
var ErrBadCredentials = errors.New("storage: bad credentials")

// ErrAccountDisabled is returned by AuthenticatePassword when the password is
// CORRECT but the user is disabled. It is a success-of-password signal, not a
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

// ResolveUserRef maps a user reference to the user id. A value that
// is a uuid is returned as-is (any user kind, and an unknown uuid still
// resolves here so the caller's own not-found handling applies unchanged).
// Otherwise it is a person's username or a machine user's name (#1133); an
// unknown one is ErrUserNotFound, and one that is both a username and a
// service name is refused as naming no one user (userIDByRef). This
// backs addressing a user by name on the API and CLI.
func (p *PG) ResolveUserRef(ctx context.Context, ref string) (string, error) {
	return userIDByRef(ctx, p.pool, ref)
}

// ResolveUsername maps a person's username to its user id, matching the
// human table alone; an unknown username is ErrUserNotFound.
func (p *PG) ResolveUsername(ctx context.Context, username string) (string, error) {
	var id string
	err := p.pool.QueryRow(ctx, `select user_id from person where username = $1`, username).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", ErrUserNotFound
	case err != nil:
		return "", fmt.Errorf("storage: resolve username %q: %w", username, err)
	}
	return id, nil
}

// AuthenticateBearer resolves a bearer credential by its sha256 hash to the
// user, its kind profile, and its grants. ErrCredentialNotFound if none, which
// includes a credential whose expiry has passed (an expired session is treated as
// absent, so it authenticates nothing).
func (p *PG) AuthenticateBearer(ctx context.Context, hash []byte) (*User, error) {
	var pr User
	var credID string
	var agent *string
	var agentRoles, agentChannels []string
	err := p.pool.QueryRow(ctx, `
		select pr.id, pr.kind, c.id, c.agent, c.agent_roles, c.agent_channels
		from credential c
		join users pr on pr.id = c.user_id
		where c.kind = 'bearer' and c.secret_hash = $1 and pr.active
			and (c.expires_at is null or c.expires_at > now())`, hash).Scan(&pr.ID, &pr.Kind, &credID, &agent, &agentRoles, &agentChannels)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrCredentialNotFound
	case err != nil:
		return nil, fmt.Errorf("storage: authenticate: %w", err)
	}
	// Record activity so the session list can show "last active", throttled to at most
	// once a minute so a burst of requests is not a burst of writes. Best-effort: a
	// failed bump never fails the authentication.
	_, _ = p.pool.Exec(ctx,
		`update credential set last_used_at = now()
			where secret_hash = $1 and (last_used_at is null or last_used_at < now() - interval '1 minute')`, hash)
	if err := p.loadUser(ctx, &pr); err != nil {
		return nil, err
	}
	if agent != nil {
		pr.Agent = &AgentCredential{CredentialID: credID, Name: *agent, Roles: agentRoles, Channels: agentChannels}
	}
	return &pr, nil
}

// AuthenticatePassword verifies a human's password against their argon2id
// credential and resolves the user, its profile, and its grants.
// ErrBadCredentials for an unknown user, no password set, or a wrong password.
func (p *PG) AuthenticatePassword(ctx context.Context, username, password string) (*User, error) {
	pr := User{Kind: "person"}
	var encoded []byte
	var active bool
	var failedCount int
	var lockedUntil *time.Time
	// Do NOT filter on pr.active here: the row must be fetched for a disabled user
	// so their real hash is compared, and the active flag must not steer control
	// flow before the password compare (that would leak account state by timing).
	err := p.pool.QueryRow(ctx, `
		select h.user_id, c.secret_hash, pr.active, h.failed_login_count, h.locked_until
		from person h
		join users pr on pr.id = h.user_id
		join credential c on c.user_id = h.user_id and c.kind = 'password'
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
		return &User{ID: pr.ID, Kind: pr.Kind}, ErrAccountLocked
	}
	if !ok {
		// A real account, wrong password: bump the consecutive-failure counter and,
		// on the threshold-th miss, lock the account. Best effort (the attempt has
		// already failed; a counter write error must not change the response). Return
		// the user id (server-side only, the client error is unchanged) so the
		// handler can audit the failed attempt, a security signal worth recording.
		d := nextLockout(failedCount, now)
		_, _ = p.pool.Exec(ctx,
			`update person set failed_login_count = $2, locked_until = $3 where user_id = $1`,
			pr.ID, d.count, d.lockedUntil)
		return &User{ID: pr.ID, Kind: pr.Kind}, ErrBadCredentials
	}
	// Correct password: clear any accumulated failures (best effort) so a run of
	// wrong guesses that ended in the right password does not carry toward a lock.
	if failedCount != 0 || lockedUntil != nil {
		_, _ = p.pool.Exec(ctx,
			`update person set failed_login_count = 0, locked_until = null where user_id = $1`, pr.ID)
	}
	if !active {
		// Correct password against a disabled account: a distinct, disclosable
		// signal, reachable only with the right password (not an enumeration oracle).
		return &User{ID: pr.ID, Kind: pr.Kind}, ErrAccountDisabled
	}
	if err := p.loadUser(ctx, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// SetPassword installs or replaces the password credential for a human, returning
// false if no such username exists. The caller passes the PHC-encoded argon2id
// hash; cleartext never reaches storage.
func (p *PG) SetPassword(ctx context.Context, username, encoded string) (bool, error) {
	var pid string
	err := p.pool.QueryRow(ctx, `select user_id from person where username = $1`, username).Scan(&pid)
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
		insert into credential (user_id, kind, secret_hash, prefix)
		values ($1, 'password', $2, '')
		on conflict (user_id) where kind = 'password'
			do update set secret_hash = excluded.secret_hash`,
		pid, []byte(encoded)); err != nil {
		return false, fmt.Errorf("storage: set password: %w", err)
	}
	// A user setting their own password clears any force-change flag: this is the
	// lane an admin reset gates the account into, and completing it releases the gate.
	// Rotating the secret also clears any brute-force lockout (issue #171): the lock
	// otherwise only expired lazily at the next login, so a rotation is the immediate
	// intervention.
	if _, err := tx.Exec(ctx,
		`update person set must_change_password = false, failed_login_count = 0, locked_until = null where user_id = $1`,
		pid); err != nil {
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

// SetUserPassword resets a person's password by id, on behalf of an
// admin. Requires an all-scope grant (users are not scope-tree scoped). Unlike
// SetPassword (self-service, keyed on username, audited as the target), this audits
// the acting admin as the actor and does not require the target's current password.
// The target must be a human; an unknown id is ErrUserNotFound. A reset also
// revokes every one of the target's bearer credentials (all sessions and tokens) in
// the same transaction, so the reset takes effect immediately: any live session is
// signed out and must re-authenticate with the new password.
func (p *PG) SetUserPassword(ctx context.Context, actorID, userID, encoded string, action scope.Set) error {
	if !action.All {
		return ErrUserForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin reset password: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Only a human holds a password credential; an unknown id (or a service) is not found.
	var pgErr *pgconn.PgError
	if err := tx.QueryRow(ctx, `select 1 from person where user_id = $1`, userID).Scan(new(int)); err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrUserNotFound
		case errors.As(err, &pgErr) && pgErr.Code == "22P02":
			return ErrUserNotFound
		default:
			return fmt.Errorf("storage: reset password lookup: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		insert into credential (user_id, kind, secret_hash, prefix)
		values ($1, 'password', $2, '')
		on conflict (user_id) where kind = 'password'
			do update set secret_hash = excluded.secret_hash`,
		userID, []byte(encoded)); err != nil {
		return fmt.Errorf("storage: reset password: %w", err)
	}
	// Force logout: drop every SESSION for the target so a reset immediately signs it
	// out everywhere. API tokens are left intact: a token is its own bearer secret, not
	// tied to the password, and has its own revoke surface (issue #194).
	if _, err := tx.Exec(ctx, `delete from credential where user_id = $1 and kind = 'bearer' and purpose = 'session'`, userID); err != nil {
		return fmt.Errorf("storage: revoke sessions on reset: %w", err)
	}
	// Force a change on next login: the admin knows the value they just set, so the
	// target must replace it before doing anything else (cleared by their own change).
	// Clear any brute-force lockout in the same write (issue #171): a reset is the
	// reachable intervention while an account is locked, but the lock otherwise only
	// expired lazily at the next login, so it would keep the account out for the rest
	// of the window even with the new secret.
	if _, err := tx.Exec(ctx,
		`update person set must_change_password = true, failed_login_count = 0, locked_until = null where user_id = $1`,
		userID); err != nil {
		return fmt.Errorf("storage: flag must-change on reset: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "reset_password", "credential", userID, nil, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit reset password: %w", err)
	}
	return nil
}

// setAvatarTx writes (or clears, when b64 == "") a human's avatar within tx and
// audits the change against actorID. It does not commit. avatar_updated_at is
// server-clocked to now() on write and null on clear.
func setAvatarTx(ctx context.Context, tx pgx.Tx, actorID, userID, b64 string) error {
	verb := "clear_avatar"
	var val any
	if b64 != "" {
		val, verb = b64, "set_avatar"
	}
	if _, err := tx.Exec(ctx,
		`update person set avatar = $2, avatar_updated_at = case when $3 then now() else null end
		 where user_id = $1`,
		userID, val, b64 != ""); err != nil {
		return fmt.Errorf("storage: write avatar: %w", err)
	}
	return writeAuditRes(ctx, tx, actorID, verb, "user", userID, nil, nil)
}

// SetOwnAvatar sets the caller's own profile picture (a normalized base64 JPEG),
// audited as the caller. No capability is required.
func (p *PG) SetOwnAvatar(ctx context.Context, userID, jpegB64 string) error {
	return p.avatarTxn(ctx, userID, userID, jpegB64)
}

// ClearOwnAvatar removes the caller's own profile picture, audited as the caller.
func (p *PG) ClearOwnAvatar(ctx context.Context, userID string) error {
	return p.avatarTxn(ctx, userID, userID, "")
}

// SetUserAvatar sets another user's profile picture on behalf of an
// admin, audited as the actor. Requires an all-scope grant; an unknown id is
// ErrUserNotFound.
func (p *PG) SetUserAvatar(ctx context.Context, actorID, id, jpegB64 string, action scope.Set) error {
	if !action.All {
		return ErrUserForbidden
	}
	return p.avatarTxn(ctx, actorID, id, jpegB64)
}

// ClearUserAvatar removes another user's profile picture on behalf of
// an admin, audited as the actor. Requires an all-scope grant; an unknown id is
// ErrUserNotFound.
func (p *PG) ClearUserAvatar(ctx context.Context, actorID, id string, action scope.Set) error {
	if !action.All {
		return ErrUserForbidden
	}
	return p.avatarTxn(ctx, actorID, id, "")
}

// avatarTxn confirms the target human exists, then writes (or clears) its avatar
// and commits. An unknown id (or a service user) is ErrUserNotFound.
func (p *PG) avatarTxn(ctx context.Context, actorID, id, b64 string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin avatar: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists int
	if err := tx.QueryRow(ctx, `select 1 from person where user_id = $1`, id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			return ErrUserNotFound
		}
		return fmt.Errorf("storage: avatar lookup: %w", err)
	}
	if err := setAvatarTx(ctx, tx, actorID, id, b64); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit avatar: %w", err)
	}
	return nil
}

// GetPersonAvatar returns a human's profile picture as a base64 JPEG. The bool is
// false (with no error) when the human has no picture; an unknown id is
// ErrUserNotFound.
func (p *PG) GetPersonAvatar(ctx context.Context, id string) (string, bool, error) {
	var b64 *string
	if err := p.pool.QueryRow(ctx, `select avatar from person where user_id = $1`, id).Scan(&b64); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, ErrUserNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			return "", false, ErrUserNotFound
		}
		return "", false, fmt.Errorf("storage: get avatar: %w", err)
	}
	if b64 == nil {
		return "", false, nil
	}
	return *b64, true, nil
}

// GetUserAvatar is the admin read of another user's profile picture. It
// applies the directory's all-scope invariant (a non-all scope is
// ErrUserForbidden, mirroring the admin write methods), then delegates to
// GetPersonAvatar.
func (p *PG) GetUserAvatar(ctx context.Context, id string, action scope.Set) (string, bool, error) {
	if !action.All {
		return "", false, ErrUserForbidden
	}
	return p.GetPersonAvatar(ctx, id)
}

// PersonProfilePatch carries the editable self-profile fields. A nil pointer
// leaves the column unchanged; a non-nil pointer sets it, with an empty string
// clearing the nullable column to NULL.
type PersonProfilePatch struct {
	Label *string
	Email *string
}

// UpdatePersonProfile applies a partial update to a human's own profile, addressed
// by user id (the authenticated session's own id). Absent fields are left
// as-is; a provided empty string clears the column. The row exists by
// construction (the caller is that user), so a no-match is not signalled.
func (p *PG) UpdatePersonProfile(ctx context.Context, userID string, patch PersonProfilePatch) error {
	setDisplay, display := patch.Label != nil, any(nil)
	if patch.Label != nil {
		display = labelOrNull(*patch.Label)
	}
	setEmail, email := patch.Email != nil, any(nil)
	if patch.Email != nil {
		email = nullize(*patch.Email)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin update person profile: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		update person set
			label = case when $2 then $3 else label end,
			email        = case when $4 then $5 else email end
		where user_id = $1`,
		userID, setDisplay, display, setEmail, email); err != nil {
		return fmt.Errorf("storage: update person profile: %w", err)
	}
	// Audit the self-profile change so an impersonated (act-as) edit records the
	// real actor, and every profile edit leaves a trail. Log the changed fields.
	summary := map[string]any{}
	if patch.Label != nil {
		summary["label"] = *patch.Label
	}
	if patch.Email != nil {
		summary["email"] = *patch.Email
	}
	if err := writeAuditRes(ctx, tx, userID, "update", "user", userID, nil, summary); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit update person profile: %w", err)
	}
	return nil
}

// AnyPerson reports whether any person exists, so the login screen hides
// the bootstrap hint once the system has an owner.
func (p *PG) AnyPerson(ctx context.Context) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `select exists(select 1 from person)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("storage: any human: %w", err)
	}
	return exists, nil
}

// RevokeBearer deletes the bearer credential with the given sha256 hash (logout
// of a session token). A no-op if none matches.
func (p *PG) RevokeBearer(ctx context.Context, hash []byte) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin revoke bearer: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `delete from credential where kind = 'bearer' and secret_hash = $1 `+revokedCols, hash)
	var revoked []revokedCredential
	if err == nil {
		revoked, err = scanRevoked(rows)
	}
	if err != nil {
		return fmt.Errorf("storage: revoke bearer: %w", err)
	}
	// A token presented as the session and logged out is ended by the user it
	// belonged to (#1162).
	for _, r := range revoked {
		if err := auditRevokedTokens(ctx, tx, r.img.UserID, []revokedCredential{r}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit revoke bearer: %w", err)
	}
	return nil
}

// BearerCredential is one of a user's live bearer credentials (a login
// session or an API token), carrying only its non-secret metadata. The secret hash
// is never returned: this is the shape the self-service session list exposes.
// Purpose is 'session' (a web login) or 'token' (a CLI/API credential), the
// discriminator between the two, since both now carry an ExpiresAt. Current is true
// for the one credential that authenticated the request doing the listing (so the
// console can label it and treat its revoke as a self sign-out).
type BearerCredential struct {
	ID          string
	Prefix      string
	Purpose     string
	Description string
	UserAgent   string
	ClientIP    string
	CreatedAt   time.Time
	LastUsedAt  *time.Time
	ExpiresAt   *time.Time
	Current     bool
	// Agent, AgentRoles and AgentChannels are an agent token's bounds (#1179);
	// empty for a session or a personal token.
	Agent         string
	AgentRoles    []string
	AgentChannels []string
	// OAuthClient is the name of the OAuth client a token was issued to by
	// sign-in (#2048); empty for any other credential.
	OAuthClient string
}

// ListBearerCredentials returns the user's own LIVE bearer credentials, newest
// first, with their id, prefix, purpose, created_at, and expires_at. The raw
// secret_hash is never returned: currentHash (the sha256 of the token that
// authenticated this request) is only compared in SQL to compute the Current flag,
// so the hash bytes never leave the database. Only live credentials are returned (an
// unexpired or legacy-null expires_at), mirroring AuthenticateBearer's own filter, so
// a signed-in user never sees a dead row that would authenticate nothing. An OAuth
// client whose access token lapsed but whose refresh token is live is listed, with
// the refresh token's expiry, since it can still renew itself (#2073). Backs the
// self-service session list (issue #172).
func (p *PG) ListBearerCredentials(ctx context.Context, userID string, currentHash []byte) ([]BearerCredential, error) {
	rows, err := p.pool.Query(ctx, `
		select c.id, c.prefix, coalesce(c.purpose, ''), coalesce(c.description, ''), coalesce(c.user_agent, ''), coalesce(c.client_ip, ''),
			c.created_at, c.last_used_at, coalesce(r.expires_at, c.expires_at), coalesce(c.secret_hash = $2, false) as current,
			coalesce(c.agent, ''), c.agent_roles, c.agent_channels, coalesce(o.client_name, '')
		from credential c
		left join oauth_client o on o.id = c.oauth_client_id
		left join lateral (
			select max(expires_at) as expires_at from oauth_refresh
			where credential_id = c.id and spent_at is null and expires_at > now()
		) r on true
		where c.user_id = $1 and c.kind = 'bearer'
			and (c.expires_at is null or c.expires_at > now() or r.expires_at is not null)
		order by c.created_at desc`, userID, currentHash)
	if err != nil {
		return nil, fmt.Errorf("storage: list bearer credentials: %w", err)
	}
	defer rows.Close()
	var out []BearerCredential
	for rows.Next() {
		var c BearerCredential
		if err := rows.Scan(&c.ID, &c.Prefix, &c.Purpose, &c.Description, &c.UserAgent, &c.ClientIP,
			&c.CreatedAt, &c.LastUsedAt, &c.ExpiresAt, &c.Current, &c.Agent, &c.AgentRoles, &c.AgentChannels, &c.OAuthClient); err != nil {
			return nil, fmt.Errorf("storage: scan bearer credential: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RevokeBearerByID deletes one bearer credential by id, scoped to the owning
// user so a caller can only revoke their own (a session or token belonging to
// another user is untouched). Returns false when nothing matched: a wrong or
// already-revoked id, a credential owned by a different user, or a malformed id
// (which the API maps to a 404, never a 500). This backs self-service revoke (#172).
func (p *PG) RevokeBearerByID(ctx context.Context, actorID, userID, credentialID string) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("storage: begin revoke bearer by id: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx,
		`delete from credential where id = $1 and user_id = $2 and kind = 'bearer' `+revokedCols,
		credentialID, userID)
	var revoked []revokedCredential
	if err == nil {
		revoked, err = scanRevoked(rows)
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22P02" {
			// A malformed uuid (id or user id) matches no row: not found, not a 500.
			return false, nil
		}
		return false, fmt.Errorf("storage: revoke bearer by id: %w", err)
	}
	if err := auditRevokedTokens(ctx, tx, actorID, revoked); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("storage: commit revoke bearer by id: %w", err)
	}
	return len(revoked) > 0, nil
}

// RevokeBearersByPurpose deletes every bearer credential of one purpose ('session'
// or 'token') for a user, scoped to the owning user, and returns the count.
// Purpose is the authoritative discriminator (both kinds carry an expiry now), so a
// caller ending all sessions never touches a token, and vice versa.
func (p *PG) RevokeBearersByPurpose(ctx context.Context, actorID, userID, purpose string) (int, error) {
	return p.RevokeBearersByPurposeExcept(ctx, actorID, userID, purpose, nil)
}

// RevokeBearersByPurposeExcept deletes bearers of one purpose except any whose
// secret_hash is in keep, scoped to the user, returning the count. An empty keep
// deletes all of the purpose (the plain bulk revoke); a non-empty keep preserves the
// caller's current session while ending its others, tokens untouched.
func (p *PG) RevokeBearersByPurposeExcept(ctx context.Context, actorID, userID, purpose string, keep [][]byte) (int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: begin revoke bearers by purpose: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var rows pgx.Rows
	if len(keep) == 0 {
		rows, err = tx.Query(ctx,
			`delete from credential where user_id = $1 and kind = 'bearer' and purpose = $2 `+revokedCols,
			userID, purpose)
	} else {
		rows, err = tx.Query(ctx,
			`delete from credential where user_id = $1 and kind = 'bearer' and purpose = $2 and secret_hash <> all($3::bytea[]) `+revokedCols,
			userID, purpose, keep)
	}
	var revoked []revokedCredential
	if err == nil {
		revoked, err = scanRevoked(rows)
	}
	if err != nil {
		return 0, fmt.Errorf("storage: revoke bearers by purpose: %w", err)
	}
	if err := auditRevokedTokens(ctx, tx, actorID, revoked); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("storage: commit revoke bearers by purpose: %w", err)
	}
	return len(revoked), nil
}

// ErrUserForbidden is returned by the user directory methods when the
// caller's resolved scope is not all-scope. A user is not a scope-tree
// entity, so a location or system grant confers no user access; only an
// all-scope grant does. The API maps it to 403.
var ErrUserForbidden = errors.New("storage: user access requires an all-scope grant")

// ErrNotArchived is returned by PurgeUser when the target has not been
// archived first (the hard gate between the soft and hard delete).
var ErrNotArchived = errors.New("storage: user must be archived before purge")

// ErrUserNotFound is returned by GetUser when no user has the id.
// The API maps it to 404.
var ErrUserNotFound = errors.New("storage: user not found")

// ErrUsernameTaken is returned by CreatePerson when the username already
// exists. The API maps it to 409.
var ErrUsernameTaken = errors.New("storage: username already exists")

// PersonSpec is the admin create-a-human input. PasswordHash, when set, installs a
// password credential (PHC-encoded argon2id); cleartext never reaches storage.
type PersonSpec struct {
	Username     string
	Email        string
	Label        string
	PasswordHash string
}

// ListUsers returns every user, people before machine users, each kind in
// creation order. Kind leads so the directory groups people apart from machine
// identities; within a kind, seq (the DB-assigned identity) is the order,
// because clock-derived keys (created_at, uuidv7 ids) invert across
// transactions when the wall clock steps mid-run and flapped the
// zero-tolerance screenshot gate (#780).
// Reads require an all-scope grant (a user is not scope-tree scoped),
// so a non-all read scope is ErrUserForbidden rather than a silent empty
// list. Archived (soft-deleted) users are excluded unless
// includeArchived is set (the directory's "show archived" view, so a hidden
// account can be restored or purged). Credentials are never loaded.
func (p *PG) ListUsers(ctx context.Context, read scope.Set, includeArchived bool) ([]User, error) {
	if !read.All {
		return nil, ErrUserForbidden
	}
	rows, err := p.pool.Query(ctx, `select id, kind, active, archived_at from users where ($1 or archived_at is null) order by kind <> 'person', seq`, includeArchived)
	if err != nil {
		return nil, fmt.Errorf("storage: list users: %w", err)
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
			return nil, fmt.Errorf("storage: scan user: %w", err)
		}
		bases = append(bases, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: list users: %w", err)
	}
	// loadUsers runs its own queries, so the row cursor is drained first. It
	// is the batched counterpart of loadUser (#671): three statements for the
	// whole page rather than three per row, which is what keeps the admin
	// directory readable at organisation scale. loadUser stays the single-row
	// path and the oracle the batch is measured against.
	out := make([]User, 0, len(bases))
	for _, b := range bases {
		out = append(out, User{ID: b.id, Kind: b.kind, Active: b.active, ArchivedAt: b.archivedAt})
	}
	if err := p.loadUsers(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetUser resolves one user by id, with its profile and grants. Reads
// require an all-scope grant; an unknown id is ErrUserNotFound.
func (p *PG) GetUser(ctx context.Context, id string, read scope.Set) (*User, error) {
	if !read.All {
		return nil, ErrUserForbidden
	}
	pr := User{ID: id}
	err := p.pool.QueryRow(ctx, `select id, kind, active, archived_at from users where id = $1`, id).Scan(&pr.ID, &pr.Kind, &pr.Active, &pr.ArchivedAt)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrUserNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "22P02":
		// A malformed id (invalid uuid text) identifies no user: a clean 404,
		// not a 500.
		return nil, ErrUserNotFound
	case err != nil:
		return nil, fmt.Errorf("storage: get user: %w", err)
	}
	if err := p.loadUser(ctx, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// CreatePerson creates a person (and, when a password hash is
// given, its password credential) in one audited transaction. Creates require an
// all-scope grant; a duplicate username is ErrUsernameTaken. The new user
// holds no grants (role assignment is a later admin surface).
func (p *PG) CreatePerson(ctx context.Context, actorID string, spec PersonSpec, create scope.Set) (*User, error) {
	if !create.All {
		return nil, ErrUserForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create user: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pid string
	if err := tx.QueryRow(ctx, `insert into users (kind) values ('person') returning id`).Scan(&pid); err != nil {
		return nil, fmt.Errorf("storage: create user: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`insert into person (user_id, username, email, label) values ($1, $2, $3, $4)`,
		pid, spec.Username, nullize(spec.Email), labelOrNull(spec.Label)); err != nil {
		return nil, mapUserWriteErr(err)
	}
	if spec.PasswordHash != "" {
		if _, err := tx.Exec(ctx,
			`insert into credential (user_id, kind, secret_hash, prefix) values ($1, 'password', $2, '')`,
			pid, []byte(spec.PasswordHash)); err != nil {
			return nil, fmt.Errorf("storage: create password: %w", err)
		}
	}
	summary := map[string]any{"username": spec.Username, "kind": "person"}
	if err := writeAuditRes(ctx, tx, actorID, "create", "user", pid, nil, summary); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create user: %w", err)
	}

	pr := User{ID: pid, Kind: "person"}
	if err := p.loadUser(ctx, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// ErrUserNotPerson is returned when an admin profile update targets a
// machine user (only people have username / email / label). The API
// maps it to 422.
var ErrUserNotPerson = errors.New("storage: user is not a human")

// AdminPersonPatch carries the admin-editable fields of a person. A nil
// pointer leaves the field unchanged; a provided empty string clears the nullable
// label or email. Username is not nullable: a provided empty string is a
// request fault the API rejects before the gateway.
type AdminPersonPatch struct {
	Label    *string
	Email    *string
	Username *string
}

// UpdatePerson applies an admin profile update to a person by id,
// in one audited transaction. Requires an all-scope grant. A non-human target is
// ErrUserNotPerson, an unknown id ErrUserNotFound, a username clash
// ErrUsernameTaken. Renaming is safe: nothing keys on the username (credentials
// and grants reference the user id), so a rename follows the identity.
func (p *PG) UpdatePerson(ctx context.Context, actorID, userID string, patch AdminPersonPatch, action scope.Set) (*User, error) {
	if !action.All {
		return nil, ErrUserForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update user: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var kind string
	err = tx.QueryRow(ctx, `select kind from users where id = $1`, userID).Scan(&kind)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrUserNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "22P02":
		return nil, ErrUserNotFound
	case err != nil:
		return nil, fmt.Errorf("storage: update users lookup: %w", err)
	}
	if kind != "person" {
		return nil, ErrUserNotPerson
	}

	// The audit "before" is the current human row.
	var before PersonProfile
	if err := tx.QueryRow(ctx,
		`select username, coalesce(email, ''), coalesce(label, '') from person where user_id = $1`,
		userID).Scan(&before.Username, &before.Email, &before.Label); err != nil {
		return nil, fmt.Errorf("storage: update users before: %w", err)
	}

	setDisplay, display := patch.Label != nil, any(nil)
	if patch.Label != nil {
		display = labelOrNull(*patch.Label)
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
		update person set
			label = case when $2 then $3 else label end,
			email        = case when $4 then $5 else email end,
			username     = case when $6 then $7 else username end
		where user_id = $1`,
		userID, setDisplay, display, setEmail, email, setUsername, username); err != nil {
		return nil, mapUserWriteErr(err)
	}

	after := before
	if patch.Label != nil {
		after.Label = *patch.Label
	}
	if patch.Email != nil {
		after.Email = *patch.Email
	}
	if patch.Username != nil {
		after.Username = *patch.Username
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "user", userID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update user: %w", err)
	}

	pr := User{ID: userID, Kind: "person"}
	if err := p.loadUser(ctx, &pr); err != nil {
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

// GrantSpec is a role x scope to assign to a user. ScopeID is empty for the
// "all" scope; for any other kind it names the scope root.
type GrantSpec struct {
	Role      string
	ScopeKind string
	ScopeID   string
	ScopeOp   string // subtree (default), subtree_excl_root, or self; empty means subtree
}

// CreateGrant assigns a role x scope to a user, audited. Requires an
// all-scope grant. A non-all scope with no scope id is ErrBadScope; an unknown
// role ErrUnknownRole; an unknown user ErrUserNotFound; a duplicate
// ErrGrantExists.
func (p *PG) CreateGrant(ctx context.Context, actorID, userID string, spec GrantSpec, action scope.Set) (*Grant, error) {
	if !action.All {
		return nil, ErrUserForbidden
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

	// A scoped grant must target a real entity of that kind, stored by its id so
	// the scope filter (which matches entity ids) resolves. The root may be named
	// by uuid, name or dotted address (#1133); whatever names it, the uuid is what
	// the grant stores, and a root that names nothing is refused up front rather
	// than 500-ing the scoped list later.
	if spec.ScopeKind != "all" {
		if spec.ScopeID, err = resolveScopeRoot(ctx, tx, spec.ScopeKind, spec.ScopeID); err != nil {
			return nil, err
		}
	}

	var gid string
	err = tx.QueryRow(ctx,
		`insert into user_grant (user_id, role_id, scope_kind, scope_id, scope_op) values ($1, (select id from `+refRows("role", spec.Role)+` role where `+catalogRefCol(spec.Role)+` = $2), $3, $4, $5) returning id`,
		userID, spec.Role, spec.ScopeKind, nullize(spec.ScopeID), spec.ScopeOp).Scan(&gid)
	if err != nil {
		return nil, mapGrantWriteErr(err)
	}
	g := Grant{ID: gid, Role: spec.Role, ScopeKind: spec.ScopeKind, ScopeOp: spec.ScopeOp}
	if spec.ScopeID != "" {
		g.ScopeID = &spec.ScopeID
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "user_grant", gid, nil, g); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create grant: %w", err)
	}
	return &g, nil
}

// RevokeGrant deletes one grant from a user, audited. Requires an all-scope
// grant. An unknown grant (for that user) is ErrGrantNotFound. Revoking the
// last owner grant is refused by the deferred owner-invariant trigger, surfaced at
// COMMIT as ErrLastOwner.
func (p *PG) RevokeGrant(ctx context.Context, actorID, userID, grantID string, action scope.Set) error {
	if !action.All {
		return ErrUserForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin revoke grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var g Grant
	err = tx.QueryRow(ctx,
		`select id, (select name from role where id = user_grant.role_id), scope_kind, scope_id, scope_op from user_grant where id = $1 and user_id = $2`,
		grantID, userID).Scan(&g.ID, &g.Role, &g.ScopeKind, &g.ScopeID, &g.ScopeOp)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrGrantNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "22P02":
		return ErrGrantNotFound
	case err != nil:
		return fmt.Errorf("storage: revoke grant lookup: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from user_grant where id = $1`, grantID); err != nil {
		return fmt.Errorf("storage: revoke grant: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "user_grant", grantID, g, nil); err != nil {
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

// SetUserActive enables or disables a user (soft), audited. Requires an
// all-scope grant. Disabling the last active owner is refused (ErrLastOwner); a
// disabled user cannot authenticate, and enabling restores access.
func (p *PG) SetUserActive(ctx context.Context, actorID, userID string, active bool, action scope.Set) error {
	if !action.All {
		return ErrUserForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin set active: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var kind string
	var wasActive bool
	err = tx.QueryRow(ctx, `select kind, active from users where id = $1`, userID).Scan(&kind, &wasActive)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrUserNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "22P02":
		return ErrUserNotFound
	case err != nil:
		return fmt.Errorf("storage: set active lookup: %w", err)
	}
	if _, err := tx.Exec(ctx, `update users set active = $2 where id = $1`, userID, active); err != nil {
		return fmt.Errorf("storage: set active: %w", err)
	}
	if !active {
		// Refuse if disabling this user leaves no active owner@all grant (the
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
	if err := writeAuditRes(ctx, tx, actorID, verb, "user", userID,
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
// user's active flag rather than the grant's existence).
func activeOwnerRemains(ctx context.Context, tx pgx.Tx) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `
		select exists(
			select 1 from user_grant g
			join users pr on pr.id = g.user_id
			where g.role_id = (select id from role where namespace = 'og' and name = 'owner') and g.scope_kind = 'all' and pr.active
		)`).Scan(&ok)
	return ok, err
}

// ArchiveUser soft-deletes a user (the lifecycle, issue #143): it sets
// archived_at and forces the account inactive, so it is hidden from the
// directory and cannot authenticate, reversibly (RestoreUser restores it)
// until PurgeUser hard-deletes it. Requires an all-scope grant; archiving
// the last active owner is refused (ErrLastOwner).
func (p *PG) ArchiveUser(ctx context.Context, actorID, id string, action scope.Set) error {
	return p.setArchived(ctx, actorID, id, true, action)
}

// RestoreUser reverses an archive: it clears archived_at and
// restores the active flag, so the account is live and can authenticate again.
// Requires an all-scope grant.
func (p *PG) RestoreUser(ctx context.Context, actorID, id string, action scope.Set) error {
	return p.setArchived(ctx, actorID, id, false, action)
}

func (p *PG) setArchived(ctx context.Context, actorID, userID string, archive bool, action scope.Set) error {
	if !action.All {
		return ErrUserForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin archive: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pgErr *pgconn.PgError
	if err := tx.QueryRow(ctx, `select 1 from users where id = $1`, userID).Scan(new(int)); err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrUserNotFound
		case errors.As(err, &pgErr) && pgErr.Code == "22P02":
			return ErrUserNotFound
		default:
			return fmt.Errorf("storage: archive lookup: %w", err)
		}
	}

	if archive {
		if _, err := tx.Exec(ctx, `update users set archived_at = now(), active = false where id = $1`, userID); err != nil {
			return fmt.Errorf("storage: archive: %w", err)
		}
		remains, err := activeOwnerRemains(ctx, tx)
		if err != nil {
			return fmt.Errorf("storage: owner check: %w", err)
		}
		if !remains {
			return ErrLastOwner // rolls back the archive
		}
	} else if _, err := tx.Exec(ctx, `update users set archived_at = null, active = true where id = $1`, userID); err != nil {
		return fmt.Errorf("storage: restore: %w", err)
	}

	verb := "restore"
	if archive {
		verb = "archive"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "user", userID, nil, map[string]any{"archived": archive}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit archive: %w", err)
	}
	return nil
}

// PurgeUser hard-deletes a user (issue #143), gated on prior archival
// (ErrNotArchived otherwise, the hard gate between the soft and hard delete).
// The delete cascades the user's owned rows (profile, credentials, grants,
// group memberships, impersonation sessions); the audit trail survives, because the
// audit foreign keys are ON DELETE SET NULL and every row keeps a denormalized
// actor label. Requires an all-scope grant; refused (ErrLastOwner) if it would
// remove the last owner grant.
func (p *PG) PurgeUser(ctx context.Context, actorID, userID string, action scope.Set) error {
	if !action.All {
		return ErrUserForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin purge: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var archivedAt *time.Time
	var pgErr *pgconn.PgError
	if err := tx.QueryRow(ctx, `select archived_at from users where id = $1`, userID).Scan(&archivedAt); err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrUserNotFound
		case errors.As(err, &pgErr) && pgErr.Code == "22P02":
			return ErrUserNotFound
		default:
			return fmt.Errorf("storage: purge lookup: %w", err)
		}
	}
	if archivedAt == nil {
		return ErrNotArchived
	}
	// A notification target that notifies this person holds them (#1341).
	if err := boundTarget(ctx, tx, "user_id", userID, "user"); err != nil {
		return err
	}
	// Audit the purge before the row is gone; the actor is the purger (unaffected by
	// the delete), and the resource id is a plain text value, not a foreign key.
	if err := writeAuditRes(ctx, tx, actorID, "purge", "user", userID, nil, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from users where id = $1`, userID); err != nil {
		if isForeignKeyViolation(err) {
			if berr := boundTarget(ctx, p.pool, "user_id", userID, "user"); berr != nil {
				return berr
			}
		}
		return fmt.Errorf("storage: purge user: %w", err)
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

// mapGrantWriteErr translates the user_grant constraint violations into the
// grant sentinels the API reports.
func mapGrantWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrGrantExists
		case "23502": // not_null_violation (spec.Role resolved to no role)
			return ErrUnknownRole
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "user_grant_role_id_fkey" {
				return ErrUnknownRole
			}
			return ErrUserNotFound
		case "23514": // check_violation (scope_kind)
			return ErrBadScope
		case "22P02": // invalid uuid (user id)
			return ErrUserNotFound
		}
	}
	return fmt.Errorf("storage: create grant: %w", err)
}

// mapUserWriteErr translates the human.username unique violation into
// ErrUsernameTaken (the API's 409); other errors pass through wrapped.
func mapUserWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrUsernameTaken
	}
	return fmt.Errorf("storage: create person: %w", err)
}

// loadUser fills a user's kind profile and its grants, given its id and kind
// already set. A machine user's profile is its machine row, or its outpost row
// when the machine is an outpost's.
func (p *PG) loadUser(ctx context.Context, pr *User) error {
	switch pr.Kind {
	case "person":
		var h PersonProfile
		if err := p.pool.QueryRow(ctx,
			`select username, coalesce(email, ''), coalesce(label, ''), must_change_password,
			        avatar is not null, avatar_updated_at
			 from person where user_id = $1`,
			pr.ID).Scan(&h.Username, &h.Email, &h.Label, &h.MustChangePassword,
			&h.HasAvatar, &h.AvatarUpdatedAt); err != nil {
			return fmt.Errorf("storage: load person: %w", err)
		}
		pr.Person = &h
	case "machine":
		var name string
		var outpost bool
		if err := p.pool.QueryRow(ctx,
			`select name, false from machine where user_id = $1
			  union all
			 select name, true from outpost where user_id = $1
			  limit 1`, pr.ID).Scan(&name, &outpost); err != nil {
			return fmt.Errorf("storage: load machine: %w", err)
		}
		if outpost {
			pr.Outpost = &OutpostProfile{Name: name}
		} else {
			pr.Machine = &MachineProfile{Name: name}
		}
	}

	// A user's effective grants are its direct grants unioned with the grants
	// of every group it belongs to. This is the one seam group access rides: the
	// permission flatten (roleIDs) and the gateway's per-action scope resolution
	// both read pr.Grants, so a member inherits a group's role and scope here and
	// nowhere else. group_id tags an inherited grant so callers can tell it apart.
	rows, err := p.pool.Query(ctx,
		`select g.id, (select name from role where id = g.role_id), g.scope_kind, g.scope_id, g.scope_op, g.group_id, pg.name, pg.label
		   from user_grant g
		   left join user_group pg on pg.id = g.group_id
		  where g.user_id = $1
		     or g.group_id in (select group_id from user_group_member where user_id = $1)
		  order by g.group_id nulls first, g.created_at`, pr.ID)
	if err != nil {
		return fmt.Errorf("storage: load grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.ID, &g.Role, &g.ScopeKind, &g.ScopeID, &g.ScopeOp, &g.GroupID, &g.GroupName, &g.GroupLabel); err != nil {
			return fmt.Errorf("storage: scan grant: %w", err)
		}
		pr.Grants = append(pr.Grants, g)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: grants: %w", err)
	}

	// The user's group memberships (id + label), so the admin directory can
	// show where a user's inherited access comes from.
	grows, err := p.pool.Query(ctx,
		`select g.id, coalesce(g.label, g.name)
		   from user_group_member m join user_group g on g.id = m.group_id
		  where m.user_id = $1 order by g.name`, pr.ID)
	if err != nil {
		return fmt.Errorf("storage: load groups: %w", err)
	}
	defer grows.Close()
	for grows.Next() {
		var ref UserGroupRef
		if err := grows.Scan(&ref.ID, &ref.Name); err != nil {
			return fmt.Errorf("storage: scan group ref: %w", err)
		}
		pr.Groups = append(pr.Groups, ref)
	}
	return grows.Err()
}

// ListRoles returns every role, for building the in-process role index.
func (p *PG) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := p.pool.Query(ctx, `select id, name, official, permissions, inherits, coalesce(label, ''), coalesce(description, ''), namespace from role order by name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list roles: %w", err)
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Official, &r.Permissions, &r.Inherits, &r.Label, &r.Description, &r.Namespace); err != nil {
			return nil, fmt.Errorf("storage: scan role: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RolesInEffect is the roles a bare role name reaches (#1432): a role refuses
// ambiguity rather than shadow (the #318 ruling), so a name both the operator's
// local and the platform's og namespace hold reaches neither, and a role in any
// other namespace is never reached by a bare name. The permission index is built
// from these, so an ambiguous name fails closed: its holders lose the role
// rather than gain the wrong one. (Operators have no role-create path yet;
// grants that name a role exactly come with it.)
func RolesInEffect(roles []Role) []Role {
	held := map[string]int{}
	for _, r := range roles {
		if r.Namespace == NamespaceLocal || r.Namespace == NamespaceOG || r.Namespace == "" {
			held[r.Name]++
		}
	}
	out := make([]Role, 0, len(roles))
	for _, r := range roles {
		if (r.Namespace == NamespaceLocal || r.Namespace == NamespaceOG || r.Namespace == "") && held[r.Name] == 1 {
			out = append(out, r)
		}
	}
	return out
}

// EnsureMachine finds the service user of that name, or creates
// it (audited), returning its id. The trusted lane's (#1370: the lab seed's
// simulated pusher); no route creates a service this way.
func (p *PG) EnsureMachine(ctx context.Context, actorID, name string) (string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("storage: begin ensure service: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var pid string
	err = tx.QueryRow(ctx, `select user_id from machine where name = $1`, name).Scan(&pid)
	if err == nil {
		return pid, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("storage: find service %q: %w", name, err)
	}
	if err := tx.QueryRow(ctx, `insert into users (kind) values ('machine') returning id`).Scan(&pid); err != nil {
		return "", fmt.Errorf("storage: create service user: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into machine (user_id, name) values ($1, $2)`, pid, name); err != nil {
		return "", fmt.Errorf("storage: create service %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "user", pid, nil, map[string]string{"kind": "machine", "name": name}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("storage: commit ensure service: %w", err)
	}
	return pid, nil
}

// SetMachineToken makes one token the service's only one, by its hash and
// non-secret prefix: every token it held before is revoked, audited, in the
// same transaction. False when no service has the name. The trusted lane's
// (#1370): the lab mints its simulated pusher a token on every run, as it
// does its outpost's.
func (p *PG) SetMachineToken(ctx context.Context, actorID, name string, hash []byte, prefix string) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("storage: begin set service token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var pid string
	err = tx.QueryRow(ctx, `select user_id from machine where name = $1`, name).Scan(&pid)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: find service %q: %w", name, err)
	}
	rows, err := tx.Query(ctx,
		`delete from credential where user_id = $1 and kind = 'bearer' and purpose = $2 `+revokedCols,
		pid, credentialPurposeToken)
	var revoked []revokedCredential
	if err == nil {
		revoked, err = scanRevoked(rows)
	}
	if err != nil {
		return false, fmt.Errorf("storage: revoke service tokens: %w", err)
	}
	if err := auditRevokedTokens(ctx, tx, actorID, revoked); err != nil {
		return false, err
	}
	var credID string
	if err := tx.QueryRow(ctx,
		`insert into credential (user_id, kind, secret_hash, prefix, purpose, description)
			values ($1, 'bearer', $2, $3, $4, 'minted by the lab for this run') returning id`,
		pid, hash, prefix, credentialPurposeToken).Scan(&credID); err != nil {
		return false, fmt.Errorf("storage: set service token: %w", err)
	}
	img := credentialAuditImage{UserID: pid, Prefix: prefix, Purpose: credentialPurposeToken, Description: "minted by the lab for this run"}
	if err := writeAuditRes(ctx, tx, actorID, "create", "credential", credID, nil, img); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("storage: commit set service token: %w", err)
	}
	return true, nil
}

// ErrRoleConflict refuses EnsureRole over a role of the same name that is
// official or holds other permissions: the trusted lane never rewrites a role.
var ErrRoleConflict = errors.New("storage: a role of that name exists with other permissions, or is official")

// EnsureRole creates an operator (non-official) role if no role has its name,
// audited in the same transaction, and otherwise leaves it: a role of that
// name holding exactly these permissions is the one wanted, and any other
// (an official role, or one an operator gave other permissions) is refused
// with ErrRoleConflict rather than overwritten. The trusted lane's (#1370:
// the lab's pusher role); the boot seed's official roles use UpsertRole.
func (p *PG) EnsureRole(ctx context.Context, actorID string, r Role) error {
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	if r.Inherits == nil {
		r.Inherits = []string{}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin ensure role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var official bool
	var perms []string
	// A role of the name in either tier answers (#1432): a shipped one is a
	// conflict, and an operator's is the one wanted only with these permissions.
	err = tx.QueryRow(ctx, `select official, permissions from role where name = $1 and namespace in ('local', 'og')
		order by official desc limit 1`, r.Name).Scan(&official, &perms)
	if err == nil {
		if official || !sameStrings(perms, r.Permissions) {
			return ErrRoleConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("storage: find role %q: %w", r.Name, err)
	}
	var roleID string
	if err := tx.QueryRow(ctx, `
		insert into role (name, official, permissions, inherits, label, description)
		values ($1, false, $2, $3, $4, $5) returning id`,
		r.Name, r.Permissions, r.Inherits, labelOrNull(r.Label), nullize(r.Description)).Scan(&roleID); err != nil {
		return fmt.Errorf("storage: create role %q: %w", r.Name, err)
	}
	img := map[string]any{"name": r.Name, "permissions": r.Permissions}
	if err := writeAuditRes(ctx, tx, actorID, "create", "role", roleID, nil, img); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit ensure role: %w", err)
	}
	return nil
}

// sameStrings reports two string sets equal, order aside.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		if seen[s] == 0 {
			return false
		}
		seen[s]--
	}
	return true
}
