package storage

import (
	"context"
	"fmt"
)

// BenchPathsOf exposes the batch address walk to the benchmarks in the external
// test package (bench_test.go), which cannot reach [PathsOf] on its own: it
// takes the unexported querier and scopeTable, and *PG does not export its pool.
// This shim is declared in a _test.go file, so it exists only in the test binary
// and ships in nothing.
//
// It exists so the walk can be measured on the SAME estate as the list that
// calls it. A list is the scoped-list query PLUS this walk, and only one of the
// two is the recursive CTE #648 rewrote; measured separately, the two numbers
// subtract, and a plan regression in the walk shows here at full strength where
// the list would dilute it.
//
// The plane is named rather than typed because scopeTable is unexported too.
func (p *PG) BenchPathsOf(ctx context.Context, plane string, ids []string) (map[string][]string, error) {
	var tbl scopeTable
	switch plane {
	case "component":
		tbl = componentTable
	case "system":
		tbl = systemTable
	case "location":
		tbl = locationTable
	default:
		return nil, fmt.Errorf("storage: BenchPathsOf: unknown plane %q", plane)
	}
	return PathsOf(ctx, p.pool, tbl, ids)
}
