package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/settings"
)

// Single sign-on through an OIDC identity provider (#2362, ADR-2361). An
// identity at the provider is an oidc credential, a link, whose identifier is
// iss|sub; a sign-in through it mints the ordinary session credential with
// via_link naming it. The link keeps the provider's latest refresh token,
// sealed, which the re-check sweep redeems: a refusal revokes every credential
// through the link, and a link with no accepted refresh past its grace is
// refused at authentication.

// SSOProviderDefault is the one provider's name: the flat sso keys.
const SSOProviderDefault = "default"

// SSOProvider is a configured identity provider, its client secret unsealed.
// The platform's own read: it never leaves the process.
type SSOProvider struct {
	Name         string
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	GroupsClaim  string
	Recheck      time.Duration
	Grace        time.Duration
}

// ErrSSOSecretInvalid is an sso client_secret that is not a platform
// oauth2-client secret, one with no client_id, or one that no longer unseals.
var ErrSSOSecretInvalid = errors.New("storage: the sso client secret is invalid")

// ErrIdentityNotLinked is an identity no person is linked to.
var ErrIdentityNotLinked = errors.New("storage: the identity is not linked to a person")

// ErrIdentityLinked is an identity already linked to a person.
var ErrIdentityLinked = errors.New("storage: the identity is already linked")

// ErrIdentityLinkNotFound is a link that is not the named person's.
var ErrIdentityLinkNotFound = errors.New("storage: identity link not found")

// ErrOIDCStateNotFound is a sign-in state that is unknown, expired or spent.
var ErrOIDCStateNotFound = errors.New("storage: sign-in state not found")

// ssoScopes splits the scopes setting, openid always first.
func ssoScopes(s string) []string {
	out := []string{"openid"}
	for _, f := range strings.Fields(s) {
		if f != "openid" {
			out = append(out, f)
		}
	}
	return out
}

// SSOProvider reads the named provider; nil when single sign-on is off (no
// issuer) or the name is not a provider. A configured provider whose secret
// is broken is ErrSSOSecretInvalid with the reason.
func (p *PG) SSOProvider(ctx context.Context, name string) (*SSOProvider, error) {
	if name != SSOProviderDefault {
		return nil, nil
	}
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	c := s.SSO
	if c.Issuer == "" {
		return nil, nil
	}
	sp := &SSOProvider{Name: name, Issuer: c.Issuer, Scopes: ssoScopes(c.Scopes), GroupsClaim: c.GroupsClaim,
		Recheck: time.Duration(c.RecheckMinutes) * time.Minute, Grace: time.Duration(c.RecheckGraceMinutes) * time.Minute}
	if c.ClientSecret == "" {
		return nil, fmt.Errorf("%w: client_secret is not set", ErrSSOSecretInvalid)
	}
	fields, ok := p.envPlatformFields(c.ClientSecret, "oauth2-client")
	if !ok {
		id, err := platformSecretOfType(ctx, p.pool, c.ClientSecret, "oauth2-client")
		if errors.Is(err, ErrSecretNotFound) {
			return nil, fmt.Errorf("%w: client_secret %q is not a platform oauth2-client secret", ErrSSOSecretInvalid, c.ClientSecret)
		}
		if err != nil {
			return nil, err
		}
		if fields, err = p.unsealSecretByID(ctx, p.pool, id); err != nil {
			return nil, fmt.Errorf("%w: client_secret %q no longer unseals", ErrSSOSecretInvalid, c.ClientSecret)
		}
	}
	if fields["client_id"] == "" || fields["client_secret"] == "" {
		return nil, fmt.Errorf("%w: client_secret %q needs both its client_id and client_secret fields", ErrSSOSecretInvalid, c.ClientSecret)
	}
	sp.ClientID, sp.ClientSecret = fields["client_id"], fields["client_secret"]
	return sp, nil
}

// SSOProviders names the providers a sign-in page offers: the default one
// when an issuer is set, else none.
func (p *PG) SSOProviders(ctx context.Context) ([]string, error) {
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	if s.SSO.Issuer == "" {
		return nil, nil
	}
	return []string{SSOProviderDefault}, nil
}

