package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// labelledTables is the schema's own list of tables carrying the friendly
// string an operator reads, kept here rather than derived from the live
// catalog so that DROPPING the column somewhere is a failure and not a silently
// shorter list.
var labelledTables = []string{
	"choice_alternate", "command_type", "component", "component_type", "driver",
	"event_type", "human", "location", "location_type", "metric_type", "node",
	"principal_group", "product", "property_type", "role", "role_choice",
	"secret_type", "standard", "system", "system_role", "system_type", "task",
	"vendor",
}

// pennedTables carry the pen beside the label: label_generated is true when the
// platform rendered the label from a rule and false when an operator typed it.
var pennedTables = []string{"component", "location", "system"}

// TestTheLabelColumnIsCalledLabel is the schema half of #613. The Go and
// TypeScript halves are checked by the compiler; nothing checks the DATABASE
// except this, and a rename that reached the code but missed a table would fail
// at runtime on one query rather than at build time.
func TestTheLabelColumnIsCalledLabel(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// Nothing in the schema still says display_name. Asked of the whole catalog
	// rather than of the known list, so a table nobody remembered is caught too.
	var leftovers []string
	rows, err := conn.Query(ctx, `
		select table_name || '.' || column_name
		  from information_schema.columns
		 where table_schema = 'public'
		   and column_name in ('display_name', 'display_name_generated')
		 order by 1`)
	if err != nil {
		t.Fatalf("probe display_name: %v", err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		leftovers = append(leftovers, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("probe display_name: %v", err)
	}
	if len(leftovers) > 0 {
		t.Errorf("these columns still say display_name after the rename: %v", leftovers)
	}

	// Every labelled table has `label`, NOT NULL, DEFAULT ''. One spelling of
	// unset, which is what makes an `order by label` mean the same thing on
	// every one of them.
	for _, table := range labelledTables {
		var nullable, def string
		err := conn.QueryRow(ctx, `
			select is_nullable, coalesce(column_default, '')
			  from information_schema.columns
			 where table_schema = 'public' and table_name = $1 and column_name = 'label'`, table).
			Scan(&nullable, &def)
		if err == pgx.ErrNoRows {
			t.Errorf("%s has no label column", table)
			continue
		}
		if err != nil {
			t.Fatalf("probe %s.label: %v", table, err)
		}
		if nullable != "NO" {
			t.Errorf("%s.label is nullable; unset must have one spelling, '' (#613 D3)", table)
		}
		if def != "''::text" {
			t.Errorf("%s.label default = %q, want ''::text", table, def)
		}
	}

	// The pen renamed with the column it governs. A renamed label beside a
	// display_name_generated is the exact inconsistency this slice exists to
	// avoid.
	for _, table := range pennedTables {
		var nullable, def string
		err := conn.QueryRow(ctx, `
			select is_nullable, coalesce(column_default, '')
			  from information_schema.columns
			 where table_schema = 'public' and table_name = $1 and column_name = 'label_generated'`, table).
			Scan(&nullable, &def)
		if err == pgx.ErrNoRows {
			t.Errorf("%s has no label_generated column", table)
			continue
		}
		if err != nil {
			t.Fatalf("probe %s.label_generated: %v", table, err)
		}
		if nullable != "NO" || def != "false" {
			t.Errorf("%s.label_generated is %s nullable default %q, want NOT NULL default false", table, nullable, def)
		}
	}

	// `stem` and `abbrev` are NOT swept: their NULL means "inherit from an
	// ancestor", a third state with content. A sweep that collapsed them would
	// turn every inheriting type into one that overrides its parent with ''.
	for _, probe := range []struct{ table, column string }{
		{"component_type", "stem"}, {"component_type", "abbrev"},
		{"system_type", "stem"}, {"system_type", "abbrev"},
	} {
		var nullable string
		if err := conn.QueryRow(ctx, `
			select is_nullable from information_schema.columns
			 where table_schema = 'public' and table_name = $1 and column_name = $2`,
			probe.table, probe.column).Scan(&nullable); err != nil {
			t.Fatalf("probe %s.%s: %v", probe.table, probe.column, err)
		}
		if nullable != "YES" {
			t.Errorf("%s.%s is NOT NULL; its NULL means inherit-from-ancestor and must stay",
				probe.table, probe.column)
		}
	}

	// The two named NOT NULL constraints moved with their column, or the
	// schema carries a constraint whose name lies about what it constrains.
	for _, want := range []string{"standard_label_not_null", "vendor_label_not_null"} {
		var exists bool
		if err := conn.QueryRow(ctx,
			`select exists (select 1 from pg_constraint where conname = $1)`, want).Scan(&exists); err != nil {
			t.Fatalf("probe constraint %s: %v", want, err)
		}
		if !exists {
			t.Errorf("constraint %s missing; the rename left its old name behind", want)
		}
	}
}
