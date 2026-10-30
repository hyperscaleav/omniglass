package storage_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A sample naming its namespace lands on that namespace's row even when the
// operator's row of the same name shadows it (#1433), on every lane; a bare
// sample still lands on the operator's. Each lane's x is held in both tiers.
func TestAQualifiedSampleLandsOnItsNamespacesRow(t *testing.T) {
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
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "ns-metric", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.UpsertMetricType(ctx, storage.MetricType{Name: "ns-metric", DataType: "float", Official: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "ns-prop", DataType: "string"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.UpsertPropertyType(ctx, storage.PropertyType{Name: "ns-prop", DataType: "string", Official: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{Name: "ns-event"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.UpsertEventType(ctx, storage.EventType{Name: "ns-event", Official: true}); err != nil {
		t.Fatal(err)
	}

	w := storage.PushWriter()
	if err := gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "codec-1", Namespace: "og", Key: "ns-metric", Value: 1},
		{OwnerKind: "component", OwnerID: "codec-1", Key: "ns-metric", Value: 2},
	}); err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if err := gw.InsertPropertySamples(ctx, w, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: "codec-1", Namespace: "og", Key: "ns-prop", Value: "a"},
		{OwnerKind: "component", OwnerID: "codec-1", Key: "ns-prop", Value: "b"},
	}); err != nil {
		t.Fatalf("properties: %v", err)
	}
	if err := gw.InsertEvents(ctx, w, []storage.EventWrite{
		{OwnerKind: "component", OwnerID: "codec-1", Namespace: "og", Key: "ns-event", Message: "a"},
		{OwnerKind: "component", OwnerID: "codec-1", Key: "ns-event", Message: "b"},
	}); err != nil {
		t.Fatalf("events: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	landed := func(sql string) map[string]string {
		t.Helper()
		rows, err := conn.Query(ctx, sql)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		got := map[string]string{}
		for rows.Next() {
			var value, ns string
			if err := rows.Scan(&value, &ns); err != nil {
				t.Fatal(err)
			}
			got[value] = ns
		}
		return got
	}
	for lane, sql := range map[string]string{
		"metric": `select m.value::text, t.namespace from metric m join metric_series s on s.id = m.series_id
			join metric_type t on t.id = s.metric_type_id where t.name = 'ns-metric'`,
		"property": `select p.value #>> '{}', t.namespace from property p join property_type t on t.id = p.property_type_id
			where t.name = 'ns-prop'`,
		"event": `select e.message, t.namespace from event e join event_type t on t.id = e.event_type_id
			where t.name = 'ns-event'`,
	} {
		got := landed(sql)
		want := map[string]string{"1": "og", "2": "local"}
		if lane != "metric" {
			want = map[string]string{"a": "og", "b": "local"}
		}
		if len(got) != len(want) {
			t.Errorf("%s: landed %v, want %v", lane, got, want)
			continue
		}
		for v, ns := range want {
			if got[v] != ns {
				t.Errorf("%s: sample %q landed in %q, want %q", lane, v, got[v], ns)
			}
		}
	}
}

// A sample naming a namespace that holds no row of its key lands nothing: the
// batch is refused rather than falling back along the search path.
func TestAQualifiedSampleNeverFallsBackAlongTheSearchPath(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "only-local", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	if err := gw.InsertMetricSamples(ctx, storage.PushWriter(), []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "codec-1", Namespace: "og", Key: "only-local", Value: 1},
	}); err == nil {
		t.Fatal("a sample naming og/only-local landed; og holds no such row")
	}
	if err := gw.InsertEvents(ctx, storage.PushWriter(), []storage.EventWrite{
		{OwnerKind: "component", OwnerID: "codec-1", Namespace: "acme", Key: "only-local", Message: "m"},
	}); err == nil {
		t.Fatal("an event naming acme/only-local landed; acme holds no such row")
	}
}

// The writer check reads the row a qualified sample lands on (#1433): a calc
// rule owns the operator's calc-out, so a push of the bare name is refused,
// while a push naming og/calc-out, a collected row no rule outputs, lands.
func TestAQualifiedSampleIsHeldToItsOwnRowsWriter(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	for _, name := range []string{"calc-in", "calc-out"} {
		if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: name, DataType: "float"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := gw.UpsertMetricType(ctx, storage.MetricType{Name: "calc-out", DataType: "float", Official: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateCalcRule(ctx, "", storage.CalcRuleSpec{Name: "calc", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "t", Key: "calc-in"}}, Reduce: rules.CalcReduce{Expr: "avg(t)"},
		OutputKey: "calc-out"}, all); err != nil {
		t.Fatalf("calc rule: %v", err)
	}
	w := storage.PushWriter()
	if err := gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "codec-1", Key: "calc-out", Value: 1},
	}); err == nil {
		t.Error("a push of the bare calc-out landed; the operator's row is a calc rule's")
	}
	if err := gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "codec-1", Namespace: "og", Key: "calc-out", Value: 1},
	}); err != nil {
		t.Errorf("a push of og/calc-out was refused: %v; no rule outputs og's row", err)
	}
}
