package storage_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestCatalogFingerprintMovesWithEveryRegistryTable: the fingerprint the
// ingest registry cache keys on (#1279) holds still while nothing changes, and
// moves on any insert, update or delete in any table the registry is built
// from, so a cached registry can never outlive the catalog it was read from.
func TestCatalogFingerprintMovesWithEveryRegistryTable(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	fingerprint := func() string {
		t.Helper()
		fp, err := gw.CatalogFingerprint(ctx)
		if err != nil {
			t.Fatalf("fingerprint: %v", err)
		}
		if fp == "" {
			t.Fatal("empty fingerprint")
		}
		return fp
	}
	exec := func(sql string) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	prev := fingerprint()
	if again := fingerprint(); again != prev {
		t.Fatalf("the fingerprint moved with no change: %s then %s", prev, again)
	}
	for _, step := range []struct{ name, sql string }{
		{"metric_type insert", `insert into metric_type (name, data_type) values ('fp-metric', 'float')`},
		{"metric_type update", `update metric_type set description = 'changed' where name = 'fp-metric'`},
		{"property_type insert", `insert into property_type (name, data_type) values ('fp-property', 'string')`},
		{"property_type update", `update property_type set description = 'changed' where name = 'fp-property'`},
		{"event_type insert", `insert into event_type (name) values ('fp-event')`},
		{"event_type update", `update event_type set description = 'changed' where name = 'fp-event'`},
		{"calc_rule insert", `insert into calc_rule (name, owner_kind, reduce, output_lane, output_metric_type_id)
			values ('fp-calc', 'component', '{}', 'metric', (select id from metric_type where name = 'fp-metric'))`},
		{"calc_rule update", `update calc_rule set enabled = false where name = 'fp-calc'`},
		{"command_type insert", `insert into command_type (name, label, target_property_type_id)
			values ('fp-command', 'FP command', (select id from property_type where name = 'fp-property'))`},
		{"command_type update", `update command_type set description = 'changed' where name = 'fp-command'`},
		{"command_type delete", `delete from command_type where name = 'fp-command'`},
		{"calc_rule delete", `delete from calc_rule where name = 'fp-calc'`},
		{"event_type delete", `delete from event_type where name = 'fp-event'`},
		{"property_type delete", `delete from property_type where name = 'fp-property'`},
		{"metric_type delete", `delete from metric_type where name = 'fp-metric'`},
	} {
		exec(step.sql)
		next := fingerprint()
		if next == prev {
			t.Errorf("%s did not move the fingerprint", step.name)
		}
		prev = next
	}
}

// TestCatalogFingerprintCoversThePlannedRelations is the exact half of the
// drift guard (#1279): each registry list statement is planned as written,
// and every relation the plan reads, in any subplan, must be a table the
// catalog fingerprint covers. Unlike the text scan in the internal test, a
// plan sees comma joins, quoted names, views and functions over tables.
func TestCatalogFingerprintCoversThePlannedRelations(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	for _, q := range storage.ExportRegistryListSQL {
		var plan []byte
		if err := conn.QueryRow(ctx, "explain (format json) "+q).Scan(&plan); err != nil {
			t.Fatalf("explain %q: %v", q, err)
		}
		var tree any
		if err := json.Unmarshal(plan, &tree); err != nil {
			t.Fatalf("plan json: %v", err)
		}
		relations := plannedRelations(tree)
		if len(relations) == 0 {
			t.Fatalf("the plan of %q reads no relation: the guard would pass vacuously", q)
		}
		for _, r := range relations {
			if !slices.Contains(storage.ExportRegistryTables, r) {
				t.Errorf("a registry list statement reads %s, which the catalog fingerprint does not cover; add it to registryTables", r)
			}
		}
	}
}

// plannedRelations collects every "Relation Name" in an EXPLAIN (FORMAT JSON)
// tree, subplans included.
func plannedRelations(node any) []string {
	var out []string
	switch n := node.(type) {
	case map[string]any:
		for k, v := range n {
			if name, ok := v.(string); ok && k == "Relation Name" {
				out = append(out, name)
				continue
			}
			out = append(out, plannedRelations(v)...)
		}
	case []any:
		for _, v := range n {
			out = append(out, plannedRelations(v)...)
		}
	}
	return out
}
