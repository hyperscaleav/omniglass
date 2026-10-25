package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	"github.com/jackc/pgx/v5"
)

// writerFixture is a seeded gateway with one component (disp-1) carrying one
// ssh endpoint (ssh) on node-a, and a raw connection for planting the legacy
// rows a write path no longer produces.
type writerFixture struct {
	ctx   context.Context
	gw    *storage.PG
	conn  *pgx.Conn
	actor string
}

func newWriterFixture(t *testing.T) writerFixture {
	t.Helper()
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
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('ssh', 'ssh', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'node-a'), '{"target":"127.0.0.1:22"}'::jsonb),
		('tls', 'tls', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'node-a'), '{"target":"127.0.0.1:443","tls-mode":"pin"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	return writerFixture{ctx: ctx, gw: gw, conn: conn, actor: actor}
}

func (f writerFixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.conn.QueryRow(f.ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// refused reports a write the gateway turned away for its writer.
func refused(err error) bool { return errors.Is(err, storage.ErrKeyWriterRefused) }

// TestCommandTypeRefusesANodeProbeTarget is point 1 of ADR-0161: a command
// opens an intended value on its target, so a command type may target only a
// key a command may write. A node-probe key (the reachability verdict, a held
// session's state, the trusted host key, a probe metric) and the platform's
// health are refused at create and at retarget, naming the key, and a command
// type stored before the refusal existed opens nothing when issued.
func TestCommandTypeRefusesANodeProbeTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	for _, target := range []storage.CommandTypeSpec{
		{TargetPropertyType: "endpoint-reachable"},
		{TargetPropertyType: "session-state"},
		{TargetPropertyType: "ssh-host-key"},
		{TargetPropertyType: "endpoint-rejection"},
		{TargetPropertyType: "health"},
		{TargetMetricType: "tcp-open"},
		{TargetMetricType: "icmp-rtt-avg"},
	} {
		key := target.TargetPropertyType + target.TargetMetricType
		target.Name = "forge-" + key
		_, err := f.gw.CreateCommandType(f.ctx, "", target)
		if !errors.Is(err, storage.ErrCommandTypeInvalid) || !refused(err) || !strings.Contains(err.Error(), key) {
			t.Errorf("a command type targeting %s = %v, want it refused naming the key", key, err)
		}
	}

	// A legal command type cannot be retargeted onto one either.
	if _, err := f.gw.CreateCommandType(f.ctx, "", storage.CommandTypeSpec{Name: "set-input-2", TargetPropertyType: "video-input"}); err != nil {
		t.Fatalf("create a legal command type: %v", err)
	}
	reach := "endpoint-reachable"
	if _, err := f.gw.UpdateCommandType(f.ctx, "", "set-input-2", storage.CommandTypePatch{TargetPropertyType: &reach}); !refused(err) {
		t.Error("a command type retargeted onto endpoint-reachable; want it refused")
	}

	// A command type stored before the refusal (planted raw) opens nothing.
	if _, err := f.conn.Exec(f.ctx, `insert into command_type (name, description, settle_window_seconds, target_property_type_id, official)
		values ('legacy-forge', '', 30, (select id from property_type where name = 'endpoint-reachable'), false)`); err != nil {
		t.Fatalf("plant a legacy command type: %v", err)
	}
	comp, err := f.gw.GetComponent(f.ctx, "disp-1", scope.Set{All: true})
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	_, err = f.gw.IssueCommand(f.ctx, f.actor, "component", comp.ID, "legacy-forge", "ssh", json.RawMessage(`"up"`), nil)
	if !refused(err) {
		t.Error("issuing a legacy command type targeting endpoint-reachable succeeded; want it refused")
	}
	if n := f.count(t, `select count(*) from property where property_type_id = (select id from property_type where name = 'endpoint-reachable')`); n != 0 {
		t.Errorf("the refused issue wrote %d endpoint-reachable rows, want none", n)
	}
}

// TestDeclaredWriteRefusesAReservedKey: an operator's declared value is never
// a node's observation or the platform's verdict, so the declared write refuses
// every node-probe key (the host key pin included: it lands only through the
// endpoint's audited :retry) and health, naming the key, and writes nothing.
func TestDeclaredWriteRefusesAReservedKey(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	for key, value := range map[string]string{
		"endpoint-reachable": `"up"`,
		"session-state":      `"up"`,
		"endpoint-rejection": `"none"`,
		"ssh-host-key":       `"SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`,
		"tls-certificate":    `"SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`,
		"health":             `"healthy"`,
	} {
		_, err := f.gw.SetProperty(f.ctx, "", "component", "disp-1", key, "ssh", json.RawMessage(value), all, all)
		if !refused(err) {
			t.Errorf("a declared %s was written; want it refused", key)
		}
		if n := f.count(t, `select count(*) from property where property_type_id = (select id from property_type where name = $1)`, key); n != 0 {
			t.Errorf("the refused declared %s wrote %d rows", key, n)
		}
	}
	// The pin's own path still writes the declared host key.
	var epID string
	if err := f.conn.QueryRow(f.ctx, `select id::text from endpoint where name = 'ssh'`).Scan(&epID); err != nil {
		t.Fatalf("endpoint id: %v", err)
	}
	if _, err := f.gw.RetryEndpoint(f.ctx, "", epID, storage.RetryPin{HostKey: "SHA256:" + strings.Repeat("b", 43)}, all, all); err != nil {
		t.Fatalf("retry with a host key pin: %v", err)
	}
	if n := f.count(t, `select count(*) from property where provenance = 'declared'
		and property_type_id = (select id from property_type where name = 'ssh-host-key')`); n != 1 {
		t.Fatalf("the :retry pin wrote %d declared host keys, want 1", n)
	}
}

// TestNodeProbeReadsServeOnlyObserved is point 3: the reads the reachability
// panel and the session pill compose over serve a node-probe key's observed
// series only. A declared or calculated row planted beside it (newer than the
// node's own observation, as a write before the gateway's refusal could leave
// one) never reads as the node's.
func TestNodeProbeReadsServeOnlyObserved(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	observedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	if _, err := f.conn.Exec(f.ctx, `
		insert into property (ts, owner_kind, component_id, property_type_id, instance, value, provenance, source)
		select $1, 'component', c.id, pt.id, 'ssh', to_jsonb(v.value), 'observed', 'ssh'
		from component c, property_type pt, (values ('endpoint-reachable', 'down'), ('session-state', 'rejected')) v(key, value)
		where c.name = 'disp-1' and pt.name = v.key`, observedAt); err != nil {
		t.Fatalf("observed rows: %v", err)
	}
	if _, err := f.conn.Exec(f.ctx, `
		insert into metric (ts, owner_kind, component_id, metric_type_id, instance, value, provenance, source)
		select $1, 'component', c.id, mt.id, 'ssh', 0, 'observed', 'tcp'
		from component c, metric_type mt where c.name = 'disp-1' and mt.name = 'tcp-open'`, observedAt); err != nil {
		t.Fatalf("observed metric: %v", err)
	}
	// The planted rows, an hour newer than the node's.
	if _, err := f.conn.Exec(f.ctx, `
		insert into property (ts, owner_kind, component_id, property_type_id, instance, value, provenance)
		select now(), 'component', c.id, pt.id, 'ssh', to_jsonb(v.value), 'declared'
		from component c, property_type pt, (values ('endpoint-reachable', 'up'), ('session-state', 'up')) v(key, value)
		where c.name = 'disp-1' and pt.name = v.key`); err != nil {
		t.Fatalf("declared rows: %v", err)
	}
	if _, err := f.conn.Exec(f.ctx, `
		insert into metric (ts, owner_kind, component_id, metric_type_id, instance, value, provenance, source, source_rule)
		select now(), 'component', c.id, mt.id, 'ssh', 1, 'calculated', 'calc', 'forged'
		from component c, metric_type mt where c.name = 'disp-1' and mt.name = 'tcp-open'`); err != nil {
		t.Fatalf("calculated metric: %v", err)
	}

	for key, want := range map[string]string{"endpoint-reachable": "down", "session-state": "rejected"} {
		got, err := f.gw.LatestProperty(f.ctx, "disp-1", key, "ssh")
		if err != nil || got == nil || got.Value != want || got.Provenance != "observed" {
			t.Fatalf("LatestProperty(%s) = %+v (%v), want the node's observed %q", key, got, err, want)
		}
	}
	rows, err := f.gw.PropertyTransitions(f.ctx, "disp-1", "endpoint-reachable", "ssh", 0)
	if err != nil {
		t.Fatalf("transitions: %v", err)
	}
	if len(rows) != 1 || rows[0].Value != "down" || rows[0].Provenance != "observed" {
		t.Fatalf("PropertyTransitions = %+v, want only the observed down", rows)
	}
	m, err := f.gw.LatestMetricInstance(f.ctx, "disp-1", "tcp-open", "ssh")
	if err != nil || m == nil || m.Value != 0 || m.Provenance != "observed" {
		t.Fatalf("LatestMetricInstance(tcp-open) = %+v (%v), want the observed 0", m, err)
	}
	m, err = f.gw.LatestMetric(f.ctx, "disp-1", "tcp-open")
	if err != nil || m == nil || m.Value != 0 || m.Provenance != "observed" {
		t.Fatalf("LatestMetric(tcp-open) = %+v (%v), want the observed 0", m, err)
	}
}

// TestWriterMismatchRefused is the write-side invariant's table (ADR-0161):
// every registered write path (storage.SampleWritePaths, which the source guard
// holds complete) is driven with a writer its key's class does not admit, and
// each is refused with ErrKeyWriterRefused, writing nothing. A path with no
// case here fails. Each path's admitted writer is driven too, so a refusal is
// never the vacuous kind.
func TestWriterMismatchRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	comp, err := f.gw.GetComponent(f.ctx, "disp-1", all)
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	var epID, tlsID string
	if err := f.conn.QueryRow(f.ctx, `select id::text from endpoint where name = 'ssh'`).Scan(&epID); err != nil {
		t.Fatalf("endpoint id: %v", err)
	}
	if err := f.conn.QueryRow(f.ctx, `select id::text from endpoint where name = 'tls'`).Scan(&tlsID); err != nil {
		t.Fatalf("tls endpoint id: %v", err)
	}
	// A calc rule owns avg-uptime.
	if _, err := f.gw.CreateMetricType(f.ctx, "", storage.MetricTypeSpec{Name: "avg-uptime", DataType: "float"}); err != nil {
		t.Fatalf("create the calc output type: %v", err)
	}
	rule, err := f.gw.CreateCalcRule(f.ctx, "", storage.CalcRuleSpec{Name: "uptime-avg", OwnerKind: "component",
		Inputs: []rules.CalcInput{{Alias: "u", Key: "uptime"}}, Reduce: rules.CalcReduce{Expr: "avg(u)"}, OutputKey: "avg-uptime"}, all)
	if err != nil {
		t.Fatalf("create the calc rule: %v", err)
	}
	if _, err := f.conn.Exec(f.ctx, `insert into command_type (name, description, settle_window_seconds, target_property_type_id, official)
		values ('legacy-forge', '', 30, (select id from property_type where name = 'endpoint-reachable'), false)`); err != nil {
		t.Fatalf("plant a legacy command type: %v", err)
	}
	var ruleID string
	if err := f.conn.QueryRow(f.ctx, `select id::text from event_rule where name = 'reachability-lost'`).Scan(&ruleID); err != nil {
		t.Fatalf("the seeded event rule: %v", err)
	}
	metric := func(key, instance string) []storage.MetricSampleWrite {
		return []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: comp.ID, Key: key, Instance: instance, Value: 1}}
	}
	property := func(key, instance, value string) []storage.PropertySampleWrite {
		return []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: comp.ID, Key: key, Instance: instance, Value: value}}
	}
	event := func(key, instance string) []storage.EventWrite {
		return []storage.EventWrite{{OwnerKind: "component", OwnerID: comp.ID, Key: key, Instance: instance, Message: "forged"}}
	}
	type writeCase struct {
		name  string
		write func() error
	}
	refusals := map[string][]writeCase{
		"writeMetricSamples": {
			{"a push of a probe metric", func() error { return f.gw.InsertMetricSamples(f.ctx, storage.PushWriter(), metric("tcp-open", "ssh")) }},
			{"a node on another endpoint", func() error {
				return f.gw.InsertMetricSamples(f.ctx, storage.NodeWriter(comp.ID, "ssh"), metric("tcp-open", "panel"))
			}},
			{"a node bound to no endpoint", func() error {
				return f.gw.InsertMetricSamples(f.ctx, storage.NodeWriter(comp.ID, ""), metric("icmp-rtt-avg", ""))
			}},
			{"a push of a calc output", func() error { return f.gw.InsertMetricSamples(f.ctx, storage.PushWriter(), metric("avg-uptime", "")) }},
			{"a node lane carrying a calc output", func() error {
				return f.gw.InsertMetricSamples(f.ctx, storage.NodeWriter(comp.ID, "ssh"), metric("avg-uptime", "ssh"))
			}},
			{"another rule's output", func() error { return f.gw.WriteMetricAsRule(f.ctx, uuidZero, metric("avg-uptime", "")) }},
			{"a rule writing a collected key", func() error { return f.gw.WriteMetricAsRule(f.ctx, rule.ID, metric("uptime", "")) }},
		},
		"writePropertySamples": {
			{"a push of the verdict", func() error {
				return f.gw.InsertPropertySamples(f.ctx, storage.PushWriter(), property("endpoint-reachable", "ssh", "up"))
			}},
			{"a push of health", func() error {
				return f.gw.InsertPropertySamples(f.ctx, storage.PushWriter(), property("health", "", "healthy"))
			}},
			{"a node on another endpoint", func() error {
				return f.gw.InsertPropertySamples(f.ctx, storage.NodeWriter(comp.ID, "ssh"), property("session-state", "panel", "up"))
			}},
			{"a node writing health", func() error {
				return f.gw.InsertPropertySamples(f.ctx, storage.NodeWriter(comp.ID, "ssh"), property("health", "ssh", "healthy"))
			}},
			{"a rule writing the verdict", func() error {
				return f.gw.WritePropertyAsRule(f.ctx, rule.ID, property("endpoint-reachable", "ssh", "up"))
			}},
		},
		"InsertEvents": {
			{"a push of collection-failed", func() error { return f.gw.InsertEvents(f.ctx, storage.PushWriter(), event("collection-failed", "ssh")) }},
			{"a node on another endpoint", func() error {
				return f.gw.InsertEvents(f.ctx, storage.NodeWriter(comp.ID, "ssh"), event("collection-failed", "panel"))
			}},
		},
		"recordHealth": {
			{"the process writing a collected key", func() error { return f.gw.AdmitWrite(f.ctx, "property", writerclass.Process, "video-input", "") }},
			{"the process writing a probe key", func() error {
				return f.gw.AdmitWrite(f.ctx, "property", writerclass.Process, "endpoint-reachable", "ssh")
			}},
		},
		"IssueCommand": {
			{"a command opening the verdict", func() error {
				_, err := f.gw.IssueCommand(f.ctx, f.actor, "component", comp.ID, "legacy-forge", "ssh", json.RawMessage(`"up"`), nil)
				return err
			}},
		},
		"writeLifecycleEvent": {
			{"an event rule settling a command", func() error { return f.gw.AdmitWrite(f.ctx, "event", writerclass.EventRule, "command-settled", "") }},
			{"a push failing a command", func() error { return f.gw.AdmitWrite(f.ctx, "event", writerclass.Push, "command-failed", "") }},
			{"a node timing a command out", func() error {
				return f.gw.AdmitWrite(f.ctx, "event", writerclass.Node, "command-timed-out", "ssh")
			}},
			{"a command's outcome on a probe key", func() error {
				return f.gw.AdmitWrite(f.ctx, "event", writerclass.Command, "collection-failed", "ssh")
			}},
		},
		"writeReplyObservation": {
			{"a reply observing the verdict", func() error { return f.gw.WriteReplyObservation(f.ctx, comp.ID, "endpoint-reachable", "ssh", "up") }},
			{"a reply observing a calc output", func() error { return f.gw.WriteReplyObservation(f.ctx, comp.ID, "health", "", "healthy") }},
		},
		"SetProperty": {
			{"a declared gate", func() error {
				_, err := f.gw.SetProperty(f.ctx, f.actor, "component", comp.ID, "endpoint-rejection", "ssh", json.RawMessage(`"none"`), all, all)
				return err
			}},
			{"a tls pin outside :retry", func() error {
				_, err := f.gw.SetProperty(f.ctx, f.actor, "component", comp.ID, "tls-certificate", "tls",
					json.RawMessage(`"SHA256:`+strings.Repeat("d", 43)+`"`), all, all)
				return err
			}},
		},
		"ClearProperty": {
			{"a declared tombstone on the verdict", func() error {
				return f.gw.ClearProperty(f.ctx, f.actor, "component", comp.ID, "endpoint-reachable", "ssh", all, all)
			}},
		},
		"RetryEndpoint": {
			{"the pin on the gate", func() error {
				return f.gw.AdmitWrite(f.ctx, "property", writerclass.KeyPin, "endpoint-rejection", "ssh")
			}},
			{"the pin on a collected key", func() error { return f.gw.AdmitWrite(f.ctx, "property", writerclass.KeyPin, "video-input", "ssh") }},
			{"the pin on a tls certificate fact", func() error {
				return f.gw.AdmitWrite(f.ctx, "property", writerclass.KeyPin, "tls-certificate-expiry", "tls")
			}},
			{"the pin on a tls timing", func() error { return f.gw.AdmitWrite(f.ctx, "metric", writerclass.KeyPin, "tls-handshake-time", "tls") }},
		},
		"writeRuleEvent": {
			{"a rule firing collection-failed", func() error { return f.gw.WriteRuleEvent(f.ctx, ruleID, "collection-failed", comp.ID, "ssh") }},
		},
	}
	for path := range storage.SampleWritePaths {
		if len(refusals[path]) == 0 {
			t.Errorf("write path %s has no mismatched-writer case", path)
		}
	}
	for path, cases := range refusals {
		if _, ok := storage.SampleWritePaths[path]; !ok {
			t.Errorf("a case names %s, which is no registered write path", path)
		}
		for _, c := range cases {
			if err := c.write(); !refused(err) {
				t.Errorf("%s: %s = %v, want ErrKeyWriterRefused", path, c.name, err)
			}
		}
	}
	// Nothing a refusal touched landed.
	if n := f.count(t, `select (select count(*) from metric) + (select count(*) from property) + (select count(*) from event)`); n != 0 {
		t.Errorf("%d sample or event rows landed from refused writes", n)
	}

	// The admitted writers, through the same paths.
	for name, write := range map[string]func() error{
		"a node on its endpoint": func() error {
			return f.gw.InsertMetricSamples(f.ctx, storage.NodeWriter(comp.ID, "ssh"), metric("tcp-open", "ssh"))
		},
		"a push of a collected": func() error { return f.gw.InsertMetricSamples(f.ctx, storage.PushWriter(), metric("uptime", "")) },
		"the rule's own output": func() error { return f.gw.WriteMetricAsRule(f.ctx, rule.ID, metric("avg-uptime", "")) },
		"a node's own failure": func() error {
			return f.gw.InsertEvents(f.ctx, storage.NodeWriter(comp.ID, "ssh"), event("collection-failed", "ssh"))
		},
		"a declared collected": func() error {
			_, err := f.gw.SetProperty(f.ctx, f.actor, "component", comp.ID, "serial-number", "", json.RawMessage(`"SN-1"`), all, all)
			return err
		},
		"a reply on a collected":  func() error { return f.gw.WriteReplyObservation(f.ctx, comp.ID, "video-input", "ssh", "hdmi1") },
		"a rule firing its event": func() error { return f.gw.WriteRuleEvent(f.ctx, ruleID, "condition-started", comp.ID, "ssh") },
		"the process on health":   func() error { return f.gw.AdmitWrite(f.ctx, "property", writerclass.Process, "health", "") },
		"a command's own outcome": func() error { return f.gw.AdmitWrite(f.ctx, "event", writerclass.Command, "command-timed-out", "") },
		"the pin on the host key": func() error {
			_, err := f.gw.RetryEndpoint(f.ctx, f.actor, epID, storage.RetryPin{HostKey: "SHA256:" + strings.Repeat("c", 43)}, all, all)
			return err
		},
		"the pin on a tls certificate": func() error {
			_, err := f.gw.RetryEndpoint(f.ctx, f.actor, tlsID, storage.RetryPin{Certificate: "SHA256:" + strings.Repeat("e", 43)}, all, all)
			return err
		},
		"a node's own tls facts": func() error {
			return f.gw.InsertMetricSamples(f.ctx, storage.NodeWriter(comp.ID, "tls"), []storage.MetricSampleWrite{
				{OwnerKind: "component", OwnerID: comp.ID, Key: "tls-certificate-days-left", Instance: "tls", Value: 30}})
		},
	} {
		if err := write(); err != nil {
			t.Errorf("%s: %v, want it admitted", name, err)
		}
	}
}

