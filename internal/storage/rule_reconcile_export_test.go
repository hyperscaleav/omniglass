package storage

import "github.com/hyperscaleav/omniglass/internal/rules"

// ExportRuleMarks reads a reconcile state's two marks, oldest first: how far
// the passes have read, which nothing a pass returns reports.
func ExportRuleMarks(st *RuleReconcileState) [2]RuleMark { return st.marks }

// ExportSeriesNewestSQL is the statement that reads one series' newest sample
// on a lane, parameterized ($1 component, $2 type id, $3 instance), so a test
// can EXPLAIN exactly what the reconcile and sweep run.
func ExportSeriesNewestSQL(lane string) string {
	return seriesNewestSQL(rules.Lane(lane), "$1::uuid", "$2::uuid", "$3")
}
