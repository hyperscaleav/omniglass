package seed_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// officialTables enumerates every catalog carrying `official` from the live
// schema, so a catalog added later is held to the namespace rule without anyone
// listing it here (#1431).
func officialTables(t *testing.T, ctx context.Context, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(ctx, `select table_name from information_schema.columns
		where table_schema = 'public' and column_name = 'official' order by table_name`)
	if err != nil {
		t.Fatalf("list official tables: %v", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect official tables: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("no table carries official: the enumeration is broken")
	}
	return tables
}

// TestEveryOfficialCatalogIsNamespacedAndTheSeedWritesOnlyOg holds the two
// halves of the namespace rule on every catalog the schema reports:
//
//   - the boot seed lands its rows in `og`, and nothing else is `og`;
//   - the seed never writes an operator's row. Every seeded row is turned into
//     an operator's (`local`, not official, a changed label where the table has
//     one), the seed runs again, and each of those rows must read back exactly
//     as the operator left it. Before #1431 the authoritative upsert took every
//     such row back.
func TestEveryOfficialCatalogIsNamespacedAndTheSeedWritesOnlyOg(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tables := officialTables(t, ctx, conn)

	seeded := 0
	for _, table := range tables {
		id := pgx.Identifier{table}.Sanitize()
		var stray, unofficialOg, n int
		if err := conn.QueryRow(ctx, `select
				count(*) filter (where official and namespace <> 'og'),
				count(*) filter (where namespace = 'og' and not official),
				count(*) filter (where official)
			from `+id).Scan(&stray, &unofficialOg, &n); err != nil {
			t.Fatalf("%s: read namespaces (every official catalog needs a namespace column): %v", table, err)
		}
		if stray != 0 {
			t.Errorf("%s: %d seeded rows outside og", table, stray)
		}
		if unofficialOg != 0 {
			t.Errorf("%s: %d og rows not official", table, unofficialOg)
		}
		seeded += n
	}
	if seeded == 0 {
		t.Fatal("the seed shipped no official row: nothing was tested")
	}

	// Every seeded row becomes an operator's row holding the shipped name. The
	// seed's child rows (a product's declared-property contract) are snapshotted
	// too: they carry no `official` of their own, and a skipped parent must not
	// take its children's writes.
	before := map[string]map[string]string{}
	snapshotted := append(append([]string{}, tables...), "product_property")
	for _, table := range snapshotted {
		id := pgx.Identifier{table}.Sanitize()
		var hasLabel bool
		if err := conn.QueryRow(ctx, `select exists (select 1 from information_schema.columns
			where table_schema = 'public' and table_name = $1 and column_name = 'label')`, table).Scan(&hasLabel); err != nil {
			t.Fatalf("%s: probe label: %v", table, err)
		}
		switch table {
		case "product_property":
			if _, err := conn.Exec(ctx, `update product_property set required = not required`); err != nil {
				t.Fatalf("product_property: make the contract the operator's: %v", err)
			}
		default:
			set := `namespace = 'local', official = false`
			if hasLabel {
				set += `, label = 'Mine'`
			}
			if _, err := conn.Exec(ctx, `update `+id+` set `+set+` where official`); err != nil {
				t.Fatalf("%s: make the shipped rows an operator's: %v", table, err)
			}
		}
		rows, err := conn.Query(ctx, `select id::text, to_jsonb(t)::text from `+id+` t`)
		if err != nil {
			t.Fatalf("%s: snapshot: %v", table, err)
		}
		snap := map[string]string{}
		for rows.Next() {
			var rid, img string
			if err := rows.Scan(&rid, &img); err != nil {
				t.Fatalf("%s: scan snapshot: %v", table, err)
			}
			snap[rid] = img
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("%s: snapshot rows: %v", table, err)
		}
		before[table] = snap
	}

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("a release meeting the operator's rows must still boot: %v", err)
	}

	for _, table := range snapshotted {
		id := pgx.Identifier{table}.Sanitize()
		for rid, img := range before[table] {
			var now string
			if err := conn.QueryRow(ctx, `select to_jsonb(t)::text from `+id+` t where id::text = $1`, rid).Scan(&now); err != nil {
				t.Errorf("%s: the operator's row %s is gone after the seed: %v", table, rid, err)
				continue
			}
			if now != img {
				t.Errorf("%s: the seed wrote the operator's row %s\nbefore: %s\nafter:  %s", table, rid, img, now)
			}
		}
	}
}
