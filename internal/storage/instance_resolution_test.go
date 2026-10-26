package storage_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// ADR-0456: an observed row's instance is the endpoint that observed it, and
// the owner-level question reconciliation asks resolves across every instance
// of the key: `is` is present when any endpoint reported the key, drift is
// worst-case (any instance disagreeing with want), and the answer names the
// instance it came from. `told` is the newest intended row across instances.
func TestReconciliationResolvesEveryObservedInstance(t *testing.T) {
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
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into endpoint (name, transport, component, node_name, params) values
		('ip-control', 'tcp', $1::uuid, (select principal_id from node where name = 'edge'), '{"target":"10.0.0.1:23"}'::jsonb),
		('serial', 'tcp', $1::uuid, (select principal_id from node where name = 'edge'), '{"target":"10.0.0.2:23"}'::jsonb)`, comp.ID)

	// want: the operator's owner-level declaration.
	if _, err := gw.SetProperty(ctx, "", "component", "panel-1", "video-input", "", json.RawMessage(`"hdmi1"`), all, all); err != nil {
		t.Fatalf("declare video-input: %v", err)
	}
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	// observe lands one reading through the node lane, as the consumer does
	// for a task on endpoint ep: the instance is the endpoint's name.
	observe := func(ep, value string, at time.Duration) {
		t.Helper()
		if err := gw.InsertPropertySamples(ctx, storage.NodeWriter(comp.ID, ep), []storage.PropertySampleWrite{{
			OwnerKind: "component", OwnerID: comp.ID, Key: "video-input", Instance: ep, Value: value, Source: "tcp", TS: base.Add(at),
		}}); err != nil {
			t.Fatalf("observe %s on %s: %v", value, ep, err)
		}
	}
	pivot := func() storage.PropertyReconciliation {
		t.Helper()
		recs, err := gw.Reconciliation(ctx, "component", "panel-1", all)
		if err != nil {
			t.Fatalf("reconciliation: %v", err)
		}
		for _, r := range recs {
			if r.PropertyTypeName == "video-input" {
				return r
			}
		}
		t.Fatalf("video-input not in the reconciliation: %+v", recs)
		return storage.PropertyReconciliation{}
	}

	// One endpoint agrees: is is its value, named, and there is no drift.
	observe("ip-control", "hdmi1", time.Second)
	if r := pivot(); string(r.Is) != `"hdmi1"` || r.IsInstance != "ip-control" || r.Drift {
		t.Fatalf("one agreeing endpoint: want is=hdmi1 from ip-control, no drift; got is=%s from %q drift=%v", r.Is, r.IsInstance, r.Drift)
	}
	// It moves away from want: drift, on that endpoint.
	observe("ip-control", "hdmi2", 2*time.Second)
	if r := pivot(); string(r.Is) != `"hdmi2"` || r.IsInstance != "ip-control" || !r.Drift {
		t.Fatalf("one disagreeing endpoint: want is=hdmi2 from ip-control, drift; got is=%s from %q drift=%v", r.Is, r.IsInstance, r.Drift)
	}

	// Two endpoints, worst case: the newest reading agrees, an older one on
	// another endpoint does not, and the disagreeing one is what is shows.
	observe("serial", "hdmi3", 3*time.Second)
	observe("ip-control", "hdmi1", 4*time.Second)
	if r := pivot(); string(r.Is) != `"hdmi3"` || r.IsInstance != "serial" || !r.Drift {
		t.Fatalf("one of two endpoints disagreeing: want is=hdmi3 from serial, drift; got is=%s from %q drift=%v", r.Is, r.IsInstance, r.Drift)
	}
	// Both agree: no drift, and is is the newest agreeing reading.
	observe("serial", "hdmi1", 5*time.Second)
	if r := pivot(); string(r.Is) != `"hdmi1"` || r.IsInstance != "serial" || r.Drift {
		t.Fatalf("both endpoints agreeing: want is=hdmi1 from serial, no drift; got is=%s from %q drift=%v", r.Is, r.IsInstance, r.Drift)
	}

	// told: a command addressed to one endpoint's series is still the
	// owner's newest intended value.
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	if _, err := gw.IssueCommand(ctx, actor, "component", comp.ID, "set-input", "serial", json.RawMessage(`"dp1"`), nil); err != nil {
		t.Fatalf("issue set-input on serial: %v", err)
	}
	if r := pivot(); string(r.Told) != `"dp1"` {
		t.Fatalf("an instanced command: want told=dp1, got %s", r.Told)
	}
}

// An endpoint deleted after it observed a property is a retired observer:
// its last reading is frozen (nothing prunes a series' latest row, and an
// endpoint's name is its transport, so a changed transport is a delete and a
// new endpoint), and reconciliation leaves it out rather than holding the
// owner in drift on what a removed connection once saw. An instance a push
// sender names is the sender's own and keeps counting (ADR-0456).
func TestReconciliationLeavesOutARetiredObserver(t *testing.T) {
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
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into endpoint (name, transport, component, node_name, params) values
		('telnet', 'telnet', $1::uuid, (select principal_id from node where name = 'edge'), '{"target":"10.0.0.1:23"}'::jsonb),
		('ssh', 'ssh', $1::uuid, (select principal_id from node where name = 'edge'), '{"target":"10.0.0.1:22"}'::jsonb)`, comp.ID)
	if _, err := gw.SetProperty(ctx, "", "component", "panel-1", "video-input", "", json.RawMessage(`"hdmi1"`), all, all); err != nil {
		t.Fatalf("declare video-input: %v", err)
	}
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	land := func(w storage.KeyWriter, instance, value string, at time.Duration) {
		t.Helper()
		if err := gw.InsertPropertySamples(ctx, w, []storage.PropertySampleWrite{{
			OwnerKind: "component", OwnerID: comp.ID, Key: "video-input", Instance: instance, Value: value, Source: "test", TS: base.Add(at),
		}}); err != nil {
			t.Fatalf("land %s on %s: %v", value, instance, err)
		}
	}
	pivot := func() storage.PropertyReconciliation {
		t.Helper()
		recs, err := gw.Reconciliation(ctx, "component", "panel-1", all)
		if err != nil {
			t.Fatalf("reconciliation: %v", err)
		}
		for _, r := range recs {
			if r.PropertyTypeName == "video-input" {
				return r
			}
		}
		t.Fatalf("video-input not in the reconciliation: %+v", recs)
		return storage.PropertyReconciliation{}
	}

	land(storage.NodeWriter(comp.ID, "telnet"), "telnet", "hdmi4", time.Second)
	if r := pivot(); string(r.Is) != `"hdmi4"` || r.IsInstance != "telnet" || !r.Drift {
		t.Fatalf("a live endpoint disagreeing: want is=hdmi4 from telnet, drift; got is=%s from %q drift=%v", r.Is, r.IsInstance, r.Drift)
	}
	if err := gw.DeleteEndpoint(ctx, "", comp.ID+"/telnet", all, all); err != nil {
		t.Fatalf("delete the telnet endpoint: %v", err)
	}
	land(storage.NodeWriter(comp.ID, "ssh"), "ssh", "hdmi1", 2*time.Second)
	if r := pivot(); string(r.Is) != `"hdmi1"` || r.IsInstance != "ssh" || r.Drift {
		t.Fatalf("the disagreeing endpoint retired: want is=hdmi1 from ssh, no drift; got is=%s from %q drift=%v", r.Is, r.IsInstance, r.Drift)
	}
	land(storage.PushWriter(), "rx-2", "hdmi3", 3*time.Second)
	if r := pivot(); string(r.Is) != `"hdmi3"` || r.IsInstance != "rx-2" || !r.Drift {
		t.Fatalf("a pushed instance disagreeing: want is=hdmi3 from rx-2, drift; got is=%s from %q drift=%v", r.Is, r.IsInstance, r.Drift)
	}
}

