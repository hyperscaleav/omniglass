package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/transport"
	"github.com/jackc/pgx/v5"
)

// The webhook transport's store (#1024, ADR-1024): the token each webhook
// endpoint's URL carries, minted here and held as a sealed secret, the
// lookup the receiver resolves a post's endpoint by, and the counters and
// inbox the listener's tally flushes into.

var (
	// ErrWebhookNotFound is a token no endpoint holds: unknown, or rotated.
	ErrWebhookNotFound = errors.New("storage: webhook token not found")
	// ErrNotPushEndpoint refuses a push transport's operation on a dialed
	// endpoint (rotating a token an ssh endpoint has none of).
	ErrNotPushEndpoint = errors.New("storage: not a push endpoint")
)

// WebhookTokenSecret is the name of the secret holding a webhook endpoint's
// token, on the endpoint's component (one webhook endpoint per component).
const WebhookTokenSecret = "webhook-token"

// webhookTokenBytes is the token's entropy: 128 random bits.
const webhookTokenBytes = 16

// WebhookTokenLen is the token's length as it rides a URL (unpadded
// base64url of 128 bits).
const WebhookTokenLen = 22

// UnclaimedBound and UnclaimedMaxAge bound the inbox per listener (decision
// 4 of #1146): the least recently heard past the bound are evicted, and a
// source unheard for the age is let go.
const (
	UnclaimedBound  = 256
	UnclaimedMaxAge = 7 * 24 * time.Hour
)

