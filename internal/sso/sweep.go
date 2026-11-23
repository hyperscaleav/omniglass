package sso

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/egress"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The re-check sweep (ADR-2361 point 9): every recheck_minutes it redeems
// the refresh token of each link with a live credential through it. An
// accepted refresh keeps the rotated token and restarts the link's grace; a
// refusal revokes, in one audited transaction, every session and API token
// through the link; anything else (a network error, a 5xx) is retried at the
// next pass, and once the grace runs out authentication refuses the link's
// credentials until a refresh is accepted.

// idleEvery is how often a sweeper with no provider re-reads the settings.
const idleEvery = time.Minute

// Store is the slice of the Storage Gateway the sweep uses.
type Store interface {
	SSOProvider(ctx context.Context, name string) (*storage.SSOProvider, error)
	EgressPolicy(ctx context.Context) (egress.Policy, error)
	LinksToRecheck(ctx context.Context, issuer string) ([]storage.LinkRecheck, error)
	AcceptLinkRefresh(ctx context.Context, linkID, rotated string, grace time.Duration, groups *storage.IdPGroups) error
	RevokeLink(ctx context.Context, linkID, reason string) (int, error)
}

// Sweeper runs the re-check.
type Sweeper struct {
	store Store
	opts  egress.Options
	log   *slog.Logger
}

// NewSweeper re-checks through store, its provider calls tuned by opts (a
// test's certificate roots).
func NewSweeper(store Store, opts egress.Options, log *slog.Logger) *Sweeper {
	if log == nil {
		log = slog.Default()
	}
	return &Sweeper{store: store, opts: opts, log: log}
}

// Pass is one sweep's outcome.
type Pass struct {
	Accepted, Revoked, Failed int
}

// RunOnce sweeps every link of the configured provider once; the next delay
// is the provider's recheck interval (idleEvery with none).
func (s *Sweeper) RunOnce(ctx context.Context) (Pass, time.Duration, error) {
	p, err := s.store.SSOProvider(ctx, storage.SSOProviderDefault)
	if err != nil || p == nil {
		return Pass{}, idleEvery, err
	}
	links, err := s.store.LinksToRecheck(ctx, p.Issuer)
	if err != nil || len(links) == 0 {
		return Pass{}, p.Recheck, err
	}
	pol, err := s.store.EgressPolicy(ctx)
	if err != nil {
		return Pass{}, p.Recheck, err
	}
	c, err := Dial(ctx, p, egress.New(pol, s.opts).HTTPClient(), "")
	if err != nil {
		// The provider is unreachable: every link waits out its grace.
		return Pass{Failed: len(links)}, p.Recheck, err
	}
	var out Pass
	for _, l := range links {
		r, err := c.Refresh(ctx, l.RefreshToken)
		switch {
		case err == nil:
			// A refresh whose id_token carries the groups claim syncs the
			// person's idp memberships (#2363); one that does not verify is
			// still an accepted refresh, its groups left as they were.
			if r.ClaimsErr != nil {
				s.log.Warn("sso: a refresh's id_token did not verify; its groups are not synced", "link", l.LinkID, "err", r.ClaimsErr.Error())
			}
			if err := s.store.AcceptLinkRefresh(ctx, l.LinkID, r.Rotated, p.Grace, RefreshGroups(r.Claims, l.Identifier, p.GroupsClaim)); err != nil {
				return out, p.Recheck, err
			}
			out.Accepted++
		case errors.Is(err, ErrRefused):
			n, err := s.store.RevokeLink(ctx, l.LinkID, "the identity provider refused the refresh")
			if err != nil {
				return out, p.Recheck, err
			}
			s.log.Info("sso: the provider refused a link's refresh; its credentials are revoked", "link", l.LinkID, "revoked", n)
			out.Revoked++
		default:
			s.log.Warn("sso: a link's refresh failed; retried at the next pass", "link", l.LinkID, "err", err.Error())
			out.Failed++
		}
	}
	return out, p.Recheck, nil
}

// Run sweeps until ctx ends.
func (s *Sweeper) Run(ctx context.Context) {
	for {
		_, next, err := s.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Warn("sso: the re-check pass failed", "err", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(next):
		}
	}
}
