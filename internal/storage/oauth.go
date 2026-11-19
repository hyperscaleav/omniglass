package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// OAuth 2.1 for MCP clients (#2048). A client registers itself, a person
// consents in the console, and the client exchanges the code for an agent
// token linked to it. The protocol checks (PKCE, redirect match, expiry) are
// the API's; this lane stores and consumes what it is given.

// OAuthClient is a client that registered itself, or one named by its client
// id metadata document (#2074): the name it declared and the exact redirect
// URIs a code may be sent to. MetadataURL is the document's URL, empty for a
// registered client, and FetchedAt when the document was last read.
type OAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	MetadataURL  string
	FetchedAt    time.Time
	CreatedAt    time.Time
}

// oauthClientCols selects an OAuthClient; scanOAuthClient reads it.
const oauthClientCols = `id, client_name, redirect_uris, coalesce(metadata_url, ''), coalesce(fetched_at, created_at), created_at`

func scanOAuthClient(row pgx.Row) (OAuthClient, error) {
	var c OAuthClient
	err := row.Scan(&c.ID, &c.Name, &c.RedirectURIs, &c.MetadataURL, &c.FetchedAt, &c.CreatedAt)
	return c, err
}

// OAuthCode is an authorization code between consent and exchange. Only its
// hash is stored; Roles are the roles the person approved for the client.
type OAuthCode struct {
	Hash          []byte
	ClientID      string
	UserID        string
	RedirectURI   string
	CodeChallenge string
	Resource      string
	Roles         []string
	ExpiresAt     time.Time
}

// OAuthTokenIssue is the agent token an exchange mints: its person, its
// client, its secret's hash and prefix, its expiry, and the agent's bounds,
// with the refresh token that renews it (#2073).
type OAuthTokenIssue struct {
	UserID           string
	ClientID         string
	SecretHash       []byte
	Prefix           string
	ExpiresAt        time.Time
	RefreshHash      []byte
	RefreshExpiresAt time.Time
	Agent            string
	AgentRoles       []string
	UserAgent        string
	ClientIP         string
}

// OAuthRefresh is a refresh-token grant: the presented token's hash, the
// client presenting it, and the access and refresh token that replace it.
type OAuthRefresh struct {
	TokenHash        []byte
	ClientID         string
	SecretHash       []byte
	Prefix           string
	ExpiresAt        time.Time
	RefreshHash      []byte
	RefreshExpiresAt time.Time
}

// OAuthRefreshOutcome is how a refresh-token grant ended.
type OAuthRefreshOutcome int

const (
	// OAuthRefreshed rotated the grant's tokens.
	OAuthRefreshed OAuthRefreshOutcome = iota
	// OAuthRefreshRefused matched no live refresh token for the client, and
	// changed nothing.
	OAuthRefreshRefused
	// OAuthRefreshReplayed matched a token the client already spent, so it
	// leaked: the grant was ended.
	OAuthRefreshReplayed
)

// oauthClientStale is how long a registered client that never reached a
// token is kept. Registration is open (RFC 7591), so each one prunes the
// clients nobody consented to.
const oauthClientStale = 24 * time.Hour