// MergePatchSSOSettings is MergePatchSettingBinding on the platform sso
// namespace with the client secret's binding judged, as omnibot's model key
// is: when the RESULTING settings bind a client_secret and it or the issuer
// changed, the caller must be able to bind that secret, since every sign-in
// sends it to the issuer's token endpoint.
func (p *PG) MergePatchSSOSettings(ctx context.Context, actorID string, patch map[string]any, use SecretUse) (*SettingBinding, error) {
	return p.mergePatchSettingBinding(ctx, actorID, "platform", "sso", patch, func(tx pgx.Tx, before settings.Settings) error {
		after, err := p.platformSettings(ctx, tx)
		if err != nil {
			return err
		}
		b, a := before.SSO, after.SSO
		if a.ClientSecret == "" || (a.ClientSecret == b.ClientSecret && a.Issuer == b.Issuer) {
			return nil
		}
		return p.checkSSOSecret(ctx, tx, a.ClientSecret, use)
	})
}

// checkSSOSecret judges binding client_secret: a PLATFORM oauth2-client
// secret, by bare name, the caller may bind.
func (p *PG) checkSSOSecret(ctx context.Context, q querier, ref string, use SecretUse) error {
	if isUUID(ref) || strings.Contains(ref, "/") {
		return fmt.Errorf("%w: client_secret names a platform secret by its bare name", ErrSSOSecretInvalid)
	}
	if e, found, err := p.checkEnvBind(ref, use); found {
		if err != nil {
			return err
		}
		if e.typ.Name != "oauth2-client" {
			return fmt.Errorf("%w: secret %q is not an oauth2-client secret", ErrSSOSecretInvalid, ref)
		}
		return nil
	}
	row, _, err := p.secretRowForAction(ctx, q, ref, use.Read, use.Bind, use.CanAdmin, true)
	if err != nil {
		return err
	}
	if row.ownerKind != "platform" {
		return fmt.Errorf("%w: secret %q is not a platform secret", ErrSSOSecretInvalid, ref)
	}
	if _, err := platformSecretOfType(ctx, q, ref, "oauth2-client"); errors.Is(err, ErrSecretNotFound) {
		return fmt.Errorf("%w: secret %q is not an oauth2-client secret", ErrSSOSecretInvalid, ref)
	} else if err != nil {
		return err
	}
	return nil
}

// OIDCState is one sign-in between the redirect and the callback. Only
// hashes are kept. LinkUserID and LinkSessionHash mark a self-link flow,
// bound to the session that started it.
type OIDCState struct {
	StateHash       []byte
	BindingHash     []byte
	Provider        string
	LinkUserID      string
	LinkSessionHash []byte
	ExpiresAt       time.Time
}

// CreateOIDCState stores a sign-in's state, clearing the expired ones.
func (p *PG) CreateOIDCState(ctx context.Context, s OIDCState) error {
	if _, err := p.pool.Exec(ctx, `delete from oidc_state where expires_at < now()`); err != nil {
		return fmt.Errorf("storage: clear expired sign-in states: %w", err)
	}
	if _, err := p.pool.Exec(ctx, `insert into oidc_state (state_hash, binding_hash, provider, link_user_id, link_session_hash, expires_at)
		values ($1, $2, $3, $4, $5, $6)`, s.StateHash, s.BindingHash, s.Provider, nullize(s.LinkUserID), s.LinkSessionHash, s.ExpiresAt); err != nil {
		return fmt.Errorf("storage: store sign-in state: %w", err)
	}
	return nil
}

// ConsumeOIDCState spends a sign-in's state: once, before it expires. A
// replayed, expired or unknown state is ErrOIDCStateNotFound.
func (p *PG) ConsumeOIDCState(ctx context.Context, stateHash []byte) (*OIDCState, error) {
	s := OIDCState{StateHash: stateHash}
	var linkUser *string
	err := p.pool.QueryRow(ctx, `delete from oidc_state where state_hash = $1
		returning binding_hash, provider, link_user_id::text, link_session_hash, expires_at`, stateHash).
		Scan(&s.BindingHash, &s.Provider, &linkUser, &s.LinkSessionHash, &s.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOIDCStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: spend sign-in state: %w", err)
	}
	if !s.ExpiresAt.After(time.Now()) {
		return nil, ErrOIDCStateNotFound
	}
	if linkUser != nil {
		s.LinkUserID = *linkUser
	}
	return &s, nil
}

// IdentityLink is one person's link to an identity at a provider.
type IdentityLink struct {
	ID         string
	UserID     string
	Identifier string
	// LinkedBy is who made the link (the person themselves, or an admin);
	// empty once that user is purged.
	LinkedBy          string
	LinkedByUsername  string
	CreatedAt         time.Time
	LastUsedAt        *time.Time
	RefreshAcceptedAt *time.Time
}

// identityLinkImage is a link's audit image: who it is for, the identity,
// and who made it. The refresh token never reaches the trail.
type identityLinkImage struct {
	UserID     string `json:"user_id"`
	Identifier string `json:"identifier"`
	LinkedBy   string `json:"linked_by,omitempty"`
}

