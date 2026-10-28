package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/settings"
	"github.com/jackc/pgx/v5"
)

// TypeRetention is one type's own retention window (#417): the lane it
// belongs to, the type, and its days.
type TypeRetention struct {
	Lane     string
	TypeID   string
	TypeName string
	Days     int
}

// SetMetricTypeRetention sets or, with nil, clears a metric type's own
// retention window (#417). It works on an official type: the type's
// definition is the seed's, but how long an install keeps its samples is the
// install's. Audited as set_retention. A window outside 1 to
// settings.RetentionMaxDays is ErrMetricTypeInvalid, an unknown name
// ErrMetricTypeNotFound.
func (p *PG) SetMetricTypeRetention(ctx context.Context, actorID, name string, days *int) (*MetricType, error) {
	if err := p.setTypeRetention(ctx, actorID, "metric_type", name, days, ErrMetricTypeInvalid, ErrMetricTypeNotFound); err != nil {
		return nil, err
	}
	return p.GetMetricType(ctx, name)
}

// SetPropertyTypeRetention is SetMetricTypeRetention for a property type.
func (p *PG) SetPropertyTypeRetention(ctx context.Context, actorID, name string, days *int) (*PropertyType, error) {
	if err := p.setTypeRetention(ctx, actorID, "property_type", name, days, ErrPropertyTypeInvalid, ErrPropertyTypeNotFound); err != nil {
		return nil, err
	}
	return p.GetPropertyType(ctx, name)
}

func (p *PG) setTypeRetention(ctx context.Context, actorID, table, name string, days *int, invalid, notFound error) error {
	if err := RejectAddressForm(table, name); err != nil {
		return err
	}
	if days != nil && (*days < 1 || *days > settings.RetentionMaxDays) {
		return fmt.Errorf("%w: a retention of %d days is outside 1 to %d", invalid, *days, settings.RetentionMaxDays)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin set %s retention: %w", table, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := registryAuditImage(ctx, tx, table, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound
	} else if err != nil {
		return fmt.Errorf("storage: audit image %s %q: %w", table, name, err)
	}
	if _, err := tx.Exec(ctx, `update `+table+` set retention_days = $2 where id = $1`, auditImageID(before), days); err != nil {
		return fmt.Errorf("storage: set %s %q retention: %w", table, name, err)
	}
	after, err := registryAuditImage(ctx, tx, table, name)
	if err != nil {
		return fmt.Errorf("storage: audit image %s %q: %w", table, name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "set_retention", table, auditImageID(before), before, after); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit set %s retention: %w", table, err)
	}
	return nil
}

// RetentionOverrides is every type with its own retention window, on both
// sample lanes, for the retention pass (#417).
func (p *PG) RetentionOverrides(ctx context.Context) ([]TypeRetention, error) {
	rows, err := p.pool.Query(ctx, `
		select 'metric', id::text, name, retention_days from metric_type where retention_days is not null
		union all
		select 'property', id::text, name, retention_days from property_type where retention_days is not null
		order by 1, 3`)
	if err != nil {
		return nil, fmt.Errorf("storage: retention overrides: %w", err)
	}
	defer rows.Close()
	var out []TypeRetention
	for rows.Next() {
		var o TypeRetention
		if err := rows.Scan(&o.Lane, &o.TypeID, &o.TypeName, &o.Days); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
