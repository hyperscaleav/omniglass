package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestCalcInputsHeldByID proves a rule's inputs hold their keys by id
// (#1018): an input given by name or by id is stored as the key's row, read
// back with its name now, so a key rename leaves the rule computing; an
// unchanged input given again by its new name is no change.
func TestCalcInputsHeldByID(t *testing.T) {
	r := newEvalRig(t)
	tempID := scalar(t, r.conn, `select id::text from metric_type where name = 'room-temperature'`)
	rule, err := r.create(storage.CalcRuleSpec{Name: "spread", OwnerKind: rules.OwnerComponent,
		Inputs:    []rules.CalcInput{{Alias: "t", Key: tempID}, {Alias: "n", Key: "occupancy-count"}},
		Reduce:    rules.CalcReduce{Expr: "max(t) - min(t) + max(n)"},
		OutputKey: "temp-sum"})
	if err != nil {
		t.Fatal(err)
	}
	if in := rule.Inputs[0]; in.Alias != "t" || in.Key != "room-temperature" || in.KeyID != tempID || in.Lane != rules.LaneMetric {
		t.Fatalf("input %+v, want t reading room-temperature by its id on the metric lane", in)
	}
	if n := count(t, r.conn, `select count(*) from calc_rule_input where rule_id = '`+rule.ID+`' and metric_type_id = '`+tempID+`'`); n != 1 {
		t.Fatalf("%d input rows hold room-temperature by id, want 1", n)
	}
	r.metric("room-temperature", "north", 20)
	r.metric("room-temperature", "south", 26)
	r.metric("occupancy-count", "", 1)
	r.drain()
	if got := r.calculated("temp-sum"); len(got) != 1 || got[0] != "7 spread 1" {
		t.Fatalf("temp-sum rows %v, want 7 (26 - 20 + 1)", got)
	}

	// The key is renamed: the rule still reads it, by id, and says its new
	// name.
	if _, err := r.conn.Exec(r.ctx, `update metric_type set name = 'room-temp-renamed' where id = $1`, tempID); err != nil {
		t.Fatal(err)
	}
	r.metric("room-temp-renamed", "north", 15)
	if n := r.pending(); n != 1 {
		t.Fatalf("%d marks after a sample of the renamed key, want 1", n)
	}
	r.drain()
	if got := r.calculated("temp-sum"); len(got) != 2 || got[1] != "12 spread 1" {
		t.Fatalf("temp-sum rows %v after the rename, want 12 (26 - 15 + 1)", got)
	}
	got, err := r.gw.GetCalcRule(r.ctx, "spread")
	if err != nil || got.Inputs[0].Key != "room-temp-renamed" || got.Inputs[0].KeyID != tempID || got.CalcError != nil {
		t.Fatalf("after the rename %+v (%v), want input t named room-temp-renamed, same id, compiling", got, err)
	}
	same := []rules.CalcInput{{Alias: "t", Key: "room-temp-renamed"}, {Alias: "n", Key: "occupancy-count"}}
	up, err := r.gw.UpdateCalcRule(r.ctx, "", "spread", storage.CalcRulePatch{Inputs: &same}, r.all)
	if err != nil || up.Version != 1 {
		t.Fatalf("the same inputs by their names now: %+v (%v), want no change (version 1)", up, err)
	}
	// A cycle is told by key names now, though the graph holds ids.
	_, err = r.create(storage.CalcRuleSpec{Name: "back", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "s", Key: "temp-sum"}}, Reduce: rules.CalcReduce{Expr: "max(s)"}, OutputKey: "room-temp-renamed"})
	if err == nil || !strings.Contains(err.Error(), "temp-sum -> room-temp-renamed -> temp-sum") {
		t.Fatalf("a loop through the renamed key: %v, want it named by the new name", err)
	}
}

// calcBackfillUpSQL is the calc_rule_input backfill's up section, read from
// the embedded migration so the test runs exactly what the migration does.
func calcBackfillUpSQL(t *testing.T) string {
	t.Helper()
	raw, err := db.FS.ReadFile("migrations/20261022030000_calc_rule_input_backfill.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatal("the backfill has no down marker")
	}
	return strings.TrimPrefix(up, "-- migrate:up")
}