// told reads the intended series of the instances commands named, one probe
// each, and never walks the observed instances of the key: the series index
// carries no provenance, so walking them for an intended row filters every
// observed row they hold (the firehose, on a busy key).
func TestToldReadsOnlyTheInstancesCommandsNamed(t *testing.T) {
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
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	conn := connectDSN(t, dsn)
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	if _, err := gw.IssueCommand(ctx, actor, "component", comp.ID, "set-input", "", json.RawMessage(`"dp1"`), nil); err != nil {
		t.Fatalf("issue set-input: %v", err)
	}
	// Two busy observed instances, written after the intent, as a polled
	// switcher's endpoints would.
	mustExec(t, conn, `insert into property (ts, owner_kind, component_id, property_type_id, instance, value, provenance, source)
		select now() + make_interval(secs => g), 'component', $1::uuid, (select id from property_type where name = 'video-input'),
			i, to_jsonb('hdmi1'::text), 'observed', 'tcp'
		from generate_series(1, 2000) g, unnest(array['ip-control', 'serial']) i`, comp.ID)
	// Seqscan is off because the fixture is small enough for one to win, as
	// TestLatestPerInstanceIsAnIndexWalk does.
	mustExec(t, conn, "set enable_seqscan = off")

	rows, err := conn.Query(ctx, "explain (analyze, format json) "+storage.ExportToldSQL(), "component", comp.ID, "video-input")
	if err != nil {
		t.Fatalf("explain the told read: %v", err)
	}
	var plan string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan += line
	}
	rows.Close()
	if removed := rowsRemovedByFilter(t, plan); removed > 0 {
		t.Fatalf("the told read filtered %d rows to find the intended one: it walked the observed instances:\n%s", removed, plan)
	}

	recs, err := gw.Reconciliation(ctx, "component", "panel-1", all)
	if err != nil {
		t.Fatalf("reconciliation: %v", err)
	}
	for _, r := range recs {
		if r.PropertyTypeName == "video-input" && string(r.Told) != `"dp1"` {
			t.Fatalf("told = %s, want dp1", r.Told)
		}
	}
}

