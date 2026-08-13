// Package accesspath answers one narrow question about a statement: which
// access path the planner CAN reach a relation by. It is the repo's third
// performance instrument, after round-trip counting (storagetest/querycount)
// and the wall-clock benchmarks, and like counting it is deterministic and it
// gates.
//
// # Usability, not preference
//
// ADR-0094 deferred `EXPLAIN` assertions because a plan depends on table
// statistics: a test fixture is far smaller than production, so the plan the
// planner PREFERS on a fixture is not the plan it prefers in production, and an
// assertion about the chosen plan pins the fixture's size rather than the
// query's shape. That reasoning is untouched here and this package must never be
// used to assert a preference.
//
// Whether an index is USABLE BY THE QUERY AT ALL is a different question, and it
// is the one that breaks silently. An index can exist in the catalog and be
// unreachable by the read it was built for, because a predicate was rewritten
// (a function wrapped around the column, a type coerced, the leading column
// dropped from the filter) or because a PARTIAL index's predicate stopped being
// provable from the query's own clauses. A `pg_indexes` test passes throughout
// all of that, and so does a round-trip count: the statement count is identical
// whether the scan is an index scan or a sequential one.
//
// [Explain] asks that question by planning the statement with `enable_seqscan`
// off, which prices the sequential path out rather than forbidding it. The
// answer does not depend on how many rows the fixture holds:
//
//   - if the access path exists, the scan node names the index it uses;
//   - if it does not, Postgres still plans the sequential scan, and marks it
//     Disabled, because it was the only path left.
//
// Measured over the health reads on an empty database, a 45-row fixture with no
// statistics at all, and the same fixture analyzed: the JOIN shapes around the
// scan changed in all three (hash join to merge join, a sort appearing and
// disappearing), and the scan of `property` was the same index scan with the
// same index condition in every one. That is the line this package draws.
// Assert the access path of ONE relation. Never assert the plan's shape.
//
// # An index name alone is not the assertion
//
// A scan node can name an index and still not be USING it as an access path: if
// the query's filter can no longer be turned into an index condition, the
// planner may walk the whole index and filter afterwards, which reads as a pass
// to an assertion that only greps for the index name. Both mutations that shape
// produces (coercing the filtered column, dropping the leading column from the
// filter) leave the index name in the plan and empty out [Scan.Cond]. So an
// assertion checks the condition too, which is what [Plan.MustReach] does.
package accesspath

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Scan is one relation's access path in a plan.
type Scan struct {
	Relation string // the table, as Postgres names it
	Alias    string // the alias the statement gave it, if any
	Node     string // "Index Scan", "Seq Scan", "Index Only Scan", ...
	Index    string // the index the scan reads, empty for a sequential scan
	Cond     string // the index condition, empty when the scan carries none
	Disabled bool   // the planner took this node only because every alternative was priced out
}

// String renders a scan the way a failure message wants to read it.
func (s Scan) String() string {
	out := s.Node + " on " + s.Relation
	if s.Index != "" {
		out += " using " + s.Index
	}
	if s.Cond != "" {
		out += " (cond " + s.Cond + ")"
	} else if s.Index != "" {
		out += " (NO index condition: the index is walked, not searched)"
	}
	if s.Disabled {
		out += " [disabled: no other path existed]"
	}
	return out
}

// Plan is every scan node in one statement's plan, in tree order.
type Plan []Scan

// Scans returns the plan's scans of one relation. More than one is normal: a
// statement can join a table to itself, or scan it once per CTE.
func (p Plan) Scans(relation string) []Scan {
	var out []Scan
	for _, s := range p {
		if s.Relation == relation {
			out = append(out, s)
		}
	}
	return out
}

// String renders the whole plan one scan per line, for a failure message.
func (p Plan) String() string {
	lines := make([]string, len(p))
	for i, s := range p {
		lines[i] = s.String()
	}
	return strings.Join(lines, "\n\t")
}

// MustReach reports whether every scan of relation reaches index as a searched
// access path: the right index, an index condition on it, and not a node the
// planner was forced into. The message names what it found when the answer is
// no, since "the index was not reached" is useless without the path that was
// taken instead.
func (p Plan) MustReach(relation, index string) (bool, string) {
	scans := p.Scans(relation)
	if len(scans) == 0 {
		return false, fmt.Sprintf("the plan never scans %s at all, so nothing was proved about %s; the whole plan was:\n\t%s", relation, index, p)
	}
	for _, s := range scans {
		switch {
		case s.Index != index:
			return false, fmt.Sprintf("%s is reached by %q, not by %s: %s", relation, s.Node, index, s)
		case s.Cond == "":
			return false, fmt.Sprintf("%s names %s but carries no index condition, so the index is walked rather than searched (a coerced or dropped predicate does this): %s", relation, index, s)
		case s.Disabled:
			return false, fmt.Sprintf("%s reaches %s only as a disabled node: %s", relation, index, s)
		}
	}
	return true, ""
}

// Explain plans sql (with args bound, so the planner sees the values the
// gateway really passes) and returns the access path of every relation it
// scans, with sequential scans priced out. See the package doc for why that
// setting is what makes the answer independent of the fixture's size.
//
// It takes a *pgx.Conn rather than a pool because it changes a session setting:
// on a pooled connection that change would outlive the call and silently
// re-plan whatever ran next on the same connection.
func Explain(ctx context.Context, conn *pgx.Conn, sql string, args ...any) (Plan, error) {
	if _, err := conn.Exec(ctx, "set enable_seqscan = off"); err != nil {
		return nil, fmt.Errorf("accesspath: disable seqscan: %w", err)
	}
	defer func() { _, _ = conn.Exec(ctx, "reset enable_seqscan") }()

	var doc []byte
	if err := conn.QueryRow(ctx, "explain (format json, costs off) "+sql, args...).Scan(&doc); err != nil {
		return nil, fmt.Errorf("accesspath: explain: %w", err)
	}
	return Parse(doc)
}

// Parse reads the access paths out of an `EXPLAIN (FORMAT JSON)` document. It is
// separated from [Explain] so the tree walk, which is the only part with any
// logic in it, is testable without a database.
func Parse(doc []byte) (Plan, error) {
	var roots []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(doc, &roots); err != nil {
		return nil, fmt.Errorf("accesspath: parse explain output: %w", err)
	}
	var out Plan
	for _, r := range roots {
		out = append(out, walk(r.Plan)...)
	}
	return out, nil
}

// walk collects the scan nodes of one plan subtree, depth first. A node is a
// scan when it names a relation, which is what tells `Seq Scan on property`
// apart from the joins, sorts and CTE scans above it.
func walk(node map[string]any) Plan {
	var out Plan
	if rel, ok := node["Relation Name"].(string); ok {
		out = append(out, Scan{
			Relation: rel,
			Alias:    str(node["Alias"]),
			Node:     str(node["Node Type"]),
			Index:    str(node["Index Name"]),
			Cond:     str(node["Index Cond"]),
			Disabled: node["Disabled"] == true,
		})
	}
	children, ok := node["Plans"].([]any)
	if !ok {
		return out
	}
	for _, c := range children {
		child, ok := c.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, walk(child)...)
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
