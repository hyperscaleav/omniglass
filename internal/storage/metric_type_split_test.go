package storage_test

import (
	"context"
	"testing"
)

// TestMetricTypeSplitMigration asserts the #587 split: the catalog is two lanes
// partitioned by value type, each carrying only its own facts, and neither able
// to hold the other's row.
func TestMetricTypeSplitMigration(t *testing.T) {
	ctx := context.Background()
	conn := connectMigrated(t)

	// metric_type takes a numeric row with the numeric facts.
	mustExec(t, conn, `insert into metric_type (name, label, data_type, unit, "precision")
		values ('cpu-load', 'CPU load', 'float', 'ratio', 2)`)
	// property_type takes a text row.
	mustExec(t, conn, `insert into property_type (name, label, data_type)
		values ('power-state', 'Power state', 'string')`)

	// Neither lane holds the other's row: the data_type domains are disjoint.
	if _, err := conn.Exec(ctx, `insert into metric_type (name, data_type) values ('bad-metric', 'string')`); err == nil {
		t.Error("metric_type accepted a string row; the lane is numeric")
	}
	if _, err := conn.Exec(ctx, `insert into property_type (name, data_type) values ('bad-prop', 'float')`); err == nil {
		t.Error("property_type accepted a float row; numbers are metrics")
	}

	// kind is gone, and so are the metric facts on the property side: the lane
	// is the table now, and a discriminator column would be a second copy of it.
	for _, col := range []string{"kind", "unit", "precision"} {
		var present bool
		if err := conn.QueryRow(ctx,
			`select exists (select 1 from information_schema.columns
			 where table_name = 'property_type' and column_name = $1)`, col).Scan(&present); err != nil {
			t.Fatalf("read property_type column %s: %v", col, err)
		}
		if present {
			t.Errorf("property_type still carries %q, a fact of the other lane or of the retired discriminator", col)
		}
	}

	// The metric sample table points at its own catalog, by the lane's name.
	var target string
	if err := conn.QueryRow(ctx, `
		select ccu.table_name
		from information_schema.table_constraints tc
		join information_schema.constraint_column_usage ccu on ccu.constraint_name = tc.constraint_name
		where tc.table_name = 'metric' and tc.constraint_type = 'FOREIGN KEY'
		  and tc.constraint_name = 'metric_metric_type_id_fkey'`).Scan(&target); err != nil {
		t.Fatalf("metric has no metric_type_id FK: %v", err)
	}
	if target != "metric_type" {
		t.Errorf("metric's catalog FK points at %q, want metric_type", target)
	}

	// Each contract family has a metric sibling, so a contract can declare a
	// number without borrowing the property table.
	for _, table := range []string{"product_metric", "standard_metric", "location_type_metric"} {
		var present bool
		if err := conn.QueryRow(ctx,
			`select exists (select 1 from information_schema.tables where table_name = $1)`, table).Scan(&present); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !present {
			t.Errorf("contract table %s is missing", table)
		}
	}
}
