package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/jackc/pgx/v5"
)

// ErrCannotImpersonateSelf is returned when the real actor and the target are the
// same principal.
var ErrCannotImpersonateSelf = errors.New("storage: cannot impersonate self")

// ErrImpersonationNotFound is returned when ending a session that is not active.
var ErrImpersonationNotFound = errors.New("storage: impersonation session not found")

// ImpersonationSession is an active impersonation grant: the target being acted as
// or viewed as, the real actor behind it, and the mode.
type ImpersonationSession struct {
	ID          string
	TargetID    string
	RealActorID string
	Mode        string // "view_as" (read-only) or "act_as" (full)
	ExpiresAt   time.Time
}

// BeginImpersonation mints an impersonation token for realActorID to view/act as
// targetID for ttl, storing its sha256 hash. The API enforces the escalation guard
// (the real actor's capabilities must cover the target's) and the
// principal:impersonate capability before calling this; the gateway refuses only
// self-impersonation and an invalid mode, then persists. Returns the plaintext
// token (shown once) and the session.
func (p *PG) BeginImpersonation(ctx context.Context, realActorID, targetID, mode string, ttl time.Duration) (string, *ImpersonationSession, error) {
	if realActorID == targetID {
		return "", nil, ErrCannotImpersonateSelf
	}
	if mode != "view_as" && mode != "act_as" {
		return "", nil, fmt.Errorf("storage: bad impersonation mode %q", mode)
	}
	token, hash, _, err := auth.NewBearerToken()
	if err != nil {
		return "", nil, fmt.Errorf("storage: mint impersonation token: %w", err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("storage: begin impersonation tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	s := ImpersonationSession{TargetID: targetID, RealActorID: realActorID, Mode: mode}
	err = tx.QueryRow(ctx, `
		insert into impersonation_session (token_hash, target_principal_id, real_actor_principal_id, mode, expires_at)
		values ($1, $2, $3, $4, now() + make_interval(secs => $5))
		returning id, expires_at`,
		hash, targetID, realActorID, mode, ttl.Seconds()).Scan(&s.ID, &s.ExpiresAt)
	if err != nil {
		return "", nil, fmt.Errorf("storage: begin impersonation: %w", err)
	}
	// Audit the START: a normal action by the admin acting as themselves (not yet
	// impersonated), so actor is the real admin and real_actor is null.
	if err := writeAuditRes(ctx, tx, realActorID, "impersonate", "principal", targetID, nil, map[string]any{"mode": mode}); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", nil, fmt.Errorf("storage: commit begin impersonation: %w", err)
	}
	return token, &s, nil
}

// AuthenticateImpersonation resolves an impersonation token hash to the TARGET
// principal (its profile and grants), plus the real actor id, the mode, and the
// session id. It is the authn fallback the API tries on a bearer miss. The session
// must be unrevoked and unexpired, and BOTH the target and the real actor must be
// active, so disabling either kills the session immediately. A miss is
// ErrCredentialNotFound, so the API treats it exactly like a bad bearer.
func (p *PG) AuthenticateImpersonation(ctx context.Context, hash []byte) (pr *Principal, realActorID, mode, sessionID string, err error) {
	var t Principal
	err = p.pool.QueryRow(ctx, `
		select s.id, s.target_principal_id, pr.kind, s.real_actor_principal_id, s.mode
		from impersonation_session s
		join principal pr on pr.id = s.target_principal_id and pr.active
		join principal ra on ra.id = s.real_actor_principal_id and ra.active
		where s.token_hash = $1 and s.revoked_at is null and s.expires_at > now()`,
		hash).Scan(&sessionID, &t.ID, &t.Kind, &realActorID, &mode)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, "", "", "", ErrCredentialNotFound
	case err != nil:
		return nil, "", "", "", fmt.Errorf("storage: authenticate impersonation: %w", err)
	}
	if err := p.loadPrincipal(ctx, &t); err != nil {
		return nil, "", "", "", err
	}
	return &t, realActorID, mode, sessionID, nil
}

// EndImpersonation revokes an active session by id. Ending an already-revoked,
// expired, or unknown session is ErrImpersonationNotFound.
func (p *PG) EndImpersonation(ctx context.Context, sessionID string) error {
	tag, err := p.pool.Exec(ctx, `
		update impersonation_session set revoked_at = now()
		where id = $1 and revoked_at is null and expires_at > now()`, sessionID)
	if err != nil {
		return fmt.Errorf("storage: end impersonation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrImpersonationNotFound
	}
	return nil
}