// TestCalcRuleInputBackfill proves the #1018 data migration on a database
// that ran the calc branch before it: each {as, key} input becomes a row
// holding its key by id (a property before a metric of the same name), each
// named reducer becomes its expression over the input's alias, a rule
// reading a key no longer registered is disabled, and the backfill run again
// changes nothing. The migrated rules then read and compile through the
// gateway.
func TestCalcRuleInputBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20261022020000"); err != nil {
		t.Fatalf("roll back below the calc_rule_input table: %v", err)
	}
	ctx := context.Background()
	conn := connectDSN(t, dsn)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`insert into metric_type (name, data_type) values ('tcp-connect-time', 'float'), ('peak-tcp', 'float'), ('rule-count', 'float'),
		('shared-name', 'float')`)
	exec(`insert into property_type (name, data_type) values ('reachable', 'bool'), ('any-up', 'bool'), ('shared-name', 'string'),
		('picked', 'string')`)
	rule := func(name, inputs, reduce, out, lane string) {
		t.Helper()
		col := "output_metric_type_id"
		table := "metric_type"
		if lane == "property" {
			col, table = "output_property_type_id", "property_type"
		}
		exec(`insert into calc_rule (name, owner_kind, inputs, reduce, output_lane, `+col+`)
			values ($1, 'component', $2::jsonb, $3::jsonb, $4, (select id from `+table+` where name = $5))`, name, inputs, reduce, lane, out)
	}
	rule("peak", `[{"as": "tcp", "key": "tcp-connect-time"}]`, `{"reducer": "max"}`, "peak-tcp", "metric")
	rule("mean", `[{"as": "tcp", "key": "tcp-connect-time"}]`, `{"reducer": "average"}`, "rule-count", "metric")
	rule("up-count", `[{"as": "r", "key": "reachable"}]`, `{"reducer": "count"}`, "rule-count", "metric")
	rule("any-up", `[{"as": "r", "key": "reachable"}]`, `{"reducer": "any"}`, "any-up", "property")
	rule("pick", `[{"as": "s", "key": "shared-name"}]`, `{"reducer": "majority"}`, "picked", "property")
	rule("mixed", `[{"as": "a", "key": "tcp-connect-time"}, {"as": "b", "key": "reachable"}]`, `{"expr": "a + 1"}`, "peak-tcp", "metric")
	rule("orphan", `[{"as": "a", "key": "no-longer-here"}, {"as": "b", "key": "tcp-connect-time"}]`, `{"expr": "b"}`, "peak-tcp", "metric")

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate to head: %v", err)
	}
	snapshot := func() string {
		t.Helper()
		rows, err := conn.Query(ctx, `select c.name, c.enabled, c.reduce->>'expr', coalesce(string_agg(i.position || ':' || i.alias || ':' || i.lane || ':' ||
				coalesce(pt.name, mt.name), ',' order by i.position), '')
			from calc_rule c left join calc_rule_input i on i.rule_id = c.id
			left join property_type pt on pt.id = i.property_type_id left join metric_type mt on mt.id = i.metric_type_id
			group by c.name, c.enabled, c.reduce order by c.name`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var name, expr, inputs string
			var enabled bool
			if err := rows.Scan(&name, &enabled, &expr, &inputs); err != nil {
				t.Fatal(err)
			}
			flag := ""
			if !enabled {
				flag = " disabled"
			}
			out = append(out, name+flag+" "+expr+" "+inputs)
		}
		return strings.Join(out, "\n")
	}
	want := strings.Join([]string{
		"any-up any(r) 0:r:property:reachable",
		"mean avg(tcp) 0:tcp:metric:tcp-connect-time",
		"mixed a + 1 0:a:metric:tcp-connect-time,1:b:property:reachable",
		"orphan disabled b 1:b:metric:tcp-connect-time",
		"peak max(tcp) 0:tcp:metric:tcp-connect-time",
		"pick majority(s) 0:s:property:shared-name",
		"up-count len(r) 0:r:property:reachable",
	}, "\n")
	if got := snapshot(); got != want {
		t.Fatalf("after the backfill:\n%s\nwant:\n%s", got, want)
	}
	if n := count(t, conn, `select count(*) from information_schema.columns where table_name = 'calc_rule' and column_name = 'inputs'`); n != 0 {
		t.Error("calc_rule.inputs survived the migration")
	}

	// The backfill again, at head and from just after itself, changes
	// nothing.
	exec(calcBackfillUpSQL(t))
	if got := snapshot(); got != want {
		t.Fatalf("a second backfill at head changed the rules:\n%s", got)
	}
	if err := migrate.RollbackBelow(dsn, "20261022040000"); err != nil {
		t.Fatal(err)
	}
	exec(calcBackfillUpSQL(t))
	exec(calcBackfillUpSQL(t))
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(); got != want {
		t.Fatalf("the backfill run twice more before the drop changed the rules:\n%s", got)
	}

	// The migrated rules read through the gateway and compile.
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	for _, name := range []string{"any-up", "mean", "peak", "pick", "up-count"} {
		got, err := gw.GetCalcRule(ctx, name)
		if err != nil || got.CalcError != nil || len(got.Inputs) != 1 || got.Inputs[0].KeyID == "" {
			t.Errorf("%s reads %+v (%v), want one input held by id and a reduce that compiles", name, got, err)
		}
	}
	// A rule's stored expression that read a scalar no longer compiles
	// over a list, and says so.
	if got, err := gw.GetCalcRule(ctx, "mixed"); err != nil || got.CalcError == nil {
		t.Errorf("mixed reads %+v (%v), want a calc_error", got, err)
	}
}

