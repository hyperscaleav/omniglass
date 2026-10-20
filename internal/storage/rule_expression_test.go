package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/expression"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// criteriaBackfillVersion is the one-time data backfill that rewrote every
// stored {op,value} criterion to {expr} (#957).
const criteriaBackfillVersion = "20261021040000"

func exprCriteria(src string) rules.Criteria { return rules.Criteria{Expr: src} }

// TestRuleCriteriaAreExpressions proves a rule's criteria are stored and
// returned as expressions (#957): the short form is lowered on write, an
// expression is judged against the prepared input (value, key, instance,
// component {name, type}), a restated short form equal to the stored
// expression writes nothing, and a bad expression is refused naming its
// criteria, line and column.
func TestRuleCriteriaAreExpressions(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	gw, all := f.gw, scope.Set{All: true}

	short, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "short-form", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Clear: ptrCriteria(criteria("eq", "up")), FireEventType: "condition-started", Enabled: ptrBool(false)}, all)
	if err != nil {
		t.Fatal(err)
	}
	if !eqC(short.FireCriteria, exprCriteria(`value == "down"`)) || short.ClearCriteria == nil || !eqC(*short.ClearCriteria, exprCriteria(`value == "up"`)) {
		t.Fatalf("stored %+v / %+v, want the lowered expressions", short.FireCriteria, short.ClearCriteria)
	}
	same, err := gw.UpdateEventRule(ctx, "", "short-form", storage.EventRulePatch{Fire: ptrCriteria(criteria("eq", "down"))}, all)
	if err != nil || same.Version != 1 {
		t.Fatalf("restating the short form: version %d (%v), want 1 (nothing changed)", same.Version, err)
	}

	typ := scalar(t, f.conn, `select coalesce(ct.name, '') from component c join product p on p.id = c.product_id
		left join component_type ct on ct.id = p.component_type_id where c.name = 'bar-1'`)
	src := `value == "down" && instance != "aux" && key == "endpoint-reachable" && component.name == "bar-1" && component.type == "` + typ + `"`
	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "expr-form", SampleKey: "endpoint-reachable",
		Fire: exprCriteria(src), Clear: ptrCriteria(exprCriteria(`value == "up"`)), Severity: "high", FireEventType: "condition-started"}, all); err != nil {
		t.Fatal(err)
	}
	prop := func(instance, value string) {
		t.Helper()
		if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
			Key: "endpoint-reachable", Instance: instance, Value: value, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	prop("aux", "down")
	prop("tcp", "down")
	alarms, err := gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(alarms) != 1 || alarms[0].DedupKey != "rule:expr-form:tcp" {
		t.Fatalf("alarms %+v, want one on tcp (aux is excluded by the expression)", alarms)
	}

	if _, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "metric-expr", SampleKey: "room-temperature",
		Fire: exprCriteria("value >= 40"), Severity: "high", FireEventType: "condition-started"}, all); err != nil {
		t.Fatal(err)
	}
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "room-temperature", Value: 41, TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.conn, `select count(*) from alarm a join event_rule r on r.id = a.raised_by_rule_id
		where r.name = 'metric-expr' and a.cleared_at is null`); n != 1 {
		t.Fatalf("the metric expression opened %d alarms, want 1", n)
	}

	for _, tc := range []struct {
		spec      storage.EventRuleSpec
		field     string
		line, col int
	}{
		{storage.EventRuleSpec{Name: "bad-1", SampleKey: "room-temperature", Fire: exprCriteria("value >= 40 &&\n  humidity > 2")}, "fire_criteria", 2, 3},
		{storage.EventRuleSpec{Name: "bad-2", SampleKey: "room-temperature", Fire: exprCriteria("value + 1")}, "fire_criteria", 1, 1},
		{storage.EventRuleSpec{Name: "bad-3", SampleKey: "endpoint-reachable", Fire: exprCriteria(`value == "down"`), Clear: ptrCriteria(exprCriteria("value > 2"))}, "clear_criteria", 1, 7},
		{storage.EventRuleSpec{Name: "bad-4", SampleKey: "room-temperature", Fire: exprCriteria("now() > 0")}, "fire_criteria", 1, 1},
	} {
		tc.spec.FireEventType = "condition-started"
		_, err := gw.CreateEventRule(ctx, "", tc.spec, all)
		var ce *rules.CriteriaError
		var ee *expression.Error
		if !errors.Is(err, storage.ErrEventRuleInvalid) || !errors.As(err, &ce) || !errors.As(err, &ee) {
			t.Errorf("%s: err %v, want ErrEventRuleInvalid over a positioned criteria error", tc.spec.Name, err)
			continue
		}
		if ce.Field != tc.field || ee.Line != tc.line || ee.Column != tc.col {
			t.Errorf("%s: %s at %d:%d, want %s at %d:%d", tc.spec.Name, ce.Field, ee.Line, ee.Column, tc.field, tc.line, tc.col)
		}
	}
}

