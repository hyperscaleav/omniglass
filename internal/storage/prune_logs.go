package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/logline"
)

// The log tables retention prunes (#417).
var logTables = map[string]bool{"log_line": true, "node_log": true}

// pruneLogsSQL deletes at most $2 lines of one log table older than $1: the
// lines of the levels in $3 for "rated", or the lines with no severity for
// "unrated". A log line has no floor: it is not a series' value, and an event
// that named one as its source keeps itself and drops the link
// (event.source_log_line_id is on delete set null, through its own index). Each
// statement searches the table's retention index for exactly the rows it may
// delete (TestPruneLogsReachesItsIndex), ordered as that index is: a LIMIT with
// no ORDER BY let the planner prefer a sequential scan once retention made
// severity and time correlate, reading 2.4M rows for a chunk that found none
// (the 2026-10-28 review). The outer delete takes the chosen ids as an array, so
// it finds them by primary key: an IN over the subquery let a small table's
// planner hash-join it over a sequential scan instead (TestPruneStatementsKeepTheirShape).
func pruneLogsSQL(table, kind string) string {
	where, order := `c.severity = any($3) and c.ts < $1`, `c.severity, c.ts`
	if kind == "unrated" {
		where, order = `c.severity is null and c.ts < $1`, `c.ts`
	}
	return fmt.Sprintf(`delete from %[1]s where id = any(array(select c.id from %[1]s c where %[2]s order by %[3]s limit $2))`, table, where, order)
}

// PruneLogs deletes up to limit lines of one log table (log_line or node_log)
// in one retention band (logline.RetentionBands) older than before: the band's
// levels, and for the band a line with no severity is kept in, those lines too.
// It returns the lines deleted; fewer than limit means the band holds nothing
// more before that time. Each kind of line is one statement of its own.
func (p *PG) PruneLogs(ctx context.Context, table, band string, before time.Time, limit int) (int64, error) {
	if !logTables[table] {
		return 0, fmt.Errorf("storage: prune logs: unknown table %q", table)
	}
	levels := logline.BandLevels(band)
	if levels == nil {
		return 0, fmt.Errorf("storage: prune logs: unknown band %q", band)
	}
	if limit <= 0 {
		return 0, fmt.Errorf("storage: prune logs: limit %d, want at least 1", limit)
	}
	tag, err := p.pool.Exec(ctx, pruneLogsSQL(table, "rated"), before, limit, levels)
	if err != nil {
		return 0, fmt.Errorf("storage: prune %s %s lines: %w", table, band, err)
	}
	total := tag.RowsAffected()
	if band == logline.UnratedBand && total < int64(limit) {
		tag, err := p.pool.Exec(ctx, pruneLogsSQL(table, "unrated"), before, int64(limit)-total)
		if err != nil {
			return total, fmt.Errorf("storage: prune %s lines with no severity: %w", table, err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}
