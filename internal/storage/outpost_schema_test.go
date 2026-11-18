package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestOutpostSchema proves the outpost table is the kind='outpost' user detail table:
// it is keyed by user_id (a FK to user), still carries the unique name
// the collection FKs reference, has enrolled_at, and no longer has the bespoke
// enrollment_token column (the enrollment secret is a credential row now).
// Skipped under -short.
func TestOutpostSchema(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// The detail-table shape: user_id and enrolled_at present.
	for _, col := range []string{"user_id", "name", "enrolled_at"} {
		var exists bool
		if err := conn.QueryRow(ctx,
			`select exists (select 1 from information_schema.columns where table_name = 'outpost' and column_name = $1)`,
			col).Scan(&exists); err != nil {
			t.Fatalf("probe outpost.%s: %v", col, err)
		}
		if !exists {
			t.Errorf("column outpost.%s missing after migrate", col)
		}
	}

	// The bespoke enrollment_token column is gone (superseded by a credential row).
	var tokenExists bool
	if err := conn.QueryRow(ctx,
		`select exists (select 1 from information_schema.columns where table_name = 'outpost' and column_name = 'enrollment_token')`).Scan(&tokenExists); err != nil {
		t.Fatalf("probe outpost.enrollment_token: %v", err)
	}
	if tokenExists {
		t.Errorf("column outpost.enrollment_token should be gone (enrollment is a credential row)")
	}

	// user_id is the primary key and a foreign key into user.
	var pkCol string
	if err := conn.QueryRow(ctx, `
		select kcu.column_name
		from information_schema.table_constraints tc
		join information_schema.key_column_usage kcu on kcu.constraint_name = tc.constraint_name
		where tc.table_name = 'outpost' and tc.constraint_type = 'PRIMARY KEY'`).Scan(&pkCol); err != nil {
		t.Fatalf("probe outpost primary key: %v", err)
	}
	if pkCol != "user_id" {
		t.Errorf("outpost primary key = %q, want user_id", pkCol)
	}

	// No name rule lives in the schema (#570). The outpost table used to carry its
	// own CHECK (outpost_name_subject_safe_check, forbidding only a dot, a star, a
	// '>' and whitespace), a fifth name rule disagreeing with the one the
	// gateway enforces: it admitted uppercase and underscores the entity name
	// rule refuses. The gateway's rule is the one rule (TestOutpostNameSubjectSafety
	// proves it still refuses every character the CHECK caught).
	var checks []string
	rows, err := conn.Query(ctx, `
		select conname from pg_constraint
		where conrelid = 'outpost'::regclass and contype = 'c'
		order by conname`)
	if err != nil {
		t.Fatalf("probe outpost check constraints: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan outpost check constraint: %v", err)
		}
		checks = append(checks, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("outpost check constraints: %v", err)
	}
	if len(checks) != 0 {
		t.Errorf("outpost carries CHECK constraints %v, want none: the name rule lives in the gateway", checks)
	}
}