// TestRuleCompilesOncePerVersion proves judging a batch compiles nothing
// once a rule's version has been loaded: the programs are held per (rule,
// version), not compiled per sample or per batch.
func TestRuleCompilesOncePerVersion(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "hot", SampleKey: "room-temperature",
		Fire: criteria("gt", 40), Severity: "high", FireEventType: "condition-started"}, f.all); err != nil {
		t.Fatal(err)
	}
	batch := func(base float64) {
		t.Helper()
		var ws []storage.MetricSampleWrite
		now := time.Now()
		for i := 0; i < 50; i++ {
			ws = append(ws, storage.MetricSampleWrite{OwnerKind: "component", OwnerID: "bar-1", Key: "room-temperature",
				Instance: "probe", Value: base + float64(i%3), TS: now.Add(time.Duration(i) * time.Millisecond)})
		}
		if err := f.gw.InsertMetricSamples(ctx, ws); err != nil {
			t.Fatal(err)
		}
	}
	batch(30) // the first load of the rule's version may compile
	before := rules.CompileCount()
	batch(45)
	batch(20)
	if n := rules.CompileCount() - before; n != 0 {
		t.Fatalf("two batches of 50 samples compiled %d judges, want 0 after the rule's first load", n)
	}
}

// TestRuleRunErrorHoldsAndLogsOnce proves a run over its budget holds the
// rule for that sample, logs once, and the sample still lands.
func TestRuleRunErrorHoldsAndLogsOnce(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	var mu sync.Mutex
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, w: &buf}, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "greedy", SampleKey: "endpoint-reachable",
		Fire: exprCriteria(`value + value + value contains "zz"`), Severity: "high", FireEventType: "condition-started"}, f.all); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", 6000)
	for i := 0; i < 3; i++ {
		if err := f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
			Key: "endpoint-reachable", Instance: "tcp", Value: long, TS: time.Now()}}); err != nil {
			t.Fatalf("the sample was refused for the rule's sake: %v", err)
		}
	}
	if n := count(t, f.conn, `select count(*) from property p join property_type t on t.id = p.property_type_id
		where t.name = 'endpoint-reachable' and length(p.value #>> '{}') = 6000`); n != 3 {
		t.Fatalf("%d of 3 samples landed", n)
	}
	if n := count(t, f.conn, `select count(*) from alarm a join event_rule r on r.id = a.raised_by_rule_id where r.name = 'greedy'`); n != 0 {
		t.Fatalf("an over-budget rule opened %d alarms", n)
	}
	mu.Lock()
	logged := strings.Count(buf.String(), "rule=greedy")
	mu.Unlock()
	if logged != 1 {
		t.Fatalf("the over-budget rule logged %d times, want once:\n%s", logged, buf.String())
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// TestCriteriaBackfill proves the one-time backfill (#957): every stored
// {op,value} criterion becomes the expression the Go lowering writes, rules
// judge samples after it as they did before (an upgraded reachability rule
// and a metric rule, through the real ingest), and running it again (down
// and up) or re-running the boot seed changes nothing.
func TestCriteriaBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, criteriaBackfillVersion); err != nil {
		t.Fatal(err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into severity_level (name, rank, official) values ('high', 40, true) on conflict do nothing`)
	mustExec(t, conn, `insert into event_type (name) values ('cond-x')`)
	mustExec(t, conn, `insert into property_type (name, data_type) values ('endpoint-reachable', 'string'), ('door-open', 'bool'), ('doc', 'json')`)
	type old struct {
		name, key, lane, dataType string
		fire, clear               string
	}
	// Its lowering is past the source cap: left in short form.
	overCap := map[string]bool{"over-cap": true}
	olds := []old{
		{"reach", "endpoint-reachable", "property", "string", `{"op":"eq","value":"down"}`, `{"op":"eq","value":"up"}`},
		{"hot", "room-temperature", "metric", "float", `{"op":"gt","value":40}`, ``},
		{"cool", "room-temperature", "metric", "float", `{"op":"le","value":-0.5}`, `{"op":"ge","value":12345678901234567890}`},
		{"quoted", "endpoint-reachable", "property", "string", `{"op":"ne","value":"say \"hi\" \\ back\nslash \u0001 é </x>"}`, ``},
		{"door", "door-open", "property", "bool", `{"op":"eq","value":"true"}`, `{"op":"ne","value":"on"}`},
		{"blob", "doc", "property", "json", `{"op":"eq","value":"{\"a\":1}"}`, ``},
		{"gone-key", "no-such-key", "property", "", `{"op":"eq","value":"x"}`, ``},
		{"exp-big", "room-temperature", "metric", "float", `{"op":"gt","value":1e21}`, `{"op":"lt","value":1.50E1}`},
		{"exp-small", "room-temperature", "metric", "float", `{"op":"le","value":2.5e-7}`, `{"op":"ge","value":-0}`},
		{"line-sep", "endpoint-reachable", "property", "string", "{\"op\":\"eq\",\"value\":\"a\u2028b\u2029c\"}", ``},
		{"over-cap", "endpoint-reachable", "property", "string", `{"op":"eq","value":"` + strings.Repeat("x", 4090) + `"}`, ``},
	}
	for _, o := range olds {
		mustExec(t, conn, `insert into event_rule (name, sample_key, sample_lane, fire_criteria, clear_criteria, severity, fire_event_type_id)
			values ($1, $2, $3, $4::jsonb, nullif($5, '')::jsonb, 'high', (select id from event_type where name = 'cond-x'))`,
			o.name, o.key, o.lane, o.fire, o.clear)
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over the short-form rules: %v", err)
	}
	stored := func(name, col string) *rules.Criteria {
		t.Helper()
		var raw []byte
		if err := conn.QueryRow(ctx, `select `+col+` from event_rule where name = $1`, name).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if raw == nil {
			return nil
		}
		var c rules.Criteria
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		return &c
	}
	want := func(o old, raw string) rules.Criteria {
		t.Helper()
		var c rules.Criteria
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			t.Fatal(err)
		}
		low, err := rules.Lower(c, rules.Lane(o.lane), o.dataType)
		if err != nil {
			t.Fatal(err)
		}
		return low
	}
	check := func(when string) {
		t.Helper()
		for _, o := range olds {
			if overCap[o.name] {
				// Left as it was: the migration does not fail, and the rule
				// shows its criteria error (below).
				if got := stored(o.name, "fire_criteria"); got == nil || got.Op != "eq" || got.Expr != "" {
					t.Errorf("%s: the over-cap rule's fire was rewritten (op %q, expr of %d bytes), want its short form kept", when, got.Op, len(got.Expr))
				}
				continue
			}
			if got := stored(o.name, "fire_criteria"); got == nil || !eqC(*got, want(o, o.fire)) {
				t.Errorf("%s: %s fire %+v, want %+v", when, o.name, got, want(o, o.fire))
			}
			got := stored(o.name, "clear_criteria")
			if o.clear == "" {
				if got != nil {
					t.Errorf("%s: %s gained a clear %+v", when, o.name, got)
				}
			} else if got == nil || !eqC(*got, want(o, o.clear)) {
				t.Errorf("%s: %s clear %+v, want %+v", when, o.name, got, want(o, o.clear))
			}
		}
	}
	check("after the backfill")
	if got := stored("reach", "fire_criteria"); got.Expr != `value == "down"` {
		t.Fatalf("the reachability rule's fire is %q, want the seed's own text", got.Expr)
	}

	// Again: down restores the short form, up lowers it to the same text.
	if err := migrate.RollbackBelow(dsn, criteriaBackfillVersion); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, conn, `select fire_criteria::text from event_rule where name = 'hot'`); got != `{"op": "gt", "value": 40.0}` && got != `{"op": "gt", "value": 40}` {
		t.Errorf("rolled back, the metric rule's fire is %s, want the short form", got)
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	check("after a second run")

	// The upgraded rules judge through the real ingest.
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	official, err := gw.GetEventRule(ctx, "reachability-lost", all)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	again, err := gw.GetEventRule(ctx, "reachability-lost", all)
	if err != nil || again.Version != official.Version || !eqC(again.FireCriteria, official.FireCriteria) {
		t.Fatalf("re-seeding moved the official rule: %+v (%v), was version %d", again, err, official.Version)
	}
	over, err := gw.GetEventRule(ctx, "over-cap", all)
	if err != nil {
		t.Fatal(err)
	}
	if over.CriteriaError == nil || over.CriteriaError.Field != "fire_criteria" || !strings.Contains(over.CriteriaError.Message, "4096") {
		t.Fatalf("the over-cap rule's criteria error = %+v, want fire_criteria past the 4096-byte cap", over.CriteriaError)
	}
	if reach, err := gw.GetEventRule(ctx, "reach", all); err != nil || reach.CriteriaError != nil {
		t.Fatalf("a rule that compiles shows a criteria error %+v (%v)", reach.CriteriaError, err)
	}
	product := "generic-device"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "up-1", ProductName: &product}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	openBy := func(rule string) int {
		return count(t, conn, `select count(*) from alarm a join event_rule r on r.id = a.raised_by_rule_id
			where r.name = '`+rule+`' and a.cleared_at is null`)
	}
	prop := func(v string) {
		t.Helper()
		if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "up-1",
			Key: "endpoint-reachable", Instance: "tcp", Value: v, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	metric := func(v float64) {
		t.Helper()
		if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "up-1",
			Key: "room-temperature", Value: v, TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	prop("down")
	if openBy("reach") != 1 {
		t.Fatal("the upgraded reachability rule did not fire on down")
	}
	prop("degraded") // neither down nor up: holds
	if openBy("reach") != 1 {
		t.Fatal("the upgraded reachability rule moved on a value it neither fires nor clears on")
	}
	prop("up")
	if openBy("reach") != 0 {
		t.Fatal("the upgraded reachability rule did not clear on up")
	}
	metric(41)
	if openBy("hot") != 1 {
		t.Fatal("the upgraded metric rule did not fire above 40")
	}
	metric(40)
	if openBy("hot") != 0 {
		t.Fatal("the upgraded metric rule did not clear at 40 (the negation of gt 40)")
	}
}

func eqC(a, b rules.Criteria) bool {
	return a.Expr == b.Expr && a.Op == b.Op && string(a.Value) == string(b.Value)
}

func ptrBool(b bool) *bool { return &b }

// TestCriteriaThatCannotCompileAreVisible proves a stored criterion that no
// longer compiles (here, its property's data type changed under it) is
// visible and cheap: the rule's list entry, get and state carry the error
// (field, line, column, message), the rule holds, and the failed compile is
// cached per rule version like a success, not retried per sample.
func TestCriteriaThatCannotCompileAreVisible(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "was-text", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Severity: "high", FireEventType: "condition-started"}, f.all); err != nil {
		t.Fatal(err)
	}
	mustExec(t, f.conn, `update property_type set data_type = 'bool' where name = 'endpoint-reachable'`)

	// The official reachability rule reads the same key, so two rules'
	// criteria stop compiling: the first batch compiles each once, at most.
	var before int64
	for i := 0; i < 2; i++ {
		if i == 1 {
			before = rules.CompileCount()
		}
		var ws []storage.PropertySampleWrite
		for j := 0; j < 20; j++ {
			ws = append(ws, storage.PropertySampleWrite{OwnerKind: "component", OwnerID: "bar-1", Key: "endpoint-reachable",
				Instance: "tcp", Value: "true", TS: time.Now().Add(time.Duration(j) * time.Millisecond)})
		}
		if err := f.gw.InsertPropertySamples(ctx, ws); err != nil {
			t.Fatal(err)
		}
	}
	if n := rules.CompileCount() - before; n != 0 {
		t.Fatalf("a second batch of 20 samples against rules that cannot compile compiled %d times, want 0 (the error is cached)", n)
	}
	if n := count(t, f.conn, `select count(*) from alarm a join event_rule r on r.id = a.raised_by_rule_id where r.name = 'was-text'`); n != 0 {
		t.Fatalf("a rule that cannot compile opened %d alarms", n)
	}

	got, err := f.gw.GetEventRule(ctx, "was-text", f.all)
	if err != nil {
		t.Fatal(err)
	}
	ce := got.CriteriaError
	if ce == nil || ce.Field != "fire_criteria" || ce.Line != 1 || ce.Column == 0 || ce.Message == "" {
		t.Fatalf("get: criteria error %+v, want fire_criteria with its line, column and message", ce)
	}
	list, err := f.gw.ListEventRules(ctx, f.all)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, r := range list {
		if r.Name == "was-text" {
			seen = r.CriteriaError != nil && *r.CriteriaError == *ce
		} else if r.CriteriaError != nil && r.Name != "reachability-lost" {
			t.Errorf("%s lists a criteria error %+v", r.Name, r.CriteriaError)
		}
	}
	if !seen {
		t.Fatal("the list does not carry the rule's criteria error")
	}
	st, err := f.gw.EventRuleState(ctx, "was-text", f.all)
	if err != nil || st.CriteriaError == nil || *st.CriteriaError != *ce {
		t.Fatalf("state: criteria error %+v (%v), want %+v", st, err, ce)
	}
}