// uuidZero is a calc rule id no rule holds.
const uuidZero = "00000000-0000-0000-0000-000000000000"

// TestEveryCatalogKeyHasOneWriterClass: every seeded metric, property and
// event type reads back exactly one writer class; every reserved name is
// seeded and reads its reserved class; a calc rule's output reads calc; and an
// operator cannot create a type under a reserved name on any lane.
func TestEveryCatalogKeyHasOneWriterClass(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	classes := map[string]writerclass.Class{}
	check := func(name string, c writerclass.Class) {
		t.Helper()
		if !slices.Contains(writerclass.Classes(), c) {
			t.Errorf("%s reads writer class %q, not one of %v", name, c, writerclass.Classes())
		}
		if prev, dup := classes[name]; dup {
			t.Errorf("%s is catalogued twice (%s, %s)", name, prev, c)
		}
		classes[name] = c
	}
	mts, err := f.gw.ListMetricTypes(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, mt := range mts {
		check(mt.Name, mt.WriterClass)
	}
	pts, err := f.gw.ListPropertyTypes(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range pts {
		check(pt.Name, pt.WriterClass)
	}
	ets, err := f.gw.ListEventTypes(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, et := range ets {
		check(et.Name, et.WriterClass)
	}
	for _, name := range writerclass.ReservedNames() {
		want, _ := writerclass.Reserved(name)
		if got, ok := classes[name]; !ok || got != want {
			t.Errorf("reserved %s reads %q (seeded %v), want %s", name, got, ok, want)
		}
	}
	for name, c := range classes {
		if _, reserved := writerclass.Reserved(name); !reserved && c != writerclass.Collected {
			t.Errorf("seeded %s reads %s before any calc rule exists, want collected", name, c)
		}
	}

	all := scope.Set{All: true}
	if _, err := f.gw.CreateMetricType(f.ctx, "", storage.MetricTypeSpec{Name: "avg-uptime", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateCalcRule(f.ctx, "", storage.CalcRuleSpec{Name: "uptime-avg", OwnerKind: "component",
		Inputs: []rules.CalcInput{{Alias: "u", Key: "uptime"}}, Reduce: rules.CalcReduce{Expr: "avg(u)"}, OutputKey: "avg-uptime"}, all); err != nil {
		t.Fatal(err)
	}
	if mt, err := f.gw.GetMetricType(f.ctx, "avg-uptime"); err != nil || mt.WriterClass != writerclass.Calc {
		t.Errorf("a calc rule's output reads %+v (%v), want calc", mt, err)
	}

	// No lane takes a reserved name for an operator's type.
	for _, name := range writerclass.ReservedNames() {
		if _, err := f.gw.CreateMetricType(f.ctx, "", storage.MetricTypeSpec{Name: name, DataType: "float"}); err == nil {
			t.Errorf("an operator created metric type %s", name)
		}
		if _, err := f.gw.CreatePropertyType(f.ctx, "", storage.PropertyTypeSpec{Name: name, DataType: "string"}); err == nil {
			t.Errorf("an operator created property type %s", name)
		}
		if _, err := f.gw.CreateEventType(f.ctx, "", storage.EventTypeSpec{Name: name}); err == nil {
			t.Errorf("an operator created event type %s", name)
		}
	}
	// Even before the seed: a fresh registry refuses the names by rule, not by
	// collision with a seeded row.
	if _, err := f.conn.Exec(f.ctx, `delete from metric_type where name = 'tcp-connect-time'`); err != nil {
		t.Fatalf("drop a seeded reserved type: %v", err)
	}
	if _, err := f.gw.CreateMetricType(f.ctx, "", storage.MetricTypeSpec{Name: "tcp-connect-time", DataType: "float"}); !errors.Is(err, storage.ErrMetricTypeInvalid) || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("creating tcp-connect-time with no seeded row = %v, want it refused as reserved", err)
	}
}

// TestDeclarationsRefuseAKeyTheirWriterDoesNot is the declaration side of
// ADR-0161, point 2 at the gateway: a driver spec emitting a reserved key or a
// calc rule's output is refused at its write, as is an event rule firing the
// node's collection-failed and a calc rule taking a reserved key as its
// output; the same declarations naming a collected key are admitted.
func TestDeclarationsRefuseAKeyTheirWriterDoesNot(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateMetricType(f.ctx, "", storage.MetricTypeSpec{Name: "avg-uptime", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateCalcRule(f.ctx, "", storage.CalcRuleSpec{Name: "uptime-avg", OwnerKind: "component",
		Inputs: []rules.CalcInput{{Alias: "u", Key: "uptime"}}, Reduce: rules.CalcReduce{Expr: "avg(u)"}, OutputKey: "avg-uptime"}, all); err != nil {
		t.Fatal(err)
	}
	spec := func(emit string) []byte {
		return []byte(`{"version":1,"transport":"tcp","polls":[{"name":"status","schedule":{"every":"30s"},` +
			`"request":{"line":"GET STATUS"},"emits":[{"name":"` + emit + `","extract":{"regex":"^S (\\S+)$"}}]}]}`)
	}
	for _, key := range []string{"endpoint-reachable", "session-state", "endpoint-rejection", "tcp-open", "health", "avg-uptime"} {
		_, err := f.gw.CreateDriver(f.ctx, "", storage.Driver{Name: "rogue-" + key, Label: "Rogue", Spec: spec(key)})
		if !errors.Is(err, storage.ErrSpecInvalid) || !strings.Contains(err.Error(), key) {
			t.Errorf("a driver emitting %s = %v, want ErrSpecInvalid naming it", key, err)
		}
	}
	if _, err := f.gw.CreateDriver(f.ctx, "", storage.Driver{Name: "honest", Label: "Honest", Spec: spec("video-input")}); err != nil {
		t.Errorf("a driver emitting video-input: %v", err)
	}

	for _, fire := range []string{"collection-failed"} {
		_, err := f.gw.CreateEventRule(f.ctx, "", storage.EventRuleSpec{Name: "forge-" + fire, SampleKey: "video-input",
			Fire: rules.Criteria{Expr: `value == "hdmi1"`}, FireEventType: fire}, all)
		if !errors.Is(err, storage.ErrEventRuleInvalid) || !strings.Contains(err.Error(), fire) {
			t.Errorf("an event rule firing %s = %v, want it refused naming the key", fire, err)
		}
		_, err = f.gw.CreateEventRule(f.ctx, "", storage.EventRuleSpec{Name: "forge-clear", SampleKey: "video-input",
			Fire: rules.Criteria{Expr: `value == "hdmi1"`}, FireEventType: "condition-started", ClearEventType: fire}, all)
		if !errors.Is(err, storage.ErrEventRuleInvalid) || !strings.Contains(err.Error(), fire) {
			t.Errorf("an event rule clearing with %s = %v, want it refused naming the key", fire, err)
		}
	}

	for _, out := range []string{"endpoint-reachable", "health", "icmp-rtt-avg"} {
		_, err := f.gw.CreateCalcRule(f.ctx, "", storage.CalcRuleSpec{Name: "forge-calc", OwnerKind: "component",
			Inputs: []rules.CalcInput{{Alias: "u", Key: "uptime"}}, Reduce: rules.CalcReduce{Expr: "avg(u)"}, OutputKey: out}, all)
		if !errors.Is(err, storage.ErrCalcRuleInvalid) || !strings.Contains(err.Error(), out) {
			t.Errorf("a calc rule writing %s = %v, want it refused naming the key", out, err)
		}
	}
}

// TestCalcOutputAndCommandTargetExclude: a key is a calc rule's or a
// command's, never both, whichever is declared first (ADR-0161).
func TestCalcOutputAndCommandTargetExclude(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	for _, name := range []string{"commanded", "calculated"} {
		if _, err := f.gw.CreateMetricType(f.ctx, "", storage.MetricTypeSpec{Name: name, DataType: "float"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.gw.CreateCommandType(f.ctx, "", storage.CommandTypeSpec{Name: "set-commanded", TargetMetricType: "commanded"}); err != nil {
		t.Fatalf("a command type on a collected key: %v", err)
	}
	_, err := f.gw.CreateCalcRule(f.ctx, "", storage.CalcRuleSpec{Name: "takes-commanded", OwnerKind: "component",
		Inputs: []rules.CalcInput{{Alias: "u", Key: "uptime"}}, Reduce: rules.CalcReduce{Expr: "avg(u)"}, OutputKey: "commanded"}, all)
	if !errors.Is(err, storage.ErrCalcRuleInvalid) || !strings.Contains(err.Error(), "set-commanded") {
		t.Errorf("a calc rule over a command's target = %v, want it refused naming the command type", err)
	}
	if _, err := f.gw.CreateCalcRule(f.ctx, "", storage.CalcRuleSpec{Name: "owns-calculated", OwnerKind: "component",
		Inputs: []rules.CalcInput{{Alias: "u", Key: "uptime"}}, Reduce: rules.CalcReduce{Expr: "avg(u)"}, OutputKey: "calculated"}, all); err != nil {
		t.Fatalf("a calc rule on a free key: %v", err)
	}
	_, err = f.gw.CreateCommandType(f.ctx, "", storage.CommandTypeSpec{Name: "set-calculated", TargetMetricType: "calculated"})
	if !errors.Is(err, storage.ErrCommandTypeInvalid) || !refused(err) || !strings.Contains(err.Error(), "owns-calculated") {
		t.Errorf("a command type over a calc output = %v, want it refused naming the rule", err)
	}
}

// TestNodeProbeSampleCannotOutliveItsTime: a node-probe sample dated ahead of
// the database's clock by more than the skew is stored at the database's now,
// so a node cannot keep its account of an endpoint current past its tenure
// (ADR-0161). A collected key keeps the time its producer sent.
func TestNodeProbeSampleCannotOutliveItsTime(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	comp, err := f.gw.GetComponent(f.ctx, "disp-1", scope.Set{All: true})
	if err != nil {
		t.Fatal(err)
	}
	far := time.Now().UTC().AddDate(70, 0, 0)
	node := storage.NodeWriter(comp.ID, "ssh")
	if err := f.gw.InsertPropertySamples(f.ctx, node, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: comp.ID, Key: "endpoint-reachable", Instance: "ssh", Value: "up", TS: far},
		{OwnerKind: "component", OwnerID: comp.ID, Key: "endpoint-rejection", Instance: "ssh", Value: "authenticated auth 9f86d081", TS: far},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.InsertMetricSamples(f.ctx, node, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: comp.ID, Key: "tcp-open", Instance: "ssh", Value: 1, TS: far}}); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.InsertEvents(f.ctx, node, []storage.EventWrite{
		{OwnerKind: "component", OwnerID: comp.ID, Key: "collection-failed", Instance: "ssh", Message: "x", TS: far}}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `select (select count(*) from property where ts > now() + interval '1 hour')
		+ (select count(*) from metric where ts > now() + interval '1 hour')
		+ (select count(*) from event where ts > now() + interval '1 hour')`); n != 0 {
		t.Fatalf("%d node-probe rows kept a time seventy years ahead", n)
	}
	// The next reading, a second from now, is the current one.
	if err := f.gw.InsertPropertySamples(f.ctx, node, []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: comp.ID, Key: "endpoint-reachable", Instance: "ssh", Value: "down", TS: time.Now().UTC().Add(time.Second)}}); err != nil {
		t.Fatal(err)
	}
	if got, err := f.gw.LatestProperty(f.ctx, "disp-1", "endpoint-reachable", "ssh"); err != nil || got == nil || got.Value != "down" {
		t.Fatalf("LatestProperty = %+v (%v), want the later down", got, err)
	}
	// A small skew is the node's clock, kept.
	near := time.Now().UTC().Add(30 * time.Second).Truncate(time.Millisecond)
	if err := f.gw.InsertMetricSamples(f.ctx, node, []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: comp.ID, Key: "tcp-connect-time", Instance: "ssh", Value: 2, TS: near}}); err != nil {
		t.Fatal(err)
	}
	if m, err := f.gw.LatestMetricInstance(f.ctx, "disp-1", "tcp-connect-time", "ssh"); err != nil || m == nil || !m.TS.Equal(near) {
		t.Fatalf("a sample 30s ahead = %+v (%v), want its own time kept", m, err)
	}
	// A collected key keeps its producer's time, however far.
	if err := f.gw.InsertMetricSamples(f.ctx, storage.PushWriter(), []storage.MetricSampleWrite{
		{OwnerKind: "component", OwnerID: comp.ID, Key: "uptime", Value: 3, TS: far}}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `select count(*) from metric where ts > now() + interval '1 hour'
		and metric_type_id = (select id from metric_type where name = 'uptime')`); n != 1 {
		t.Fatalf("a pushed uptime's own time: %d far rows, want 1", n)
	}
}

// TestHealthReadsServeOnlyTheRollup: a health row the rollup did not write (a
// declared one, written before the writer check refused it) never reads as a
// system's verdict; the reads serve the rollup's calculated rows alone.
func TestHealthReadsServeOnlyTheRollup(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	sys, err := f.gw.CreateSystem(f.ctx, "", storage.SystemSpec{Name: "room-1"}, all, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(f.ctx, `insert into property (ts, owner_kind, system_id, property_type_id, instance, value, provenance, source_rule)
		values (clock_timestamp(), 'system', $1::uuid, (select id from property_type where name = 'health'), '', to_jsonb('outage'::text), 'calculated', 'health-rollup')`, sys.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(f.ctx, `insert into property (ts, owner_kind, system_id, property_type_id, instance, value, provenance)
		values (clock_timestamp() + interval '1 second', 'system', $1::uuid, (select id from property_type where name = 'health'), '', to_jsonb('healthy'::text), 'declared')`, sys.ID); err != nil {
		t.Fatal(err)
	}
	vs, err := f.gw.SystemVerdicts(f.ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if v.SystemID == sys.ID && v.Verdict != "outage" {
			t.Errorf("SystemVerdicts serves %q, want the rollup's outage", v.Verdict)
		}
	}
	fv, err := f.gw.FleetProjection(f.ctx, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range fv.Systems {
		if s.ID == sys.ID && s.Verdict != "outage" {
			t.Errorf("the fleet projection serves %q, want the rollup's outage", s.Verdict)
		}
	}
	rep, err := f.gw.SystemHealth(f.ctx, sys.ID, 0, all)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(rep.Transitions); n == 0 || rep.Transitions[n-1].Value != "outage" {
		t.Errorf("the transitions end %+v, want the rollup's outage last", rep.Transitions)
	}
}

// TestLegacyRuleFiringAReservedEventLetsTheSampleLand: an event rule stored
// before its fire type was refused (one firing the node's collection-failed)
// writes no event, and the sample it judged still lands: an ingest is never
// refused for a rule's sake.
func TestLegacyRuleFiringAReservedEventLetsTheSampleLand(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateEventRule(f.ctx, "", storage.EventRuleSpec{Name: "legacy-forge", SampleKey: "endpoint-reachable",
		Fire: rules.Criteria{Expr: `value == "down"`}, FireEventType: "condition-started"}, all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(f.ctx, `update event_rule set fire_event_type_id = (select id from event_type where name = 'collection-failed')
		where name = 'legacy-forge'`); err != nil {
		t.Fatal(err)
	}
	comp, err := f.gw.GetComponent(f.ctx, "disp-1", all)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.gw.InsertPropertySamples(f.ctx, storage.NodeWriter(comp.ID, "ssh"), []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: comp.ID, Key: "endpoint-reachable", Instance: "ssh", Value: "down"}}); err != nil {
		t.Fatalf("the node's sample was refused for a legacy rule's sake: %v", err)
	}
	if got, err := f.gw.LatestProperty(f.ctx, "disp-1", "endpoint-reachable", "ssh"); err != nil || got == nil || got.Value != "down" {
		t.Fatalf("LatestProperty = %+v (%v), want the node's down", got, err)
	}
	if n := f.count(t, `select count(*) from event where event_type_id = (select id from event_type where name = 'collection-failed')`); n != 0 {
		t.Fatalf("the legacy rule wrote %d collection-failed events", n)
	}
}
