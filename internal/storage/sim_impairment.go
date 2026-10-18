package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/sim/impair"
)

// ErrSimImpairmentInvalid is an impairment the network could not apply (a
// negative latency, jitter above latency, loss past 100%).
var ErrSimImpairmentInvalid = errors.New("storage: invalid simulator impairment")

// GetSimImpairment reads what is broken on a simulated component, by its
// already-resolved id; a healthy device reads as the zero impairment.
func (p *PG) GetSimImpairment(ctx context.Context, componentID string) (impair.Impairment, error) {
	var imp impair.Impairment
	err := p.pool.QueryRow(ctx, `select unplugged, latency_ms, jitter_ms, loss_pct::float8, rate_kbit
		from sim_impairment where component_id = $1`, componentID).
		Scan(&imp.Unplugged, &imp.LatencyMs, &imp.JitterMs, &imp.LossPct, &imp.RateKbit)
	if errors.Is(err, pgx.ErrNoRows) {
		return impair.Impairment{}, nil
	}
	if err != nil {
		return impair.Impairment{}, fmt.Errorf("storage: sim impairment of %s: %w", componentID, err)
	}
	return imp, nil
}

// SetSimImpairment records what is broken on a simulated component (by its
// already-resolved id, scope checked by the caller), audited in the same
// transaction. The zero impairment clears it: a healthy device has no row.
func (p *PG) SetSimImpairment(ctx context.Context, actorID, componentID string, imp impair.Impairment) error {
	if err := imp.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrSimImpairmentInvalid, err)
	}
	old, err := p.GetSimImpairment(ctx, componentID)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin set sim impairment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if imp == (impair.Impairment{}) {
		if _, err := tx.Exec(ctx, `delete from sim_impairment where component_id = $1`, componentID); err != nil {
			return fmt.Errorf("storage: clear sim impairment of %s: %w", componentID, err)
		}
	} else if _, err := tx.Exec(ctx, `insert into sim_impairment (component_id, unplugged, latency_ms, jitter_ms, loss_pct, rate_kbit)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (component_id) do update set unplugged = excluded.unplugged, latency_ms = excluded.latency_ms,
			jitter_ms = excluded.jitter_ms, loss_pct = excluded.loss_pct, rate_kbit = excluded.rate_kbit, updated_at = now()`,
		componentID, imp.Unplugged, imp.LatencyMs, imp.JitterMs, imp.LossPct, imp.RateKbit); err != nil {
		return fmt.Errorf("storage: set sim impairment of %s: %w", componentID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "sim_impairment", componentID, old, imp); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
