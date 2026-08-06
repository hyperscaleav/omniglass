package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestMetricContractTypeIndexes asserts the metric contract tables carry the
// metric_type_id index their property siblings have carried since init
// (product_property_property_type_idx and kin): the split migration created the
// three tables without one, so a catalog-side lookup (which contracts declare
// this metric?) walked the whole table. Names mirror the property siblings'
// post-rename shape: <table>_metric_type_idx on (metric_type_id).
func TestMetricContractTypeIndexes(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	conn := connectMigrated(t)

	for _, idx := range []struct{ table, name string }{
		{"product_metric", "product_metric_metric_type_idx"},
		{"standard_metric", "standard_metric_metric_type_idx"},
		{"location_type_metric", "location_type_metric_metric_type_idx"},
	} {
		def := indexDef(t, conn, idx.table, idx.name)
		if def == "" {
			t.Errorf("%s is missing on %s; the property sibling carries one", idx.name, idx.table)
			continue
		}
		if want := "(metric_type_id)"; !strings.Contains(def, want) {
			t.Errorf("%s = %q, want a btree on %s", idx.name, def, want)
		}
	}
}

// indexDef reads one index definition, empty when the index does not exist.
func indexDef(t *testing.T, conn *pgx.Conn, table, name string) string {
	t.Helper()
	var def string
	if err := conn.QueryRow(context.Background(),
		`select coalesce((select indexdef from pg_indexes where tablename = $1 and indexname = $2), '')`,
		table, name).Scan(&def); err != nil {
		t.Fatalf("read index %s: %v", name, err)
	}
	return def
}
