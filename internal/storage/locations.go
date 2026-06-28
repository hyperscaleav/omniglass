package storage

import (
	"context"
	"fmt"
)

// LocationType is a registry row classifying a location: a stable id, the
// official flag, a display_name, and a rank (ordering plus a soft hierarchy
// signal, not a nesting constraint). It is the only shape-definer for a
// location, which has no template.
type LocationType struct {
	ID          string
	Official    bool
	DisplayName string
	Rank        int
}

// UpsertLocationType installs or updates a location type by id, the boot-seed
// phase's write. Idempotent: re-seeding the same id updates it in place.
func (p *PG) UpsertLocationType(ctx context.Context, lt LocationType) error {
	_, err := p.pool.Exec(ctx, `
		insert into location_type (id, official, display_name, rank)
		values ($1, $2, $3, $4)
		on conflict (id) do update
			set official     = excluded.official,
			    display_name = excluded.display_name,
			    rank         = excluded.rank`,
		lt.ID, lt.Official, lt.DisplayName, lt.Rank)
	if err != nil {
		return fmt.Errorf("storage: upsert location_type %q: %w", lt.ID, err)
	}
	return nil
}

// ListLocationTypes returns every location type, ordered by rank then id, for
// the registry view and validation.
func (p *PG) ListLocationTypes(ctx context.Context) ([]LocationType, error) {
	rows, err := p.pool.Query(ctx,
		`select id, official, display_name, rank from location_type order by rank, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list location_types: %w", err)
	}
	defer rows.Close()
	var out []LocationType
	for rows.Next() {
		var lt LocationType
		if err := rows.Scan(&lt.ID, &lt.Official, &lt.DisplayName, &lt.Rank); err != nil {
			return nil, fmt.Errorf("storage: scan location_type: %w", err)
		}
		out = append(out, lt)
	}
	return out, rows.Err()
}