// rowsRemovedByFilter sums every "Rows Removed by Filter" in a JSON plan.
func rowsRemovedByFilter(t *testing.T, plan string) int {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(plan), &doc); err != nil {
		t.Fatalf("read the plan: %v", err)
	}
	total := 0
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if n, ok := x["Rows Removed by Filter"].(float64); ok {
				total += int(n)
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(doc)
	return total
}

// The multi-instance read walks a key's instances and reads each one's newest
// row by probes on the series index (the walk readCalcInput makes), never the
// key's whole history, which on the metric lane is the firehose. Seqscan is
// off because the fixture's tables are tiny, as TestSeriesNewestIsAnIndexProbe
// does for the one-series read.
func TestLatestPerInstanceIsAnIndexWalk(t *testing.T) {
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
	conn := connectDSN(t, dsn)
	mustExec(t, conn, "set enable_seqscan = off")
	for _, tc := range []struct{ lane, index string }{
		{"property", "property_series_idx"},
		{"metric", "metric_owner_idx"},
	} {
		rows, err := conn.Query(ctx, "explain "+storage.ExportLatestPerInstanceSQL(tc.lane), "component", uuid.NewString(), "video-input", []string{"tcp", "ssh"})
		if err != nil {
			t.Fatalf("explain %s: %v", tc.lane, err)
		}
		var plan string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan += line + "\n"
		}
		rows.Close()
		if strings.Count(plan, tc.index) < 2 {
			t.Errorf("the %s lane's per-instance read does not walk %s (the instance walk and the newest probe):\n%s", tc.lane, tc.index, plan)
		}
	}
}
