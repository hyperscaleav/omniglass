package storage_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// metricsByName indexes a resolved set so a test can assert one metric without
// depending on the result order beyond the resolver's own ordering guarantee.
func metricsByName(ms []storage.EffectiveMetric) map[string]storage.EffectiveMetric {
	m := make(map[string]storage.EffectiveMetric, len(ms))
	for _, e := range ms {
		m[e.MetricTypeName] = e
	}
	return m
}

// TestEffectiveMetrics proves the metric fold, the twin of EffectiveProperties
// with the set-value side reading the metric SERIES (the split bars declared
// numerics from the property store): a component's declared metrics are its
// product contract resolved against the latest observed or calculated sample
// (default until one arrives), plus ad-hoc metrics the series holds that the
// contract does not declare. The resolver is owner-generic: a system resolves
// against its standard's contract and a location against its location type's.
func TestEffectiveMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	// A custom product with a two-metric contract: one with a default, one
	// required with no default.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "acme-dsp", DisplayName: "Acme DSP", Kind: "device"}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := gw.SetProductMetric(ctx, "", "acme-dsp", storage.ProductMetricSpec{
		MetricTypeName: "icmp-rtt-avg", DefaultValue: json.RawMessage(`10.5`), Required: true,
	}); err != nil {
		t.Fatalf("declare icmp-rtt-avg: %v", err)
	}
	if _, err := gw.SetProductMetric(ctx, "", "acme-dsp", storage.ProductMetricSpec{MetricTypeName: "tcp-open"}); err != nil {
		t.Fatalf("declare tcp-open: %v", err)
	}
	product := "acme-dsp"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dsp-1", ProductName: &product}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	// Unsampled: the contract default resolves, nothing is marked sampled.
	got, err := gw.EffectiveMetrics(ctx, "component", "dsp-1", all)
	if err != nil {
		t.Fatalf("effective metrics: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want the 2 contract metrics, got %d: %+v", len(got), got)
	}
	idx := metricsByName(got)
	rtt := idx["icmp-rtt-avg"]
	if string(rtt.Value) != `10.5` || rtt.IsSampled || !rtt.FromContract || !rtt.Required {
		t.Fatalf("icmp-rtt-avg unsampled: want default 10.5 from-contract required, got %+v", rtt)
	}
	if open := idx["tcp-open"]; open.Value != nil || open.IsSampled {
		t.Fatalf("tcp-open unsampled with no default: want no value, got %+v", open)
	}

	// A sample lands: the series wins over the default, and the default stays
	// visible beside it.
	base := time.Now().UTC().Add(-time.Hour)
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "dsp-1", Key: "icmp-rtt-avg", Value: 3.25, Source: "test", TS: base},
	}); err != nil {
		t.Fatalf("insert sample: %v", err)
	}
	idx = metricsByName(mustEffectiveMetrics(t, gw, "component", "dsp-1", all))
	rtt = idx["icmp-rtt-avg"]
	if string(rtt.Value) != `3.25` || !rtt.IsSampled || string(rtt.SampleValue) != `3.25` || string(rtt.DefaultValue) != `10.5` {
		t.Fatalf("icmp-rtt-avg sampled: want 3.25 over default 10.5, got %+v", rtt)
	}

	// The latest sample per metric wins across instances (the LatestMetric rule):
	// a newer per-interface reading replaces the older un-instanced one.
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "dsp-1", Key: "icmp-rtt-avg", Instance: "if-1", Value: 7.5, Source: "test", TS: base.Add(time.Minute)},
	}); err != nil {
		t.Fatalf("insert instanced sample: %v", err)
	}
	idx = metricsByName(mustEffectiveMetrics(t, gw, "component", "dsp-1", all))
	if rtt = idx["icmp-rtt-avg"]; string(rtt.Value) != `7.5` {
		t.Fatalf("icmp-rtt-avg latest across instances: want 7.5, got %+v", rtt)
	}

	// A calculated row is part of reality too: newer than every observation, it
	// becomes the resolved value. (Inserted raw: the gateway's sample sink writes
	// only the observed lane.)
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into metric (ts, owner_kind, component_id, metric_type_id, instance, value, provenance, source, source_rule, source_rule_version)
		values ($1, 'component', (select id from component where name = 'dsp-1'),
		        (select id from metric_type where name = 'icmp-rtt-avg'), '', 9.75, 'calculated', 'rule', 'rule-x', 1)`,
		base.Add(2*time.Minute))
	idx = metricsByName(mustEffectiveMetrics(t, gw, "component", "dsp-1", all))
	if rtt = idx["icmp-rtt-avg"]; string(rtt.Value) != `9.75` || !rtt.IsSampled {
		t.Fatalf("icmp-rtt-avg calculated latest: want 9.75, got %+v", rtt)
	}

	// A metric the contract does not declare still resolves, flagged off-contract.
	if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: "dsp-1", Key: "tcp-connect-time", Value: 42, Source: "test", TS: base},
	}); err != nil {
		t.Fatalf("insert ad-hoc sample: %v", err)
	}
	idx = metricsByName(mustEffectiveMetrics(t, gw, "component", "dsp-1", all))
	if ct := idx["tcp-connect-time"]; !ct.IsSampled || ct.FromContract || ct.Required || string(ct.Value) != `42` {
		t.Fatalf("ad-hoc tcp-connect-time: want sampled off-contract 42, got %+v", ct)
	}

	// Withdrawing a line stops the contract resolving it: with samples the metric
	// survives as ad-hoc, without samples it leaves the read entirely.
	if err := gw.DeleteProductMetric(ctx, "", "acme-dsp", "icmp-rtt-avg"); err != nil {
		t.Fatalf("withdraw icmp-rtt-avg: %v", err)
	}
	if err := gw.DeleteProductMetric(ctx, "", "acme-dsp", "tcp-open"); err != nil {
		t.Fatalf("withdraw tcp-open: %v", err)
	}
	idx = metricsByName(mustEffectiveMetrics(t, gw, "component", "dsp-1", all))
	if rtt = idx["icmp-rtt-avg"]; !rtt.IsSampled || rtt.FromContract || rtt.DefaultValue != nil {
		t.Fatalf("withdrawn icmp-rtt-avg: want ad-hoc with no default, got %+v", rtt)
	}
	if _, still := idx["tcp-open"]; still {
		t.Fatalf("withdrawn tcp-open with no samples still resolves: %+v", idx)
	}

	// --- system: resolves against its standard's contract ---------------------

	if _, err := gw.SetStandardMetric(ctx, "", "huddle-room", storage.StandardMetricSpec{
		MetricTypeName: "tcp-open", DefaultValue: json.RawMessage(`1`),
	}); err != nil {
		t.Fatalf("declare on standard: %v", err)
	}
	std := "huddle-room"
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "hq-huddle", StandardID: &std}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	sys := metricsByName(mustEffectiveMetrics(t, gw, "system", "hq-huddle", all))
	if got := sys["tcp-open"]; string(got.Value) != `1` || got.IsSampled || !got.FromContract {
		t.Fatalf("system inherits the standard default: want 1 unsampled from-contract, got %+v", got)
	}

	// --- location: resolves against its location type's contract --------------

	if _, err := gw.SetLocationTypeMetric(ctx, "", "room", storage.LocationTypeMetricSpec{
		MetricTypeName: "icmp-rtt-avg", DefaultValue: json.RawMessage(`5`), Required: true,
	}); err != nil {
		t.Fatalf("declare on location type: %v", err)
	}
	// A room is not allowed at root (allowed_parent_types), so it hangs off a campus.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-campus", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create campus: %v", err)
	}
	campus := "hq-campus"
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-room-1", LocationType: "room", ParentName: &campus}, all); err != nil {
		t.Fatalf("create location: %v", err)
	}
	loc := metricsByName(mustEffectiveMetrics(t, gw, "location", "hq-room-1", all))
	if got := loc["icmp-rtt-avg"]; string(got.Value) != `5` || !got.FromContract || !got.Required {
		t.Fatalf("location inherits the type default: want 5 from-contract required, got %+v", got)
	}

	// An unknown owner is its kind's non-disclosing not-found.
	if _, err := gw.EffectiveMetrics(ctx, "component", "ghost-x", all); err == nil {
		t.Fatal("unknown component owner: want a not-found error, got nil")
	}
}

func mustEffectiveMetrics(t *testing.T, gw *storage.PG, kind, id string, s scope.Set) []storage.EffectiveMetric {
	t.Helper()
	got, err := gw.EffectiveMetrics(context.Background(), kind, id, s)
	if err != nil {
		t.Fatalf("effective metrics %s/%s: %v", kind, id, err)
	}
	return got
}
