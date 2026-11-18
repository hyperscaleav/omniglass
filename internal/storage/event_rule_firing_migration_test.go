package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A rule fires on a component and endpoint and clears there; while it has
// fired and not cleared, that endpoint is one of its firings (#2082,
// ADR-2038). One migration renames the rule's in-force table and every
// constraint and index named for it. This test stands a database just below
// the rename, plants a firing, and migrates forward over it.

const eventRuleFiringVersion = "20261118010412"

// schemaEventRuleConditionNames is every table, constraint and index in the
// public schema still named for the retired condition: the rule's in-force
// table and the alarm's one-open-row index.
const schemaEventRuleConditionNames = `
	select relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
	 where n.nspname = 'public' and relname ~ 'event_rule_condition|alarm_open_condition'
	union all
	select conname from pg_constraint con join pg_namespace n on n.oid = con.connamespace
	 where n.nspname = 'public' and conname ~ 'event_rule_condition'
	order by 1`

// TestEventRuleFiringRenameCarriesEveryRow: across the rename no schema name
// spells event_rule_condition, the planted firing keeps its rule, component,
// endpoint and since, the generic event pair reads rule-fired and
// rule-cleared, and the renamed key still refuses a second firing for one
// rule, component and endpoint. Rolled back, the row sits under the old names.
func TestEventRuleFiringRenameCarriesEveryRow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, eventRuleFiringVersion); err != nil {
		t.Fatalf("roll back below the rename: %v", err)
	}
	conn := connectDSN(t, dsn)

	mustExec(t, conn, `insert into event_type (name) values ('firing-up'), ('firing-down'), ('condition-started'), ('condition-ended')`)
	mustExec(t, conn, `insert into event_rule (name, sample_key, sample_lane, fire_criteria, fire_event_type_id, clear_event_type_id)
		values ('firing-rule', 'endpoint-reachable', 'property', '{"op":"eq","value":"down"}',
		        (select id from event_type where name = 'firing-up'), (select id from event_type where name = 'firing-down'))`)
	mustExec(t, conn, `insert into component (name, product_id)
		values ('firing-c', (select id from product where name = 'generic-device'))`)
	mustExec(t, conn, `insert into event_rule_condition (rule_id, component_id, instance, since)
		values ((select id from event_rule where name = 'firing-rule'),
		        (select id from component where name = 'firing-c'), 'tcp', '2026-11-18T00:00:00Z')`)

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	if got := snapshotRows(t, conn, `select name from event_type where name ~ '^(condition|rule)-' order by name`, true); len(got) != 2 || got[0] != "rule-cleared" || got[1] != "rule-fired" {
		t.Errorf("after the rename the generic pair is %v, want [rule-cleared rule-fired]", got)
	}
	if left := snapshotRows(t, conn, schemaEventRuleConditionNames, true); len(left) > 0 {
		t.Errorf("after the rename the schema still spells event_rule_condition: %v", left)
	}
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from event_rule_firing f
		join event_rule r on r.id = f.rule_id join component c on c.id = f.component_id
		where r.name = 'firing-rule' and c.name = 'firing-c' and f.instance = 'tcp'
		  and f.since = '2026-11-18T00:00:00Z'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("the planted firing: %d (%v), want 1", n, err)
	}
	if _, err := conn.Exec(ctx, `insert into event_rule_firing (rule_id, component_id, instance, since)
		select rule_id, component_id, instance, now() from event_rule_firing`); err == nil {
		t.Error("a second firing for one rule, component and endpoint was accepted")
	}
	var keyed int
	if err := conn.QueryRow(ctx, `select count(*) from pg_constraint where conname in
		('event_rule_firing_pkey', 'event_rule_firing_rule_id_fkey', 'event_rule_firing_component_id_fkey')`).Scan(&keyed); err != nil || keyed != 3 {
		t.Errorf("renamed constraints: %d (%v), want 3", keyed, err)
	}
	if err := conn.QueryRow(ctx, `select count(*) from pg_class where relname = 'alarm_open_identity_key'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("the alarm's one-open-row index: %d (%v), want alarm_open_identity_key", n, err)
	}

	if err := migrate.RollbackBelow(dsn, eventRuleFiringVersion); err != nil {
		t.Fatalf("roll back the rename: %v", err)
	}
	for _, q := range []string{
		`select count(*) from event_rule_condition where instance = 'tcp'`,
		`select count(*) from pg_constraint where conname = 'event_rule_condition_pkey'`,
		`select count(*) from event_type where name = 'condition-started'`,
		`select count(*) from pg_class where relname = 'alarm_open_condition_key'`,
	} {
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("rolled back, %s: %d (%v), want 1", q, n, err)
		}
	}
}
