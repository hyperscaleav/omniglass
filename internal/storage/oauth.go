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

// OAuthClient is a client that registered itself: the name it declared and
// the exact redirect URIs a code may be sent to.
type OAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	CreatedAt    time.Time
}

// OAuthCode is an authorization code between consent and exchange. Only its
// hash is stored; Roles are the roles the person approved for the client.
type OAuthCode struct {
	Hash          []byte
	ClientID      string
	PrincipalID   string
	RedirectURI   string
	CodeChallenge string
	Resource      string
	Roles         []string
	ExpiresAt     time.Time
}

// OAuthTokenIssue is the agent token an exchange mints: its person, its
// client, its secret's hash and prefix, its expiry, and the agent's bounds.
type OAuthTokenIssue struct {
	PrincipalID string
	ClientID    string
	SecretHash  []byte
	Prefix      string
	ExpiresAt   time.Time
	Agent       string
	AgentRoles  []string
	UserAgent   string
	ClientIP    string
}

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
		where c.created_at < now() - $1::interval
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
	var c OAuthClient
	err := p.pool.QueryRow(ctx,
		`select id, client_name, redirect_uris, created_at from oauth_client where id = $1`, id).
		Scan(&c.ID, &c.Name, &c.RedirectURIs, &c.CreatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows), isInvalidText(err):
		return OAuthClient{}, false, nil
	case err != nil:
		return OAuthClient{}, false, fmt.Errorf("storage: get oauth client: %w", err)
	}
	return c, true, nil
}

// CreateOAuthCode stores a code a person's consent produced, sweeping the
// expired ones.
func (p *PG) CreateOAuthCode(ctx context.Context, c OAuthCode) error {
	if _, err := p.pool.Exec(ctx, `delete from oauth_code where expires_at <= now()`); err != nil {
		return fmt.Errorf("storage: sweep oauth codes: %w", err)
	}
	if _, err := p.pool.Exec(ctx, `
		insert into oauth_code (code_hash, client_id, principal_id, redirect_uri, code_challenge, resource, roles, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		c.Hash, c.ClientID, c.PrincipalID, c.RedirectURI, c.CodeChallenge, c.Resource, c.Roles, c.ExpiresAt); err != nil {
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
		returning client_id, principal_id, redirect_uri, code_challenge, resource, roles, expires_at`, hash).
		Scan(&c.ClientID, &c.PrincipalID, &c.RedirectURI, &c.CodeChallenge, &c.Resource, &c.Roles, &c.ExpiresAt)
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
		`delete from credential where principal_id = $1 and oauth_client_id = $2 and kind = 'bearer' `+revokedCols,
		spec.PrincipalID, spec.ClientID)
	if err != nil {
		return fmt.Errorf("storage: replace oauth token: %w", err)
	}
	revoked, err := scanRevoked(rows)
	if err != nil {
		return err
	}
	if err := auditRevokedTokens(ctx, tx, spec.PrincipalID, revoked); err != nil {
		return err
	}
	channels := []string{string(AuditChannelMCP)}
	var credID string
	if err := tx.QueryRow(ctx,
		`insert into credential (principal_id, kind, secret_hash, prefix, purpose, expires_at, description, user_agent, client_ip, agent, agent_roles, agent_channels, oauth_client_id)
			values ($1, 'bearer', $2, $3, $4, $5, $6, nullif($7,''), nullif($8,''), $9, $10, $11, $12)
			returning id`,
		spec.PrincipalID, spec.SecretHash, spec.Prefix, credentialPurposeToken, spec.ExpiresAt, "OAuth: "+spec.Agent,
		spec.UserAgent, spec.ClientIP, spec.Agent, emptySliceNil(spec.AgentRoles), channels, spec.ClientID).Scan(&credID); err != nil {
		return fmt.Errorf("storage: issue oauth token: %w", err)
	}
	exp := spec.ExpiresAt
	img := credentialAuditImage{PrincipalID: spec.PrincipalID, Prefix: spec.Prefix, Purpose: credentialPurposeToken, Description: "OAuth: " + spec.Agent,
		ExpiresAt: &exp, Agent: spec.Agent, AgentRoles: emptySliceNil(spec.AgentRoles), AgentChannels: channels}
	if err := writeAuditRes(ctx, tx, spec.PrincipalID, "create", "credential", credID, nil, img); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit issue oauth token: %w", err)
	}
	return nil
}

// isInvalidText reports a malformed uuid, which matches no row.
func isInvalidText(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