// WebhookTokenHash is the digest a token is looked up by.
func WebhookTokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// mintToken draws a fresh token.
func mintToken() (string, error) {
	b := make([]byte, webhookTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("storage: mint webhook token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// mintWebhookToken mints a webhook endpoint's token inside its create: the
// webhook-token secret on the endpoint's component, sealed like any secret,
// and its binding row with the token's digest. A secret of that name already
// on the component refuses as ErrSecretExists.
func (p *PG) mintWebhookToken(ctx context.Context, tx pgx.Tx, actorID, endpointID, componentID string) error {
	if p.secret == nil {
		return ErrNoSecretProvider
	}
	token, err := mintToken()
	if err != nil {
		return err
	}
	st, err := p.GetSecretType(ctx, WebhookTokenSecret)
	if err != nil {
		return err
	}
	owner := componentID
	value, err := p.sealFields(ctx, st.Shape(), "component", &owner, WebhookTokenSecret, map[string]string{"token": token})
	if err != nil {
		return err
	}
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("storage: marshal webhook token: %w", err)
	}
	s, err := scanSecretRow(tx.QueryRow(ctx, `
		insert into secret (name, label, secret_type, owner_kind, component_id, value, admin_sensitive)
		values ($1, 'Webhook token', (select id from secret_type_in_effect() secret_type where name = $1), 'component', $2, $3, false)
		returning `+secretCols, WebhookTokenSecret, componentID, valueJSON), st.Shape())
	if err != nil {
		return mapSecretWriteErr(err)
	}
	if _, err := tx.Exec(ctx, `insert into webhook_token (endpoint_id, secret_id, token_hash) values ($1, $2, $3)`,
		endpointID, s.ID, WebhookTokenHash(token)); err != nil {
		return fmt.Errorf("storage: bind webhook token: %w", err)
	}
	return writeAuditRes(ctx, tx, actorID, "create", "secret", s.ID, nil, auditSecret(s))
}

// RotateWebhookToken mints a webhook endpoint's token anew: the old one
// stops resolving at once (a post bearing it answers as a token that never
// existed), and the new one is revealed through the endpoint's secret. A
// token deleted with its secret is minted again. The read-then-action scope
// split runs through the endpoint's component; audited on the endpoint.
func (p *PG) RotateWebhookToken(ctx context.Context, actorID, ref string, read, action scope.Set) (*Endpoint, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin rotate webhook token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ep, err := resolveEndpointScoped(ctx, tx, ref, read, action)
	if err != nil {
		return nil, err
	}
	if !transport.IsPush(ep.Transport) || ep.ComponentID == nil {
		return nil, ErrNotPushEndpoint
	}
	var secretID string
	err = tx.QueryRow(ctx, `select secret_id from webhook_token where endpoint_id = $1 for update`, ep.ID).Scan(&secretID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := p.mintWebhookToken(ctx, tx, actorID, ep.ID, *ep.ComponentID); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, fmt.Errorf("storage: lock webhook token: %w", err)
	default:
		token, err := mintToken()
		if err != nil {
			return nil, err
		}
		st, err := p.GetSecretType(ctx, WebhookTokenSecret)
		if err != nil {
			return nil, err
		}
		owner := *ep.ComponentID
		value, err := p.sealFields(ctx, st.Shape(), "component", &owner, WebhookTokenSecret, map[string]string{"token": token})
		if err != nil {
			return nil, err
		}
		valueJSON, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("storage: marshal webhook token: %w", err)
		}
		if _, err := tx.Exec(ctx, `update secret set value = $2, updated_at = now() where id = $1`, secretID, valueJSON); err != nil {
			return nil, fmt.Errorf("storage: reseal webhook token: %w", err)
		}
		if _, err := tx.Exec(ctx, `update webhook_token set token_hash = $2, minted_at = now() where endpoint_id = $1`,
			ep.ID, WebhookTokenHash(token)); err != nil {
			return nil, fmt.Errorf("storage: rebind webhook token: %w", err)
		}
		if err := writeAuditRes(ctx, tx, actorID, "update", "secret", secretID, nil, nil); err != nil {
			return nil, err
		}
	}
	if err := writeAuditRes(ctx, tx, actorID, "rotate-token", "endpoint", ep.ID, nil, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit rotate webhook token: %w", err)
	}
	return ep, nil
}

// deleteWebhookToken removes a webhook endpoint's token secret inside the
// endpoint's delete, audited as the secret's own delete.
func deleteWebhookToken(ctx context.Context, tx pgx.Tx, actorID, endpointID string) error {
	var secretID string
	err := tx.QueryRow(ctx, `delete from secret where id = (select secret_id from webhook_token where endpoint_id = $1) returning id`, endpointID).Scan(&secretID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: delete webhook token: %w", err)
	}
	return writeAuditRes(ctx, tx, actorID, "delete", "secret", secretID, nil, nil)
}

// WebhookTarget is everything the receiver needs to check and land one post
// to a resolved token: the endpoint and its component, the scheme its driver
// declares (none for a bare endpoint) with its block, the scheme's credential
// unsealed (confined to secrets cascading onto the component, as a node's
// worklist is), and its listeners in the spec's order.
type WebhookTarget struct {
	EndpointID, EndpointName, ComponentID string
	Scheme                                string
	Webhook                               *driver.WebhookSpec
	// Credential is the credential input's fields; nil with CredentialMissing
	// set when its secret no longer resolves, which refuses every post
	// rather than admitting one unchecked.
	Credential        map[string]string
	CredentialMissing bool
	Listeners         []*driver.BakedFunction
}

// ResolveWebhook finds the endpoint a token digest names, ErrWebhookNotFound
// when none does. It runs as the server's own read: the token is the
// sender's authorization for its one endpoint, and nothing here is scoped to
// a caller.
func (p *PG) ResolveWebhook(ctx context.Context, tokenHash []byte) (*WebhookTarget, error) {
	var t WebhookTarget
	var inputs, spec []byte
	err := p.pool.QueryRow(ctx, `
		select e.id::text, e.name, e.component::text, e.inputs, d.spec
		from webhook_token w join endpoint e on e.id = w.endpoint_id
		left join driver d on d.id = e.driver_id
		where w.token_hash = $1 and e.component is not null`, tokenHash).
		Scan(&t.EndpointID, &t.EndpointName, &t.ComponentID, &inputs, &spec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWebhookNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: resolve webhook: %w", err)
	}
	t.Scheme = driver.WebhookNone
	var order []string
	credInput := ""
	if len(spec) > 0 {
		sp, err := driver.Parse(spec)
		if err != nil {
			return nil, fmt.Errorf("storage: stored spec for webhook endpoint %q does not parse: %w", t.EndpointID, err)
		}
		if sp.Session != nil {
			t.Scheme, t.Webhook, credInput = sp.Session.Scheme, sp.Session.Webhook, sp.Session.Credential
		}
		for _, l := range sp.Listeners {
			order = append(order, l.Name)
		}
	}
	if credInput != "" {
		var in map[string]string
		_ = json.Unmarshal(inputs, &in)
		fields, _, err := p.nodeSecretFields(ctx, in[credInput], t.ComponentID)
		var amb *ErrAmbiguousName
		switch {
		case errors.Is(err, ErrSecretNotFound), errors.As(err, &amb):
			t.CredentialMissing = true
		case err != nil:
			return nil, err
		default:
			t.Credential = fields
		}
	}
	rows, err := p.pool.Query(ctx, `select spec from task where endpoint_id = $1 and mode = 'listen' and enabled`, t.EndpointID)
	if err != nil {
		return nil, fmt.Errorf("storage: webhook listeners: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("storage: scan webhook listener: %w", err)
		}
		fn, err := driver.ParseBaked(raw)
		if err != nil {
			return nil, err
		}
		t.Listeners = append(t.Listeners, fn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: webhook listeners: %w", err)
	}
	slices.SortStableFunc(t.Listeners, func(a, b *driver.BakedFunction) int {
		return slices.Index(order, a.Function) - slices.Index(order, b.Function)
	})
	return &t, nil
}

// EndpointWebhook is a webhook endpoint's own read (the endpoint's page):
// its scheme (none reads unsigned), the secret its token is revealed
// through (empty once deleted), when it was minted, and what its scheme has
// refused.
type EndpointWebhook struct {
	Scheme        string
	Secret        string
	MintedAt      *time.Time
	RefusedCount  int64
	RefusedAt     *time.Time
	RefusedReason string
	RefusedSource string
}

// GetEndpointWebhook reads a webhook endpoint's block by its id (the caller
// has already resolved and scoped the endpoint).
func (p *PG) GetEndpointWebhook(ctx context.Context, endpointID string) (*EndpointWebhook, error) {
	w := EndpointWebhook{Scheme: driver.WebhookNone}
	var spec []byte
	var component *string
	var refusedReason, refusedSource *string
	err := p.pool.QueryRow(ctx, `
		select d.spec, e.component::text, t.minted_at, coalesce(t.refused_count, 0), t.refused_at, t.refused_reason, t.refused_source
		from endpoint e
		left join driver d on d.id = e.driver_id
		left join webhook_token t on t.endpoint_id = e.id
		where e.id = $1`, endpointID).
		Scan(&spec, &component, &w.MintedAt, &w.RefusedCount, &w.RefusedAt, &refusedReason, &refusedSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEndpointNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: endpoint webhook: %w", err)
	}
	if refusedReason != nil {
		w.RefusedReason = *refusedReason
	}
	if refusedSource != nil {
		w.RefusedSource = *refusedSource
	}
	if len(spec) > 0 {
		if sp, err := driver.Parse(spec); err == nil && sp.Session != nil && sp.Session.Scheme != "" {
			w.Scheme = sp.Session.Scheme
		}
	}
	if w.MintedAt != nil && component != nil {
		w.Secret = *component + "/" + WebhookTokenSecret
	}
	return &w, nil
}

// WebhookRefusal is what an endpoint's scheme refused from one source for
// one reason since the last flush.
type WebhookRefusal struct {
	EndpointID, Source, Reason string
	Count                      int64
	LastAt                     time.Time
}

// RecordWebhookRefusals adds a flush's refusals to their endpoints'
// counters, keeping the latest source and reason.
func (p *PG) RecordWebhookRefusals(ctx context.Context, rs []WebhookRefusal) error {
	for _, r := range rs {
		if _, err := p.pool.Exec(ctx, `
			update webhook_token set refused_count = refused_count + $2,
				refused_at = greatest(coalesce(refused_at, $3), $3),
				refused_reason = case when refused_at is null or $3 >= refused_at then $4 else refused_reason end,
				refused_source = case when refused_at is null or $3 >= refused_at then $5 else refused_source end
			where endpoint_id = $1`, r.EndpointID, r.Count, r.LastAt, r.Reason, r.Source); err != nil {
			return fmt.Errorf("storage: record webhook refusals: %w", err)
		}
	}
	return nil
}

// UnclaimedSource is one inbox row: a source a listener heard from that no
// endpoint claims.
type UnclaimedSource struct {
	Host, Transport, Source   string
	FirstHeardAt, LastHeardAt time.Time
	Count                     int64
	LastMessage               string
}

// RecordUnclaimedSources folds a flush's unclaimed sources into the inbox,
// then holds it to its bound: per listener, the least recently heard past
// UnclaimedBound are evicted, and any unheard for UnclaimedMaxAge let go.
func (p *PG) RecordUnclaimedSources(ctx context.Context, us []UnclaimedSource) error {
	if len(us) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin record unclaimed sources: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	listeners := map[[2]string]bool{}
	for _, u := range us {
		if _, err := tx.Exec(ctx, `
			insert into unclaimed_source (host, transport, source, first_heard_at, last_heard_at, heard_count, last_message)
			values ($1, $2, $3, $4, $5, $6, $7)
			on conflict (host, transport, source) do update set
				last_heard_at = greatest(unclaimed_source.last_heard_at, excluded.last_heard_at),
				heard_count = unclaimed_source.heard_count + excluded.heard_count,
				last_message = excluded.last_message`,
			u.Host, u.Transport, u.Source, u.FirstHeardAt, u.LastHeardAt, u.Count, u.LastMessage); err != nil {
			return fmt.Errorf("storage: record unclaimed source: %w", err)
		}
		listeners[[2]string{u.Host, u.Transport}] = true
	}
	for l := range listeners {
		if _, err := tx.Exec(ctx, `
			delete from unclaimed_source where host = $1 and transport = $2 and source in (
				select source from unclaimed_source where host = $1 and transport = $2
				order by last_heard_at desc offset $3)`, l[0], l[1], UnclaimedBound); err != nil {
			return fmt.Errorf("storage: bound unclaimed sources: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `delete from unclaimed_source where last_heard_at < now() - $1::interval`, UnclaimedMaxAge.String()); err != nil {
		return fmt.Errorf("storage: age unclaimed sources: %w", err)
	}
	return tx.Commit(ctx)
}

// ListUnclaimedSources reads the inbox, most recently heard first, within
// its age (the node:read observation of every listener).
func (p *PG) ListUnclaimedSources(ctx context.Context) ([]UnclaimedSource, error) {
	rows, err := p.pool.Query(ctx, `
		select host, transport, source, first_heard_at, last_heard_at, heard_count, last_message
		from unclaimed_source where last_heard_at >= now() - $1::interval
		order by last_heard_at desc, source`, UnclaimedMaxAge.String())
	if err != nil {
		return nil, fmt.Errorf("storage: list unclaimed sources: %w", err)
	}
	defer rows.Close()
	out := []UnclaimedSource{}
	for rows.Next() {
		var u UnclaimedSource
		if err := rows.Scan(&u.Host, &u.Transport, &u.Source, &u.FirstHeardAt, &u.LastHeardAt, &u.Count, &u.LastMessage); err != nil {
			return nil, fmt.Errorf("storage: scan unclaimed source: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ClaimUnclaimedSource lets go of a source's inbox row once a message from
// it lands on an endpoint: it is claimed. Reports whether a row was there.
func (p *PG) ClaimUnclaimedSource(ctx context.Context, host, transportName, source string) (bool, error) {
	tag, err := p.pool.Exec(ctx, `delete from unclaimed_source where host = $1 and transport = $2 and source = $3`, host, transportName, source)
	if err != nil {
		return false, fmt.Errorf("storage: claim unclaimed source: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
