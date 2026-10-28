package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/settings"
)

// The sample lanes retention prunes, with each lane's type column.
var sampleLanes = map[string]string{"metric": "metric_type_id", "property": "property_type_id"}

// sampleArcs is every owner arc a sample row fills one of, in the order a prune
// visits them.
var sampleArcs = []string{"component", "system", "location", "node"}

// pruneProbe is how one lane finds, for an old row of one owner arc, a newer
// row of the same series through an index (TestPruneSamplesReachesAnIndexOnEveryArc):
//   - a component's rows, on either lane, through the series index that leads
//     with component_id (metric_owner_idx, property_series_idx);
//   - the property lane's system and location rows through their owner indexes;
//   - the metric lane's other rows through metric_arc_owner_idx, keyed on the one
//     arc column such a row fills.
//
// No writer gives a node a property today and no index serves one, so that probe
// walks the type's rows; it finds no candidate to probe for until something does.
func pruneProbe(lane, arc string) string {
	col := arc + "_id"
	if lane == "metric" && arc != "component" {
		return `n.component_id is null and coalesce(n.system_id, n.location_id, n.node_id) = c.` + col
	}
	return `n.` + col + ` = c.` + col
}

// pruneSamplesSQL deletes at most $2 rows of one lane and owner arc older than
// $1. It spares:
//   - a declared row;
//   - a command's intended row (command_id set), which the command table, never
//     pruned, reads back as the command's intended value;
//   - a row with no newer row in its series (type, owner arc, instance,
//     provenance) under BOTH orders the readers use, ts then id (the latest-value
//     reads) and id alone (the health reads). So a row is deleted only when some
//     row of its series has both a later-or-equal ts and a higher id, and a clock
//     that stepped back (#801) never costs a series the row either reader calls
//     its latest.
//
// The probe walks the series upward from the candidate's ts (order by n.ts), so
// the first row it meets past the candidate is usually the one that answers. A
// walk down from the newest row would first pass every row a node's clock
// future-dated before it was corrected: they head the ts-descending index with
// low ids, and the id test is a filter, not part of the index condition
// (measured on PG18: 2.5s a chunk against 46ms, with 2,016 skewed rows a series).
//
// The probe is a scalar subquery with limit 1 rather than an EXISTS: Postgres
// flattens an EXISTS into a semi join, which it may run as a hash over every row
// of the table (measured on PG18 at 2.5M rows: 1.5s a chunk, against 61ms for
// the per-row probe). A limited scalar subquery stays a per-row index search,
// which TestPruneSamplesReachesAnIndexOnEveryArc holds. The outer delete takes
// the chosen ids as an array, so it finds them by primary key rather than a
// small table's planner hash-joining an IN over a sequential scan
// (TestPruneStatementsKeepTheirShape).
func pruneSamplesSQL(lane, arc string) string {
	typeCol := sampleLanes[lane]
	return fmt.Sprintf(`
		delete from %[1]s where id = any(array(
			select c.id from %[1]s c
			where c.ts < $1 and c.provenance <> 'declared' and c.command_id is null
			  and c.%[2]s_id is not null
			  and (
				select 1 from %[1]s n
				where %[3]s
				  and n.%[4]s = c.%[4]s and n.owner_kind = c.owner_kind
				  and n.instance = c.instance and n.provenance = c.provenance
				  and n.ts >= c.ts and n.id > c.id
				order by n.ts
				limit 1) is not null
			limit $2))`, lane, arc, pruneProbe(lane, arc), typeCol)
}

// PruneSamples deletes up to limit rows of one sample lane (metric or property)
// older than before, with two exceptions that make retention provenance-aware
// (#591, ADR-0080): a declared row is never deleted (an operator's assertion is
// the whole truth however old, not a sample), and the latest row of every series
// is never deleted (a prune must not erase a current value). It returns the rows
// deleted; fewer than limit means the lane holds nothing more to prune before
// that time, and a re-run deletes nothing. Each owner arc is one statement of its
// own, so no call holds a long transaction (#417).
func (p *PG) PruneSamples(ctx context.Context, lane string, before time.Time, limit int) (int64, error) {
	if _, ok := sampleLanes[lane]; !ok {
		return 0, fmt.Errorf("storage: prune samples: unknown lane %q", lane)
	}
	if limit <= 0 {
		return 0, fmt.Errorf("storage: prune samples: limit %d, want at least 1", limit)
	}
	var total int64
	for _, arc := range sampleArcs {
		if total >= int64(limit) {
			break
		}
		tag, err := p.pool.Exec(ctx, pruneSamplesSQL(lane, arc), before, int64(limit)-total)
		if err != nil {
			return total, fmt.Errorf("storage: prune %s samples owned by a %s: %w", lane, arc, err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// RetentionWindows is the effective retention namespace (#417), read from the
// platform settings level with every default filled: the windows the server's
// retention pass prunes by.
func (p *PG) RetentionWindows(ctx context.Context) (settings.RetentionSettings, error) {
	doc, locks, err := p.settingLevel(ctx, p.pool, "platform")
	if err != nil {
		return settings.RetentionSettings{}, fmt.Errorf("storage: retention windows: %w", err)
	}
	s, err := settings.Typed(p.settings.ResolveOverride(doc, locks).Values)
	if err != nil {
		return settings.RetentionSettings{}, fmt.Errorf("storage: retention windows: %w", err)
	}
	return s.Retention, nil
}
