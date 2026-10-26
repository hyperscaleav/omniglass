package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestNodeSchema proves the node table is the kind='node' principal detail table:
// it is keyed by principal_id (a FK to principal), still carries the unique name
// the collection FKs reference, has enrolled_at, and no longer has the bespoke
// enrollment_token column (the enrollment secret is a credential row now).
// Skipped under -short.
func TestNodeSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// The detail-table shape: principal_id and enrolled_at present.
	for _, col := range []string{"principal_id", "name", "enrolled_at"} {
		var exists bool
		if err := conn.QueryRow(ctx,
			`select exists (select 1 from information_schema.columns where table_name = 'node' and column_name = $1)`,
			col).Scan(&exists); err != nil {
			t.Fatalf("probe node.%s: %v", col, err)
		}
		if !exists {
			t.Errorf("column node.%s missing after migrate", col)
		}
	}

	// The bespoke enrollment_token column is gone (superseded by a credential row).
	var tokenExists bool
	if err := conn.QueryRow(ctx,
		`select exists (select 1 from information_schema.columns where table_name = 'node' and column_name = 'enrollment_token')`).Scan(&tokenExists); err != nil {
		t.Fatalf("probe node.enrollment_token: %v", err)
	}
	if tokenExists {
		t.Errorf("column node.enrollment_token should be gone (enrollment is a credential row)")
	}

	// principal_id is the primary key and a foreign key into principal.
	var pkCol string
	if err := conn.QueryRow(ctx, `
		select kcu.column_name
		from information_schema.table_constraints tc
		join information_schema.key_column_usage kcu on kcu.constraint_name = tc.constraint_name
		where tc.table_name = 'node' and tc.constraint_type = 'PRIMARY KEY'`).Scan(&pkCol); err != nil {
		t.Fatalf("probe node primary key: %v", err)
	}
	if pkCol != "principal_id" {
		t.Errorf("node primary key = %q, want principal_id", pkCol)
	}

	// No name rule lives in the schema (#570). The node table used to carry its
	// own CHECK (node_name_subject_safe_check, forbidding only a dot, a star, a
	// '>' and whitespace), a fifth name rule disagreeing with the one the
	// gateway enforces: it admitted uppercase and underscores the entity name
	// rule refuses. The gateway's rule is the one rule (TestNodeNameSubjectSafety
	// proves it still refuses every character the CHECK caught).
	var checks []string
	rows, err := conn.Query(ctx, `
		select conname from pg_constraint
		where conrelid = 'node'::regclass and contype = 'c'
		order by conname`)
	if err != nil {
		t.Fatalf("probe node check constraints: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan node check constraint: %v", err)
		}
		checks = append(checks, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("node check constraints: %v", err)
	}
	if len(checks) != 0 {
		t.Errorf("node carries CHECK constraints %v, want none: the name rule lives in the gateway", checks)
	}
}