// RegisterOAuthClient stores a self-registered client and prunes the stale
// ones: older than a day with no token and no code outstanding.
func (p *PG) RegisterOAuthClient(ctx context.Context, name string, redirectURIs []string) (OAuthClient, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return OAuthClient{}, fmt.Errorf("storage: begin register oauth client: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		delete from oauth_client c
		where coalesce(c.fetched_at, c.created_at) < now() - $1::interval
			and not exists (select 1 from credential where oauth_client_id = c.id)
			and not exists (select 1 from oauth_code where client_id = c.id and expires_at > now())`,
		oauthClientStale.String()); err != nil {
		return OAuthClient{}, fmt.Errorf("storage: prune oauth clients: %w", err)
	}
	c := OAuthClient{Name: name, RedirectURIs: redirectURIs}
	if err := tx.QueryRow(ctx,
		`insert into oauth_client (client_name, redirect_uris) values ($1, $2) returning id, created_at`,
		name, redirectURIs).Scan(&c.ID, &c.CreatedAt); err != nil {
		return OAuthClient{}, fmt.Errorf("storage: register oauth client: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return OAuthClient{}, fmt.Errorf("storage: commit register oauth client: %w", err)
	}
	return c, nil
}

// GetOAuthClient returns a registered client, false when there is none (a
// malformed id included).
func (p *PG) GetOAuthClient(ctx context.Context, id string) (OAuthClient, bool, error) {
	c, err := scanOAuthClient(p.pool.QueryRow(ctx, `select `+oauthClientCols+` from oauth_client where id = $1`, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows), isInvalidText(err):
		return OAuthClient{}, false, nil
	case err != nil:
		return OAuthClient{}, false, fmt.Errorf("storage: get oauth client: %w", err)
	}
	return c, true, nil
}

// GetOAuthClientByURL returns the client a metadata document URL names, false
// when its document has not been read.
func (p *PG) GetOAuthClientByURL(ctx context.Context, metadataURL string) (OAuthClient, bool, error) {
	c, err := scanOAuthClient(p.pool.QueryRow(ctx, `select `+oauthClientCols+` from oauth_client where metadata_url = $1`, metadataURL))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return OAuthClient{}, false, nil
	case err != nil:
		return OAuthClient{}, false, fmt.Errorf("storage: get oauth client by url: %w", err)
	}
	return c, true, nil
}

// UpsertOAuthMetadataClient stores what a client's metadata document says,
// keyed by its URL: the first read creates the client, a later one replaces
// its name and redirect URIs and marks it fetched now. Its id is stable, so
// its codes and tokens stay linked across reads.
func (p *PG) UpsertOAuthMetadataClient(ctx context.Context, metadataURL, name string, redirectURIs []string) (OAuthClient, error) {
	c, err := scanOAuthClient(p.pool.QueryRow(ctx, `
		insert into oauth_client (client_name, redirect_uris, metadata_url, fetched_at) values ($1, $2, $3, now())
		on conflict (metadata_url) where metadata_url is not null
			do update set client_name = excluded.client_name, redirect_uris = excluded.redirect_uris, fetched_at = now()
		returning `+oauthClientCols, name, redirectURIs, metadataURL))
	if err != nil {
		return OAuthClient{}, fmt.Errorf("storage: upsert oauth metadata client: %w", err)
	}
	return c, nil
}

// CreateOAuthCode stores a code a person's consent produced, sweeping the
// expired ones.
func (p *PG) CreateOAuthCode(ctx context.Context, c OAuthCode) error {
	if _, err := p.pool.Exec(ctx, `delete from oauth_code where expires_at <= now()`); err != nil {
		return fmt.Errorf("storage: sweep oauth codes: %w", err)
	}
	if _, err := p.pool.Exec(ctx, `
		insert into oauth_code (code_hash, client_id, user_id, redirect_uri, code_challenge, resource, roles, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		c.Hash, c.ClientID, c.UserID, c.RedirectURI, c.CodeChallenge, c.Resource, c.Roles, c.ExpiresAt); err != nil {
		return fmt.Errorf("storage: create oauth code: %w", err)
	}
	return nil
}

// ConsumeOAuthCode deletes a code and returns it, false when there is none or
// it has expired. A code is spent by any attempt to use it, right or wrong.
func (p *PG) ConsumeOAuthCode(ctx context.Context, hash []byte) (OAuthCode, bool, error) {
	c := OAuthCode{Hash: hash}
	err := p.pool.QueryRow(ctx, `
		delete from oauth_code where code_hash = $1
		returning client_id, user_id, redirect_uri, code_challenge, resource, roles, expires_at`, hash).
		Scan(&c.ClientID, &c.UserID, &c.RedirectURI, &c.CodeChallenge, &c.Resource, &c.Roles, &c.ExpiresAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return OAuthCode{}, false, nil
	case err != nil:
		return OAuthCode{}, false, fmt.Errorf("storage: consume oauth code: %w", err)
	}
	if !c.ExpiresAt.After(time.Now()) {
		return OAuthCode{}, false, nil
	}
	return c, true, nil
}

// IssueOAuthToken mints the agent token an exchange produces, accepted on the
// MCP channel only and linked to its client. The client's earlier tokens for
// the same person go in the same transaction, so a client holds one token per
// person and revoking that token signs the client out. Both are audited as
// the person's.
func (p *PG) IssueOAuthToken(ctx context.Context, spec OAuthTokenIssue) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin issue oauth token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx,
		`delete from credential where user_id = $1 and oauth_client_id = $2 and kind = 'bearer' `+revokedCols,
		spec.UserID, spec.ClientID)
	if err != nil {
		return fmt.Errorf("storage: replace oauth token: %w", err)
	}
	revoked, err := scanRevoked(rows)
	if err != nil {
		return err
	}
	if err := auditRevokedTokens(ctx, tx, spec.UserID, revoked); err != nil {
		return err
	}
	channels := []string{string(AuditChannelMCP)}
	var credID string
	if err := tx.QueryRow(ctx,
		`insert into credential (user_id, kind, secret_hash, prefix, purpose, expires_at, description, user_agent, client_ip, agent, agent_roles, agent_channels, oauth_client_id)
			values ($1, 'bearer', $2, $3, $4, $5, $6, nullif($7,''), nullif($8,''), $9, $10, $11, $12)
			returning id`,
		spec.UserID, spec.SecretHash, spec.Prefix, credentialPurposeToken, spec.ExpiresAt, "OAuth: "+spec.Agent,
		spec.UserAgent, spec.ClientIP, spec.Agent, emptySliceNil(spec.AgentRoles), channels, spec.ClientID).Scan(&credID); err != nil {
		return fmt.Errorf("storage: issue oauth token: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into oauth_refresh (token_hash, credential_id, expires_at) values ($1, $2, $3)`,
		spec.RefreshHash, credID, spec.RefreshExpiresAt); err != nil {
		return fmt.Errorf("storage: issue oauth refresh token: %w", err)
	}
	exp := spec.ExpiresAt
	img := credentialAuditImage{UserID: spec.UserID, Prefix: spec.Prefix, Purpose: credentialPurposeToken, Description: "OAuth: " + spec.Agent,
		ExpiresAt: &exp, Agent: spec.Agent, AgentRoles: emptySliceNil(spec.AgentRoles), AgentChannels: channels}
	if err := writeAuditRes(ctx, tx, spec.UserID, "create", "credential", credID, nil, img); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit issue oauth token: %w", err)
	}
	return nil
}

