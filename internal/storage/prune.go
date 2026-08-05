package storage

import (
	"context"
	"fmt"
	"time"
)

// PruneSamples deletes series rows older than before from both sample tables
// (metric and property), with two exceptions that make retention provenance-aware
// before any retention feature exists (#591): a declared row is never deleted
// (an operator's assertion is the whole truth however old, not a sample), and
// the latest row of every series (type, owner arc, instance, provenance) is
// never deleted (a prune must not erase a current value). It returns the number
// of rows deleted; a re-run deletes nothing further. No caller wires it yet:
// the rule ships ahead of the feature so the first retention pass cannot get it
// wrong.
func (p *PG) PruneSamples(ctx context.Context, before time.Time) (int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("storage: begin prune samples: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One shape per lane, differing only in the type column. rn > 1 spares the
	// latest row per series (ts orders, id breaks same-instant ties, the same
	// rule the latest-value reads apply).
	var total int64
	for _, lane := range []struct{ table, typeCol string }{
		{"metric", "metric_type_id"},
		{"property", "property_type_id"},
	} {
		sql := fmt.Sprintf(`
			delete from %[1]s where id in (
				select id from (
					select id, ts, provenance,
					       row_number() over (
					           partition by %[2]s, owner_kind, component_id, system_id, location_id, node_id, instance, provenance
					           order by ts desc, id desc) as rn
					from %[1]s) ranked
				where rn > 1 and provenance <> 'declared' and ts < $1)`, lane.table, lane.typeCol)
		tag, err := tx.Exec(ctx, sql, before)
		if err != nil {
			return 0, fmt.Errorf("storage: prune %s samples: %w", lane.table, err)
		}
		total += tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("storage: commit prune samples: %w", err)
	}
	return total, nil
}
