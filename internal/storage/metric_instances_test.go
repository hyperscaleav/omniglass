package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A current-value display shows the newest value across the live instances of
// a key and names the one it shows (ADR-0456 rule 6, #1020): the effective
// metrics read carries the shown sample's instance, how many live instances
// hold a sample, and, when there is more than one, each instance's latest. A
// single-instance key reads as it did, its instance named and no list. A
// retired observer (an endpoint since deleted) is left out of the count and
// the list exactly as it is out of reconciliation, and a calculated sample is
// the owner-level instance.
func TestEffectiveMetricsNameTheShownInstance(t *testing.T) {
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
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dsp-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into endpoint (name, transport, component, node_name, params) values
		('http', 'http', $1::uuid, (select principal_id from node where name = 'edge'), '{"target":"10.0.0.1:80"}'::jsonb),
		('tcp', 'tcp', $1::uuid, (select principal_id from node where name = 'edge'), '{"target":"10.0.0.1:1710"}'::jsonb)`, comp.ID)

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	observe := func(ep string, v float64, at time.Duration) {
		t.Helper()
		if err := gw.InsertMetricSamples(ctx, storage.NodeWriter(comp.ID, ep), []storage.MetricSampleWrite{{
			OwnerKind: "component", OwnerID: comp.ID, Key: "tcp-connect-time", Instance: ep, Value: v, Source: "tcp", TS: base.Add(at),
		}}); err != nil {
			t.Fatalf("observe %v on %s: %v", v, ep, err)
		}
	}
	read := func() storage.EffectiveMetric {
		t.Helper()
		m, ok := metricsByName(mustEffectiveMetrics(t, gw, "component", "dsp-1", all))["tcp-connect-time"]
		if !ok {
			t.Fatal("tcp-connect-time missing from the effective read")
		}
		return m
	}

	// One endpoint: its value, named, a count of one and no per-instance list.
	observe("http", 2.2, time.Second)
	if m := read(); string(m.Value) != `2.2` || m.Instance != "http" || m.InstanceCount != 1 || m.Instances != nil {
		t.Fatalf("one endpoint: want 2.2 from http, count 1, no list; got value=%s instance=%q count=%d list=%+v", m.Value, m.Instance, m.InstanceCount, m.Instances)
	}

	// A second endpoint reports newer: the row shows it, names it, counts two,
	// and lists both newest first.
	observe("tcp", 4.8, 2*time.Second)
	m := read()
	if string(m.Value) != `4.8` || m.Instance != "tcp" || m.InstanceCount != 2 {
		t.Fatalf("two endpoints: want 4.8 from tcp, count 2; got value=%s instance=%q count=%d", m.Value, m.Instance, m.InstanceCount)
	}
	if len(m.Instances) != 2 || m.Instances[0].Instance != "tcp" || string(m.Instances[0].Value) != `4.8` ||
		m.Instances[1].Instance != "http" || string(m.Instances[1].Value) != `2.2` {
		t.Fatalf("two endpoints: want [tcp 4.8, http 2.2], got %+v", m.Instances)
	}
	if !m.Instances[0].TS.Equal(base.Add(2*time.Second)) || !m.Instances[1].TS.Equal(base.Add(time.Second)) {
		t.Fatalf("two endpoints: each instance carries its own sample time, got %+v", m.Instances)
	}

	// The older endpoint reports again: the newest across instances moves to it.
	observe("http", 2.5, 3*time.Second)
	if m := read(); string(m.Value) != `2.5` || m.Instance != "http" || m.InstanceCount != 2 || m.Instances[0].Instance != "http" {
		t.Fatalf("http newer again: want 2.5 from http first of 2; got value=%s instance=%q count=%d list=%+v", m.Value, m.Instance, m.InstanceCount, m.Instances)
	}

	// The http endpoint is deleted: its frozen reading is a retired observer,
	// left out, so the key reads as a single instance again, on tcp.
	if err := gw.DeleteEndpoint(ctx, "", comp.ID+"/http", all, all); err != nil {
		t.Fatalf("delete the http endpoint: %v", err)
	}
	if m := read(); string(m.Value) != `4.8` || m.Instance != "tcp" || m.InstanceCount != 1 || m.Instances != nil {
		t.Fatalf("http retired: want 4.8 from tcp, count 1, no list; got value=%s instance=%q count=%d list=%+v", m.Value, m.Instance, m.InstanceCount, m.Instances)
	}

	// A calculated sample on the same key is the owner-level instance: newest,
	// it is shown, named by the empty instance, and counted beside tcp.
	mustExec(t, conn, `insert into metric (ts, owner_kind, component_id, metric_type_id, instance, value, provenance, source, source_rule, source_rule_version)
		values ($1, 'component', $2::uuid, (select id from metric_type where name = 'tcp-connect-time'), '', 9.75, 'calculated', 'rule', 'rule-x', 1)`,
		base.Add(4*time.Second), comp.ID)
	if m := read(); string(m.Value) != `9.75` || m.Instance != "" || m.InstanceCount != 2 || len(m.Instances) != 2 || m.Instances[0].Instance != "" {
		t.Fatalf("calculated owner-level newest: want 9.75 at the empty instance, count 2; got value=%s instance=%q count=%d list=%+v", m.Value, m.Instance, m.InstanceCount, m.Instances)
	}

	// Every endpoint retired and nothing calculated: a key whose only
	// observers are retired has no live sample, so an off-contract metric
	// leaves the read rather than showing a frozen reading.
	observeOther := func(ep string, v float64) {
		t.Helper()
		if err := gw.InsertMetricSamples(ctx, storage.NodeWriter(comp.ID, ep), []storage.MetricSampleWrite{{
			OwnerKind: "component", OwnerID: comp.ID, Key: "icmp-rtt-avg", Instance: ep, Value: v, Source: "icmp", TS: base,
		}}); err != nil {
			t.Fatalf("observe icmp-rtt-avg on %s: %v", ep, err)
		}
	}
	observeOther("tcp", 6.1)
	if _, ok := metricsByName(mustEffectiveMetrics(t, gw, "component", "dsp-1", all))["icmp-rtt-avg"]; !ok {
		t.Fatal("icmp-rtt-avg on a live endpoint: want it in the read")
	}
	if err := gw.DeleteEndpoint(ctx, "", comp.ID+"/tcp", all, all); err != nil {
		t.Fatalf("delete the tcp endpoint: %v", err)
	}
	if got, ok := metricsByName(mustEffectiveMetrics(t, gw, "component", "dsp-1", all))["icmp-rtt-avg"]; ok {
		t.Fatalf("icmp-rtt-avg on a retired endpoint only: want it out of the read, got %+v", got)
	}

	// A system has no endpoints: its pushed metric is owner-level and reads as
	// it did, a single instance named by the empty string.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "hq-huddle"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if err := insertMetrics(ctx, gw, []storage.MetricSampleWrite{
		{OwnerKind: "system", OwnerID: "hq-huddle", Key: "occupancy-count", Instance: "", Value: 3, Source: "test", TS: base},
	}); err != nil {
		t.Fatalf("push a system metric: %v", err)
	}
	sys, ok := metricsByName(mustEffectiveMetrics(t, gw, "system", "hq-huddle", all))["occupancy-count"]
	if !ok || string(sys.Value) != `3` || sys.Instance != "" || sys.InstanceCount != 1 || sys.Instances != nil {
		t.Fatalf("system pushed metric: want 3 at the empty instance, count 1, no list; got %+v (present %v)", sys, ok)
	}
}