// TestCalcRuleReservedAliasBackfill proves the #1018 review's follow-up
// migration: a rule moved from the earlier shape whose alias is now reserved
// (a site binding, a list function's name) is disabled, as one reading a
// deleted key is, and reads back with a calc_error on that alias; a rule
// with a fine alias is left alone; the backfill run again changes nothing.
func TestCalcRuleReservedAliasBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20261022020000"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn := connectDSN(t, dsn)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`insert into metric_type (name, data_type) values ('tcp-connect-time', 'float'), ('out-a', 'float'), ('out-b', 'float'), ('out-c', 'float')`)
	for name, alias := range map[string]string{"by-value": "value", "by-avg": "avg", "by-members": "members", "fine": "tcp"} {
		out := map[string]string{"by-value": "out-a", "by-avg": "out-b", "by-members": "out-c", "fine": "out-a"}[name]
		exec(`insert into calc_rule (name, owner_kind, inputs, reduce, output_lane, output_metric_type_id, enabled)
			values ($1, 'component', jsonb_build_array(jsonb_build_object('as', $2::text, 'key', 'tcp-connect-time')), '{"reducer": "max"}',
			'metric', (select id from metric_type where name = $3), true)`, name, alias, out)
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	enabled := func() string {
		t.Helper()
		return scalar(t, conn, `select string_agg(name || '=' || enabled::text, ',' order by name) from calc_rule`)
	}
	want := "by-avg=false,by-members=false,by-value=false,fine=true"
	if got := enabled(); got != want {
		t.Fatalf("after the migration %s, want %s", got, want)
	}
	raw, err := db.FS.ReadFile("migrations/20261022050000_calc_rule_alias_reserved.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- migrate:down")
	exec(strings.TrimPrefix(up, "-- migrate:up"))
	if got := enabled(); got != want {
		t.Fatalf("a second run %s, want %s", got, want)
	}
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	got, err := gw.GetCalcRule(ctx, "by-value")
	if err != nil || got.CalcError == nil || got.CalcError.Field != "inputs[0].alias" {
		t.Fatalf("by-value reads %+v (%v), want a calc_error on its alias", got, err)
	}
	// Every name the calc site reserves today is in the migration's list.
	for _, n := range rules.CalcReservedNames() {
		if !strings.Contains(up, "'"+n+"'") {
			t.Errorf("the migration does not reserve %q", n)
		}
	}
}
