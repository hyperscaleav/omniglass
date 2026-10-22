package storage_test

import (
	"context"
	"testing"
)

// TestNoDuplicateIndexes refuses two indexes on one table with identical
// definitions (#971): same access method, key columns, operator classes,
// collations, sort options, expressions, predicate and uniqueness. A twin
// answers no read the other cannot, and every insert maintains both, which is
// how property_owner_idx and property_series_idx both lived on the property
// lane until #971 dropped the older name. The catalog is the oracle, so a
// future migration that recreates an existing index under a new name turns
// this red.
func TestNoDuplicateIndexes(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn := connectMigrated(t)

	rows, err := conn.Query(ctx, `
		select c.relname, string_agg(i.relname, ', ' order by i.relname)
		from pg_index x
		join pg_class i on i.oid = x.indexrelid
		join pg_class c on c.oid = x.indrelid
		join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = 'public'
		group by c.relname, i.relam, x.indkey::text, x.indclass::text,
			x.indcollation::text, x.indoption::text, x.indisunique,
			x.indnullsnotdistinct,
			coalesce(pg_get_expr(x.indexprs, x.indrelid), ''),
			coalesce(pg_get_expr(x.indpred, x.indrelid), '')
		having count(*) > 1
		order by 1, 2`)
	if err != nil {
		t.Fatalf("query duplicate indexes: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, names string
		if err := rows.Scan(&table, &names); err != nil {
			t.Fatalf("scan: %v", err)
		}
		t.Errorf("table %s has indexes with identical definitions: %s (drop all but one)", table, names)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
}