// RefreshOAuthToken spends a client's refresh token and rotates its grant in
// place: the same credential row takes the new access token's hash, prefix and
// expiry, and the new refresh token replaces the spent one. The spent token is
// kept until it would have expired, so presenting it again is recognized as a
// leak and ends the grant (OAuth 2.1 refresh-token rotation). A token another
// client presents, an expired one, or one whose person is disabled is refused
// without being spent. The rotation and a replay's revoke are audited as the
// person's, taken by the client as their agent.
func (p *PG) RefreshOAuthToken(ctx context.Context, spec OAuthRefresh) (OAuthRefreshOutcome, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return OAuthRefreshRefused, fmt.Errorf("storage: begin refresh oauth token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var credID, clientName string
	err = tx.QueryRow(ctx, `
		update oauth_refresh r set spent_at = now()
		from credential c
		join users pr on pr.id = c.user_id
		join oauth_client o on o.id = c.oauth_client_id
		where r.token_hash = $1 and r.spent_at is null and r.expires_at > now()
			and c.id = r.credential_id and c.oauth_client_id::text = $2 and pr.active
		returning c.id, o.client_name`, spec.TokenHash, spec.ClientID).Scan(&credID, &clientName)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return p.refuseOrEndGrant(ctx, tx, spec)
	case err != nil:
		return OAuthRefreshRefused, fmt.Errorf("storage: spend oauth refresh token: %w", err)
	}
	ctx = WithAgent(ctx, clientName)
	var old, img credentialAuditImage
	var oldExp *time.Time
	var description, agent *string
	if err := tx.QueryRow(ctx, `
		select user_id, prefix, expires_at, description, agent, agent_roles, agent_channels
		from credential where id = $1 for update`, credID).
		Scan(&old.UserID, &old.Prefix, &oldExp, &description, &agent, &old.AgentRoles, &old.AgentChannels); err != nil {
		return OAuthRefreshRefused, fmt.Errorf("storage: read oauth credential: %w", err)
	}
	old.Purpose, old.ExpiresAt = credentialPurposeToken, oldExp
	if description != nil {
		old.Description = *description
	}
	if agent != nil {
		old.Agent = *agent
	}
	if _, err := tx.Exec(ctx, `update credential set secret_hash = $2, prefix = $3, expires_at = $4 where id = $1`,
		credID, spec.SecretHash, spec.Prefix, spec.ExpiresAt); err != nil {
		return OAuthRefreshRefused, fmt.Errorf("storage: rotate oauth token: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from oauth_refresh where credential_id = $1 and expires_at <= now()`, credID); err != nil {
		return OAuthRefreshRefused, fmt.Errorf("storage: prune oauth refresh tokens: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into oauth_refresh (token_hash, credential_id, expires_at) values ($1, $2, $3)`,
		spec.RefreshHash, credID, spec.RefreshExpiresAt); err != nil {
		return OAuthRefreshRefused, fmt.Errorf("storage: issue oauth refresh token: %w", err)
	}
	img = old
	exp := spec.ExpiresAt
	img.Prefix, img.ExpiresAt = spec.Prefix, &exp
	if err := writeAuditRes(ctx, tx, old.UserID, "update", "credential", credID, old, img); err != nil {
		return OAuthRefreshRefused, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OAuthRefreshRefused, fmt.Errorf("storage: commit refresh oauth token: %w", err)
	}
	return OAuthRefreshed, nil
}

// refuseOrEndGrant settles a refresh that spent nothing: a token this client
// already spent ends its grant (the credential and, by cascade, its refresh
// chain), anything else is refused unchanged.
func (p *PG) refuseOrEndGrant(ctx context.Context, tx pgx.Tx, spec OAuthRefresh) (OAuthRefreshOutcome, error) {
	var credID, clientName string
	err := tx.QueryRow(ctx, `
		select c.id, o.client_name
		from oauth_refresh r
		join credential c on c.id = r.credential_id
		join oauth_client o on o.id = c.oauth_client_id
		where r.token_hash = $1 and r.spent_at is not null and c.oauth_client_id::text = $2`,
		spec.TokenHash, spec.ClientID).Scan(&credID, &clientName)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return OAuthRefreshRefused, nil
	case err != nil:
		return OAuthRefreshRefused, fmt.Errorf("storage: look up spent oauth refresh token: %w", err)
	}
	rows, err := tx.Query(ctx, `delete from credential where id = $1 `+revokedCols, credID)
	if err != nil {
		return OAuthRefreshRefused, fmt.Errorf("storage: end oauth grant: %w", err)
	}
	revoked, err := scanRevoked(rows)
	if err != nil {
		return OAuthRefreshRefused, err
	}
	for _, r := range revoked {
		if err := auditRevokedTokens(WithAgent(ctx, clientName), tx, r.img.UserID, []revokedCredential{r}); err != nil {
			return OAuthRefreshRefused, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return OAuthRefreshRefused, fmt.Errorf("storage: commit end oauth grant: %w", err)
	}
	return OAuthRefreshReplayed, nil
}

// isInvalidText reports a malformed uuid, which matches no row.
func isInvalidText(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
