package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// audit_id was a placeholder for the audit_log row that declared a value, but it
// was never wired, typed bigint against a uuid audit_log.id so it could never FK,
// and served only a 'declared' sample provenance the design does not use
// (declared values are config, not observations). It is dropped. The neighbouring
// source_rule_version is DELIBERATELY kept: it is designed-but-unbuilt on-row
// lineage (the backtest version hinge), so this test also pins it present,
// guarding against a cleanup that mistakes "unbuilt" for "dead". value_json was
// once in that set; #588 delivered its design by folding it into property's one
// jsonb value column, so it no longer appears here.
func TestReservedLineageColumnsDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	has := func(table, column string) bool {
		var exists bool
		if err := conn.QueryRow(ctx,
			`select exists (select 1 from information_schema.columns
			 where table_name = $1 and column_name = $2)`, table, column).Scan(&exists); err != nil {
			t.Fatalf("check %s.%s: %v", table, column, err)
		}
		return exists
	}

	for _, table := range []string{"metric", "property"} {
		if has(table, "audit_id") {
			t.Errorf("%s.audit_id still exists, want it dropped (a never-wired, mistyped declared-lineage placeholder)", table)
		}
	}

	// Kept by design: unbuilt does not mean dead.
	kept := []struct{ table, column string }{
		{"metric", "source_rule_version"},
		{"property", "source_rule_version"},
		{"event", "source_rule_version"},
	}
	for _, c := range kept {
		if !has(c.table, c.column) {
			t.Errorf("%s.%s is missing, want it kept (designed-but-unbuilt on-row lineage, not a dead column)", c.table, c.column)
		}
	}
}
