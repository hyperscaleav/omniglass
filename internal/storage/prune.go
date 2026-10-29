package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/settings"
)

// The sample lanes retention prunes, with each lane's type column, and the
// type catalog a type's own window (retention_days) lives on.
var sampleLanes = map[string]string{"metric": "metric_type_id", "property": "property_type_id"}

var sampleLaneTypes = map[string]string{"metric": "metric_type", "property": "property_type"}

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
//
// A lane's own prune (typed false) skips every type with a window of its own
// (#417); a typed prune ($3, one type's id) takes only that type, to its window,
// and only while that window is still $4, the days the pass read: an override
// cleared or changed since prunes nothing until the next pass reads it.
func pruneSamplesSQL(lane, arc string, typed bool) string {
	typeCol := sampleLanes[lane]
	types := fmt.Sprintf(`c.%s <> all(array(select id from %s where retention_days is not null))`, typeCol, sampleLaneTypes[lane])
	if typed {
		types = fmt.Sprintf(`c.%s = $3::uuid and $4::int = (select retention_days from %s where id = $3::uuid)`, typeCol, sampleLaneTypes[lane])
	}
	// A row a system, a location or a node owns has no component (the owner arc
	// is exclusive). Saying so lets the planner prove metric_arc_owner_idx's
	// partial predicate, so a typed prune searches its type's rows through that
	// index instead of reading every old row of the lane
	// (TestATypedPruneSearchesItsTypesRows).
	if arc != "component" {
		types += ` and c.component_id is null`
	}
	return fmt.Sprintf(`
		delete from %[1]s where id = any(array(
			select c.id from %[1]s c
			where c.ts < $1 and c.provenance <> 'declared' and c.command_id is null
			  and c.%[2]s_id is not null and %[5]s
			  and (
				select 1 from %[1]s n
				where %[3]s
				  and n.%[4]s = c.%[4]s and n.owner_kind = c.owner_kind
				  and n.instance = c.instance and n.provenance = c.provenance
				  and n.ts >= c.ts and n.id > c.id
				order by n.ts
				limit 1) is not null
			limit $2))`, lane, arc, pruneProbe(lane, arc), typeCol, types)
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
	return p.pruneSamples(ctx, lane, "", 0, before, limit)
}

// PruneSamplesOfType is PruneSamples for one type with a window of its own
// (#417): the lane's prune skips it, and this takes it to its window. days is
// the window the caller read, and nothing is pruned unless the type still has
// it, so a pass never prunes by an override changed since it read it.
func (p *PG) PruneSamplesOfType(ctx context.Context, lane, typeID string, days int, before time.Time, limit int) (int64, error) {
	if typeID == "" {
		return 0, fmt.Errorf("storage: prune samples of type: no type")
	}
	return p.pruneSamples(ctx, lane, typeID, days, before, limit)
}

func (p *PG) pruneSamples(ctx context.Context, lane, typeID string, days int, before time.Time, limit int) (int64, error) {
	if _, ok := sampleLanes[lane]; !ok {
		return 0, fmt.Errorf("storage: prune samples: unknown lane %q", lane)
	}
	if limit <= 0 {
		return 0, fmt.Errorf("storage: prune samples: limit %d, want at least 1", limit)
	}
	if lane == "metric" {
		return p.pruneMetricSamples(ctx, typeID, days, before, limit)
	}
	var total int64
	for _, arc := range sampleArcs {
		if total >= int64(limit) {
			break
		}
		args := []any{before, int64(limit) - total}
		if typeID != "" {
			args = append(args, typeID, days)
		}
		tag, err := p.pool.Exec(ctx, pruneSamplesSQL(lane, arc, typeID != ""), args...)
		if err != nil {
			return total, fmt.Errorf("storage: prune %s samples owned by a %s: %w", lane, arc, err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// pruneMetricSampleSQL deletes at most $2 samples older than $1 from one
// metric partition (part, a sanitized name), each statement short. The lane's
// prune (typed false) takes every sample whose type has no window of its own,
// and every sample whose series is gone (its owner deleted); a typed prune
// ($3, one type's id) takes that type's samples, series by series through the
// series index, and only while the type's window is still $4, the days the
// pass read.
//
// The metric lane keeps no floor (ADR-1275, amending ADR-0080): a series
// keeps the latest row within retention, and one with none reads as no
// reading within retention. Every observation of a quantity is stored, so a
// series still measured always has a recent row; a command's intended rows,
// the declared tier, are in metric_declared, which no prune touches. The
// chosen rows are deleted by ctid, a TID scan of the one partition.
func pruneMetricSampleSQL(part string, typed bool) string {
	if typed {
		return `delete from ` + part + ` where ctid = any(array(
			select m.ctid from metric_series s
			cross join lateral (select x.ctid from ` + part + ` x where x.series_id = s.id and x.ts < $1 limit $2) m
			where s.metric_type_id = $3::uuid and $4::int = (select retention_days from metric_type where id = $3::uuid)
			limit $2))`
	}
	return `delete from ` + part + ` where ctid = any(array(
		select m.ctid from ` + part + ` m
		where m.ts < $1 and not exists (select 1 from metric_series s join metric_type t on t.id = s.metric_type_id
			where s.id = m.series_id and t.retention_days is not null)
		limit $2))`
}

// pruneMetricSamples is pruneSamples on the metric lane: the partitions that
// begin before the cutoff, oldest first, a statement each until limit rows
// are deleted. A month wholly past every window is dropped whole by
// DropMetricPartitions; this keeps the window exact inside the months that
// remain.
func (p *PG) pruneMetricSamples(ctx context.Context, typeID string, days int, before time.Time, limit int) (int64, error) {
	parts, err := metricPartitions(ctx, p.pool)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, part := range parts {
		if total >= int64(limit) || !part.From.Before(before) {
			break
		}
		args := []any{before, int64(limit) - total}
		if typeID != "" {
			args = append(args, typeID, days)
		}
		tag, err := p.pool.Exec(ctx, pruneMetricSampleSQL(pgx.Identifier{"public", part.Name}.Sanitize(), typeID != ""), args...)
		if err != nil {
			return total, fmt.Errorf("storage: prune metric samples in %s: %w", part.Name, err)
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