// LinkIdentity links a person to an identity (iss|sub), audited with the
// actor, who is recorded as the link's maker. The API judges who may: this
// lane stores what it is given. An identity linked to anyone already is
// ErrIdentityLinked; a target that is not a person ErrUserNotFound.
func (p *PG) LinkIdentity(ctx context.Context, actorID, userID, identifier string) (*IdentityLink, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin link identity: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tx.QueryRow(ctx, `select 1 from person where user_id = $1`, userID).Scan(new(int)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("storage: link identity lookup: %w", err)
	}
	l := IdentityLink{UserID: userID, Identifier: identifier, LinkedBy: actorID}
	err = tx.QueryRow(ctx, `insert into credential (user_id, kind, identifier, prefix, linked_by)
		values ($1, 'oidc', $2, '', $3) returning id, created_at`, userID, identifier, nullize(actorID)).Scan(&l.ID, &l.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrIdentityLinked
	}
	if err != nil {
		return nil, fmt.Errorf("storage: link identity: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "link", "identity_link", l.ID, nil,
		identityLinkImage{UserID: userID, Identifier: identifier, LinkedBy: actorID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit link identity: %w", err)
	}
	return &l, nil
}

// UnlinkIdentity removes one of a person's links, audited; every session and
// token through it ends with it (via_link cascades).
func (p *PG) UnlinkIdentity(ctx context.Context, actorID, userID, linkID string) error {
	if !isUUID(linkID) {
		return ErrIdentityLinkNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin unlink identity: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var img identityLinkImage
	var by *string
	err = tx.QueryRow(ctx, `delete from credential where id = $1 and user_id = $2 and kind = 'oidc'
		returning user_id::text, identifier, linked_by::text`, linkID, userID).Scan(&img.UserID, &img.Identifier, &by)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIdentityLinkNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: unlink identity: %w", err)
	}
	if by != nil {
		img.LinkedBy = *by
	}
	if err := writeAuditRes(ctx, tx, actorID, "unlink", "identity_link", linkID, img, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit unlink identity: %w", err)
	}
	return nil
}

// ListIdentityLinks lists a person's links, oldest first.
func (p *PG) ListIdentityLinks(ctx context.Context, userID string) ([]IdentityLink, error) {
	rows, err := p.pool.Query(ctx, `select c.id, c.user_id, c.identifier, coalesce(c.linked_by::text, ''), coalesce(pp.username, ''),
			c.created_at, c.last_used_at, c.refresh_accepted_at
		from credential c left join person pp on pp.user_id = c.linked_by
		where c.user_id = $1 and c.kind = 'oidc' order by c.created_at, c.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("storage: list identity links: %w", err)
	}
	defer rows.Close()
	out := []IdentityLink{}
	for rows.Next() {
		var l IdentityLink
		if err := rows.Scan(&l.ID, &l.UserID, &l.Identifier, &l.LinkedBy, &l.LinkedByUsername, &l.CreatedAt, &l.LastUsedAt, &l.RefreshAcceptedAt); err != nil {
			return nil, fmt.Errorf("storage: scan identity link: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SSOSignIn is one sign-in through a link: the identity the provider vouched
// for, the refresh token it issued (empty when none), and the session to mint.
type SSOSignIn struct {
	Identifier   string
	RefreshToken string
	// Grace is how long the link stays good with no accepted refresh.
	Grace       time.Duration
	SessionHash []byte
	Prefix      string
	ExpiresAt   time.Time
	// GraceExpiresAt ends the session instead when the link holds no
	// refresh token after this sign-in, since nothing will re-check it.
	GraceExpiresAt time.Time
	UserAgent      string
	ClientIP       string
}

// refreshAAD binds a sealed refresh token to the link it was sealed for.
func refreshAAD(linkID string) []byte { return []byte("credential|" + linkID + "|refresh_token") }

func (p *PG) sealRefresh(ctx context.Context, linkID, token string) ([]byte, error) {
	if token == "" {
		return nil, nil
	}
	env, err := secret.Seal(ctx, p.secret, []byte(token), refreshAAD(linkID))
	if err != nil {
		return nil, fmt.Errorf("storage: seal refresh token: %w", err)
	}
	return json.Marshal(env)
}

// SignInByIdentity resolves the person linked to an identity and, in one
// transaction, keeps the provider's refresh token on the link, mints the
// session through it, and writes the login auth event with the method. An
// unlinked identity is ErrIdentityNotLinked; a disabled or archived person
// ErrAccountDisabled (the user is returned, so the denial is attributed).
// The session's expiry is returned for the cookie.
func (p *PG) SignInByIdentity(ctx context.Context, in SSOSignIn) (*User, time.Time, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("storage: begin sso sign-in: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var linkID string
	var u User
	var active, archived bool
	err = tx.QueryRow(ctx, `select c.id, u.id, u.kind, u.active, u.archived_at is not null
		from credential c join users u on u.id = c.user_id
		where c.kind = 'oidc' and c.identifier = $1 for update of c`, in.Identifier).Scan(&linkID, &u.ID, &u.Kind, &active, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, time.Time{}, ErrIdentityNotLinked
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("storage: resolve identity: %w", err)
	}
	if !active || archived {
		return &u, time.Time{}, ErrAccountDisabled
	}
	sealed, err := p.sealRefresh(ctx, linkID, in.RefreshToken)
	if err != nil {
		return nil, time.Time{}, err
	}
	// A provider may issue a refresh token only on first consent (Google
	// does), so a sign-in that brings none keeps the one the link holds.
	var holdsRefresh bool
	if err := tx.QueryRow(ctx, `update credential set refresh_sealed = coalesce($2, refresh_sealed),
			refresh_accepted_at = case when $2::bytea is null then refresh_accepted_at else now() end,
			recheck_grace_until = now() + make_interval(secs => $3), last_used_at = now() where id = $1
			returning refresh_sealed is not null`,
		linkID, sealed, in.Grace.Seconds()).Scan(&holdsRefresh); err != nil {
		return nil, time.Time{}, fmt.Errorf("storage: keep the link's refresh token: %w", err)
	}
	expiresAt := in.ExpiresAt
	if !holdsRefresh && !in.GraceExpiresAt.IsZero() {
		expiresAt = in.GraceExpiresAt
	}
	if _, err := tx.Exec(ctx, `insert into credential (user_id, kind, secret_hash, prefix, purpose, expires_at, user_agent, client_ip, via_link)
		values ($1, 'bearer', $2, $3, 'session', $4, nullif($5,''), nullif($6,''), $7)`,
		u.ID, in.SessionHash, in.Prefix, expiresAt, in.UserAgent, in.ClientIP, linkID); err != nil {
		return nil, time.Time{}, fmt.Errorf("storage: issue sso session: %w", err)
	}
	if err := writeAuthEventTx(ctx, tx, u.ID, "login", map[string]string{"method": "oidc", "identifier": in.Identifier}); err != nil {
		return nil, time.Time{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, time.Time{}, fmt.Errorf("storage: commit sso sign-in: %w", err)
	}
	if err := p.loadUser(ctx, &u); err != nil {
		return nil, time.Time{}, err
	}
	return &u, expiresAt, nil
}

// writeAuthEventTx is WriteAuthEvent inside a transaction, with a detail
// image (the sign-in method) on the row's new.
func writeAuthEventTx(ctx context.Context, tx pgx.Tx, actorID, verb string, detail any) error {
	b, err := auditJSON(detail)
	if err != nil {
		return err
	}
	channel, client := auditChannelFrom(ctx)
	if _, err := tx.Exec(ctx, `
		insert into audit_log (actor_user_id, real_actor_user_id, actor_username, real_actor_username, verb, resource, new, channel, client, agent)
		values ($1, $2, `+userIdentSQL("$1")+`, `+userIdentSQL("$2")+`, $3, 'auth', $4, $5, $6, $7)`,
		nullize(actorID), nullize(realActorFrom(ctx)), verb, b, channel, client, agentFrom(ctx)); err != nil {
		return fmt.Errorf("storage: write auth event: %w", err)
	}
	return nil
}

// LinkRecheck is one link the re-check sweep redeems: its refresh token,
// unsealed, in process only.
type LinkRecheck struct {
	LinkID       string
	UserID       string
	Identifier   string
	RefreshToken string
}

// LinksToRecheck lists the links of issuer that hold a refresh token and have
// a live credential through them, their tokens unsealed. A token that no
// longer unseals is listed with an empty one, which the sweep treats as a
// refusal.
func (p *PG) LinksToRecheck(ctx context.Context, issuer string) ([]LinkRecheck, error) {
	rows, err := p.pool.Query(ctx, `select l.id, l.user_id, l.identifier, l.refresh_sealed
		from credential l
		where l.kind = 'oidc' and l.refresh_sealed is not null and starts_with(l.identifier, $1 || '|')
		  and exists (select 1 from credential c where c.via_link = l.id and (c.expires_at is null or c.expires_at > now()))
		order by l.id`, issuer)
	if err != nil {
		return nil, fmt.Errorf("storage: list links to re-check: %w", err)
	}
	type sealedRow struct {
		LinkRecheck
		sealed []byte
	}
	var raw []sealedRow
	for rows.Next() {
		var r sealedRow
		if err := rows.Scan(&r.LinkID, &r.UserID, &r.Identifier, &r.sealed); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan link to re-check: %w", err)
		}
		raw = append(raw, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]LinkRecheck, 0, len(raw))
	for _, r := range raw {
		var env secret.Envelope
		if json.Unmarshal(r.sealed, &env) == nil {
			if b, err := secret.Open(ctx, p.secret, env, refreshAAD(r.LinkID)); err == nil {
				r.RefreshToken = string(b)
			}
		}
		out = append(out, r.LinkRecheck)
	}
	return out, nil
}

// AcceptLinkRefresh records a refresh the provider accepted: the rotated
// token (the old one kept when the provider rotated none) and a fresh grace.
func (p *PG) AcceptLinkRefresh(ctx context.Context, linkID, rotated string, grace time.Duration) error {
	sealed, err := p.sealRefresh(ctx, linkID, rotated)
	if err != nil {
		return err
	}
	if _, err := p.pool.Exec(ctx, `update credential set refresh_sealed = coalesce($2, refresh_sealed), refresh_accepted_at = now(),
			recheck_grace_until = now() + make_interval(secs => $3) where id = $1 and kind = 'oidc'`, linkID, sealed, grace.Seconds()); err != nil {
		return fmt.Errorf("storage: accept link refresh: %w", err)
	}
	return nil
}

// RevokeLink ends access through a link the provider refused: in one
// transaction it deletes every session and API token through it, clears its
// refresh token, and audits the revoke (the system's, no actor) with how many
// credentials ended. The link itself stays, so the person signs in again only
// if the provider lets them.
func (p *PG) RevokeLink(ctx context.Context, linkID, reason string) (int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: begin revoke link: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var img identityLinkImage
	err = tx.QueryRow(ctx, `update credential set refresh_sealed = null where id = $1 and kind = 'oidc'
		returning user_id::text, identifier`, linkID).Scan(&img.UserID, &img.Identifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrIdentityLinkNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("storage: revoke link: %w", err)
	}
	rows, err := tx.Query(ctx, `delete from credential where via_link = $1 `+revokedCols, linkID)
	if err != nil {
		return 0, fmt.Errorf("storage: revoke credentials through link: %w", err)
	}
	revoked, err := scanRevoked(rows)
	if err != nil {
		return 0, err
	}
	if err := auditRevokedTokens(ctx, tx, "", revoked); err != nil {
		return 0, err
	}
	if err := writeAuditRes(ctx, tx, "", "revoke", "identity_link", linkID, img,
		map[string]any{"reason": reason, "credentials_revoked": len(revoked)}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("storage: commit revoke link: %w", err)
	}
	return len(revoked), nil
}

// dropForeignLinksOfOwners deletes, audited, every link someone else made for
// a person who now holds owner over the whole fleet, directly or through a
// group (ADR-2361 point 2): the takeover guard judged the target when the
// link was made, so a link an admin chose must not outlive the target
// becoming an owner. Run in the transaction of every write that can make an
// owner: a grant, a group grant, a group membership.
func dropForeignLinksOfOwners(ctx context.Context, tx pgx.Tx, actorID string) error {
	rows, err := tx.Query(ctx, `delete from credential c
		where c.kind = 'oidc' and c.linked_by is distinct from c.user_id
		  and exists (select 1 from user_grant g
		      where g.role_id = (select id from role where namespace = 'og' and name = 'owner') and g.scope_kind = 'all'
		        and (g.user_id = c.user_id or g.group_id in (select group_id from user_group_member m where m.user_id = c.user_id)))
		returning c.id, c.user_id::text, c.identifier, coalesce(c.linked_by::text, '')`)
	if err != nil {
		return fmt.Errorf("storage: drop links of new owners: %w", err)
	}
	type dropped struct {
		linkID string
		img    identityLinkImage
	}
	var ds []dropped
	for rows.Next() {
		var d dropped
		if err := rows.Scan(&d.linkID, &d.img.UserID, &d.img.Identifier, &d.img.LinkedBy); err != nil {
			rows.Close()
			return fmt.Errorf("storage: scan dropped link: %w", err)
		}
		ds = append(ds, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, d := range ds {
		if err := writeAuditRes(ctx, tx, actorID, "unlink", "identity_link", d.linkID, d.img,
			map[string]string{"reason": "the person became an owner; an owner links their own identity"}); err != nil {
			return err
		}
	}
	return nil
}
