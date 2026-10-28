package storage

import (
	"strings"
	"testing"
)

// TestPruneStatementsKeepTheirShape pins two properties of every retention
// prune statement that a plan test run with sequential scans priced out cannot
// see, since it reaches the index either way (#417):
//   - the outer delete finds its rows by primary key through an array of the
//     chosen ids, not an IN subquery a small table's planner turns into a hash
//     join over a sequential scan;
//   - a log prune's candidates are ordered as their index is, so the LIMIT stops
//     inside the index rather than the planner preferring to read the table once
//     retention has made severity and time correlate.
func TestPruneStatementsKeepTheirShape(t *testing.T) {
	var all []string
	for lane := range sampleLanes {
		for _, arc := range sampleArcs {
			all = append(all, pruneSamplesSQL(lane, arc, false), pruneSamplesSQL(lane, arc, true))
		}
	}
	for table := range logTables {
		rated, unrated := pruneLogsSQL(table, "rated"), pruneLogsSQL(table, "unrated")
		all = append(all, rated, unrated)
		if !strings.Contains(rated, "order by c.severity, c.ts limit $2") {
			t.Errorf("the %s rated prune is not ordered as (severity, ts):\n%s", table, rated)
		}
		if !strings.Contains(unrated, "order by c.ts limit $2") {
			t.Errorf("the %s unrated prune is not ordered as (ts):\n%s", table, unrated)
		}
	}
	for _, sql := range all {
		if !strings.Contains(strings.Join(strings.Fields(sql), " "), "where id = any(array(") {
			t.Errorf("a prune's outer delete does not find its rows by primary key:\n%s", sql)
		}
	}
}
