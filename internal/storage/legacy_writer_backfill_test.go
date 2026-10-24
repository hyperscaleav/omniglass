package storage_test

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The one-time backfill that holds legacy rows to the one-writer ruling
// (ADR-0163), driven from the embedded migration text so the test runs the
// exact statements the migration applies. storagetest.NewDSN migrates to
// head, so the backfill already ran once over an empty database; the test
// seeds what releases before the ruling could have left, runs it again, and
// runs it a third time to show it changes nothing more.

const legacyBackfill = "migrations/20261024120100_legacy_writer_backfill.sql"

func legacyBackfillSQL(t *testing.T) (up, down string) {
	t.Helper()
	raw, err := db.FS.ReadFile(legacyBackfill)
	if err != nil {
		t.Fatalf("read the backfill: %v", err)
	}
	up, down, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatal("the backfill has no -- migrate:down")
	}
	return strings.TrimPrefix(up, "-- migrate:up"), down
}

// TestLegacyBackfillNamesTheRuling: the key lists the backfill holds rows to
// are the code's ruling as of the migration, so a typo cannot leave a class
// of legacy rows untouched: every node-probe key and every pinnable key the
// code declares today is listed, and nothing else.
func TestLegacyBackfillNamesTheRuling(t *testing.T) {
	up, _ := legacyBackfillSQL(t)
	list := func(name string) []string {
		t.Helper()
		m := regexp.MustCompile(`(?s)` + name + ` text\[\] := ARRAY\[(.*?)\];`).FindStringSubmatch(up)
		if m == nil {
			t.Fatalf("the backfill declares no %s list", name)
		}
		var out []string
		for _, q := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(m[1], -1) {
			out = append(out, q[1])
		}
		slices.Sort(out)
		return out
	}
	if got, want := list("node_probe"), probekeys.NodeOnly(); !slices.Equal(got, want) {
		t.Errorf("the backfill's node-probe keys %v, the ruling's %v", got, want)
	}
	var pins []string
	for _, k := range writerclass.ReservedNames() {
		if writerclass.Pinnable(k) {
			pins = append(pins, k)
		}
	}
	if got := list("pinnable"); !slices.Equal(got, pins) {
		t.Errorf("the backfill's pinnable keys %v, the ruling's %v", got, pins)
	}
	for _, k := range []string{writerclass.Health, writerclass.CommandIssued} {
		if !strings.Contains(up, "'"+k+"'") {
			t.Errorf("the backfill does not hold %s rows to their writer", k)
		}
	}
}

// TestLegacyBackfillRetiresWhatTheRulingRefuses seeds the rows a release
// before the ruling could have left beside honest ones, and proves the
// backfill retires exactly the refused ones (each still on record in
// retired_sample with its reason), that the reads stop serving them, that the
// honest rows are untouched, and that a second run changes nothing.
func TestLegacyBackfillRetiresWhatTheRulingRefuses(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := pinFixture(t)
	all := scope.Set{All: true}
	d1 := f.componentID(t, "disp-1")
	sys, err := f.gw.CreateSystem(f.ctx, "", storage.SystemSpec{Name: "room-1"}, all, all)
	if err != nil {
		t.Fatal(err)
	}
	up, _ := legacyBackfillSQL(t)
	key := func(c string) string { return "SHA256:" + strings.Repeat(c, 43) }
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.conn.Exec(f.ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	hour := time.Now().UTC().Add(-time.Hour)

	// ssh: the node learned honest; then a pin was planted through the plain
	// property route, and the node published the planted key it then held.
	honest, planted := key("h"), key("p")
	exec(`insert into property (ts, owner_kind, component_id, property_type_id, instance, provenance, source, value)
		values ($1, 'component', $2::uuid, (select id from property_type where name = 'ssh-host-key'), 'ssh', 'observed', 'ssh', to_jsonb($3::text))`,
		hour.Add(-time.Hour), d1, honest)
	f.plantPin(t, "ssh-host-key", "ssh", planted)
	exec(`insert into property (ts, owner_kind, component_id, property_type_id, instance, provenance, source, value)
		values (clock_timestamp() + interval '1 second', 'component', $1::uuid, (select id from property_type where name = 'ssh-host-key'), 'ssh', 'observed', 'ssh', to_jsonb($2::text))`,
		d1, planted)
	if got := f.endpointTask(t, "ssh").HostKey; got != planted {
		t.Fatalf("before the backfill the worklist delivers %q; the seed should leave the planted key in force through its echo", got)
	}

	// tls: an operator's :retry pin as the release before the ruling wrote
	// it, with no source.
	var tlsID string
	if err := f.conn.QueryRow(f.ctx, `select id::text from endpoint where name = 'tls'`).Scan(&tlsID); err != nil {
		t.Fatal(err)
	}
	trusted := key("t")
	if _, err := f.gw.RetryEndpoint(f.ctx, f.actor, tlsID, storage.RetryPin{Certificate: trusted}, all, all); err != nil {
		t.Fatalf("retry: %v", err)
	}
	exec(`update property set source = '' where provenance = 'declared'
		and property_type_id = (select id from property_type where name = 'tls-certificate')`)
	if got := f.endpointTask(t, "tls").CertificatePin; got != "" {
		t.Fatalf("an unstamped :retry pin is delivered (%q) before the backfill; the stamp is what makes it trusted", got)
	}

	// Node-probe rows: the node's honest ones, and the refused kinds.
	exec(`insert into property (ts, owner_kind, component_id, property_type_id, instance, provenance, source, value)
		select v.ts, 'component', $1::uuid, (select id from property_type where name = v.key), v.instance, v.provenance, 'ssh', to_jsonb(v.value)
		from (values
			($2::timestamptz, 'endpoint-reachable', 'ssh', 'observed', 'down'),
			(now() + interval '1 year', 'endpoint-reachable', 'ssh', 'observed', 'up'),
			($2::timestamptz, 'endpoint-reachable', 'fake', 'observed', 'up'),
			($2::timestamptz, 'session-state', 'ssh', 'declared', 'up')) v(ts, key, instance, provenance, value)`,
		d1, hour)
	exec(`insert into property (ts, owner_kind, system_id, property_type_id, instance, provenance, source, value)
		values ($2, 'system', $1::uuid, (select id from property_type where name = 'endpoint-reachable'), 'ssh', 'observed', 'ssh', to_jsonb('up'::text))`,
		sys.ID, hour)
	exec(`insert into metric (ts, owner_kind, component_id, metric_type_id, instance, provenance, source, value)
		select v.ts, 'component', $1::uuid, (select id from metric_type where name = 'tcp-open'), v.instance, 'observed', 'tcp', v.value
		from (values ($2::timestamptz, 'ssh', 0), (now() + interval '1 year', 'ssh', 1), ($2::timestamptz, 'fake', 1)) v(ts, instance, value)`,
		d1, hour)
	exec(`insert into event (ts, owner_kind, component_id, event_type_id, instance, origin, message, provenance, source)
		select $2, 'component', $1::uuid, (select id from event_type where name = v.key), v.instance, 'caught', v.message, 'observed', 'push'
		from (values ('collection-failed', 'ssh', 'honest'), ('collection-failed', 'fake', 'refused'), ('command-issued', '', 'refused')) v(key, instance, message)`,
		d1, hour)
	if _, err := f.gw.IssueCommand(f.ctx, f.actor, "component", d1, "set-input", "", json.RawMessage(`"hdmi2"`), nil); err != nil {
		t.Fatalf("issue a command: %v", err)
	}
	// A command's reply, read off the device, recorded as an observation of
	// a node-probe key a command type was let target before the ruling: the
	// row carries the command's settlement instant. And a collection-failed
	// an event rule derived.
	exec(`insert into command_type (name, description, settle_window_seconds, target_property_type_id, official)
		values ('legacy-reach', '', 30, (select id from property_type where name = 'endpoint-reachable'), false)`)
	exec(`insert into command (ts, owner_kind, component_id, command_type_id, instance, status, settled_at)
		values ($2, 'component', $1::uuid, (select id from command_type where name = 'legacy-reach'), 'ssh', 'settled', $2::timestamptz + interval '1 minute')`,
		d1, hour)
	exec(`insert into property (ts, owner_kind, component_id, property_type_id, instance, provenance, source, value)
		values ($2::timestamptz + interval '1 minute', 'component', $1::uuid, (select id from property_type where name = 'endpoint-reachable'), 'ssh', 'observed', 'ssh', to_jsonb('up'::text))`,
		d1, hour)
	exec(`insert into event (ts, owner_kind, component_id, event_type_id, instance, origin, message, provenance, source, source_rule)
		values ($2, 'component', $1::uuid, (select id from event_type where name = 'collection-failed'), 'ssh', 'derived', 'derived', 'observed', 'rule', 'legacy-rule')`,
		d1, hour)
	// Health: the rollup's verdict, and a row it did not write.
	exec(`insert into property (ts, owner_kind, system_id, property_type_id, instance, value, provenance, source_rule)
		values ($2, 'system', $1::uuid, (select id from property_type where name = 'health'), '', to_jsonb('outage'::text), 'calculated', 'health-rollup')`, sys.ID, hour)
	exec(`insert into property (ts, owner_kind, system_id, property_type_id, instance, value, provenance)
		values ($2, 'system', $1::uuid, (select id from property_type where name = 'health'), '', to_jsonb('healthy'::text), 'declared')`, sys.ID, hour.Add(time.Minute))

	// Before: the refused rows are served.
	if got, err := f.gw.LatestProperty(f.ctx, "disp-1", "endpoint-reachable", "ssh"); err != nil || got == nil || got.Value != "up" {
		t.Fatalf("before the backfill LatestProperty = %+v (%v); the seed should leave the future-dated up in force", got, err)
	}

	if _, err := f.conn.Exec(f.ctx, up); err != nil {
		t.Fatalf("run the backfill: %v", err)
	}

	// The pins: the planted one and its echo are retired, the node's honest
	// key is the pin again, and the legacy :retry pin is stamped and
	// delivered.
	if got := f.endpointTask(t, "ssh").HostKey; got != honest {
		t.Errorf("after the backfill the ssh pin is %q, want the key the node learned before the plant", got)
	}
	if got := f.endpointTask(t, "tls").CertificatePin; got != trusted {
		t.Errorf("after the backfill the tls pin is %q, want the operator's :retry pin", got)
	}
	// The node-probe reads serve the node's honest rows.
	if got, err := f.gw.LatestProperty(f.ctx, "disp-1", "endpoint-reachable", "ssh"); err != nil || got == nil || got.Value != "down" {
		t.Errorf("LatestProperty = %+v (%v), want the node's down", got, err)
	}
	if got, err := f.gw.LatestMetricInstance(f.ctx, "disp-1", "tcp-open", "ssh"); err != nil || got == nil || got.Value != 0 {
		t.Errorf("LatestMetricInstance = %+v (%v), want the node's 0", got, err)
	}
	evs, err := f.gw.ListComponentEvents(f.ctx, "disp-1", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, e := range evs {
		kept = append(kept, e.Key+"/"+e.Message)
	}
	if !slices.Contains(kept, "collection-failed/honest") || slices.Contains(kept, "collection-failed/refused") ||
		slices.Contains(kept, "collection-failed/derived") || slices.Contains(kept, "command-issued/refused") {
		t.Errorf("events after the backfill: %v, want the honest collection-failed and the issued command's event only", kept)
	}
	if n := f.count(t, `select count(*) from command c join event e on e.id = c.caused_event_id`); n != 1 {
		t.Errorf("the issued command lost its event: %d", n)
	}
	vs, err := f.gw.SystemVerdicts(f.ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if v.SystemID == sys.ID && v.Verdict != "outage" {
			t.Errorf("the system reads %q, want the rollup's outage", v.Verdict)
		}
	}

	// Every retired row is on record with its reason.
	reasons := map[string]int{}
	rows, err := f.conn.Query(f.ctx, `select reason from retired_sample`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		reasons[r]++
	}
	rows.Close()
	want := map[string]int{
		"a declared pin not set through the endpoint's audited :retry":                    1,
		"a key observed under a declared pin not set through :retry, and never before it": 1,
		"a node-probe key on an owner no node writes for":                                 1,
		"a node-probe key written by a writer other than the node":                        2,
		"a node-probe key a command's reply wrote":                                        1,
		"a node-probe key under an instance that names no endpoint of its component":      3,
		"a node-probe key dated past the database clock":                                  2,
		"a command lifecycle event no command issued":                                     1,
		"a platform key the health rollup did not write":                                  1,
	}
	for r, n := range want {
		if reasons[r] != n {
			t.Errorf("retired %d rows for %q, want %d (all: %v)", reasons[r], r, n, reasons)
		}
	}
	for r := range reasons {
		if _, ok := want[r]; !ok {
			t.Errorf("retired rows for an unexpected reason %q", r)
		}
	}
	if n := f.count(t, `select count(*) from retired_sample where sample->>'id' is null or (sample->>'id')::bigint <> sample_id`); n != 0 {
		t.Errorf("%d retired rows lost their record", n)
	}

	// A second run changes nothing.
	snapshot := func() string {
		var s string
		if err := f.conn.QueryRow(f.ctx, `select (select count(*) from property) || '/' || (select count(*) from metric) || '/' ||
			(select count(*) from event) || '/' || (select count(*) from retired_sample) || '/' ||
			(select count(*) from property where source = 'key-pin')`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := snapshot()
	if _, err := f.conn.Exec(f.ctx, up); err != nil {
		t.Fatalf("run the backfill again: %v", err)
	}
	if after := snapshot(); after != before {
		t.Errorf("a second run changed the rows: %s, then %s", before, after)
	}
}

// TestLegacyBackfillDownRestoresTheRows: the backfill's down puts every row
// it retired back where it was, id and all, so the retirement is reversible
// in full.
func TestLegacyBackfillDownRestoresTheRows(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := pinFixture(t)
	d1 := f.componentID(t, "disp-1")
	up, down := legacyBackfillSQL(t)
	f.plantPin(t, "ssh-host-key", "ssh", "SHA256:"+strings.Repeat("p", 43))
	if _, err := f.conn.Exec(f.ctx, `insert into metric (ts, owner_kind, component_id, metric_type_id, instance, provenance, source, value)
		values (now(), 'component', $1::uuid, (select id from metric_type where name = 'tcp-open'), 'fake', 'observed', 'tcp', 1)`, d1); err != nil {
		t.Fatal(err)
	}
	ids := func() string {
		var s string
		if err := f.conn.QueryRow(f.ctx, `select coalesce(string_agg(id::text, ',' order by id), '') from
			(select id from property union all select id from metric) x`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := ids()
	if _, err := f.conn.Exec(f.ctx, up); err != nil {
		t.Fatal(err)
	}
	if ids() == before {
		t.Fatal("the backfill retired nothing")
	}
	if _, err := f.conn.Exec(f.ctx, down); err != nil {
		t.Fatalf("run the down: %v", err)
	}
	if got := ids(); got != before {
		t.Errorf("after the down the rows are %s, want %s", got, before)
	}
	if n := f.count(t, `select count(*) from retired_sample`); n != 0 {
		t.Errorf("%d rows stay retired after the down", n)
	}
}

// TestRetiredPlantedPinIsNeverRelearned: a pin retired as planted stays out
// of force however it comes back as an observation: a node still on an
// older binary republishes it from memory, or an echo stamped by a lagging
// node clock (dated before the plant, though published after it) survives
// in the series. Neither is delivered as the pin until an operator's :retry
// confirms the key.
func TestRetiredPlantedPinIsNeverRelearned(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := pinFixture(t)
	all := scope.Set{All: true}
	d1 := f.componentID(t, "disp-1")
	up, _ := legacyBackfillSQL(t)
	for _, pc := range pinCases {
		planted := "SHA256:" + strings.Repeat("x", 43)
		f.plantPin(t, pc.property, pc.transport, planted)
		// The node's echo, its clock ten minutes slow: dated before the plant.
		if _, err := f.conn.Exec(f.ctx, `insert into property (ts, owner_kind, component_id, property_type_id, instance, provenance, source, value)
			values (now() - interval '10 minutes', 'component', $1::uuid, (select id from property_type where name = $2), $3, 'observed', $3, to_jsonb($4::text))`,
			d1, pc.property, pc.transport, planted); err != nil {
			t.Fatal(err)
		}
		if _, err := f.conn.Exec(f.ctx, up); err != nil {
			t.Fatalf("run the backfill: %v", err)
		}
		if got := pc.delivered(f.endpointTask(t, pc.transport)); got != "" {
			t.Errorf("%s: after the backfill the pin is %q, want none: the lagging echo should not survive as the pin", pc.transport, got)
		}
		if n := f.count(t, `select count(*) from property where value #>> '{}' = $1
			and property_type_id = (select id from property_type where name = $2)`, planted, pc.property); n != 0 {
			t.Errorf("%s: %d rows of the planted key survive the backfill", pc.transport, n)
		}

		// A node still holding the planted key republishes it as observed.
		if err := f.gw.InsertPropertySamples(f.ctx, storage.NodeWriter(d1, pc.transport), []storage.PropertySampleWrite{
			{OwnerKind: "component", OwnerID: d1, Key: pc.property, Instance: pc.transport, Value: planted}}); err != nil {
			t.Fatalf("%s: the republish: %v", pc.transport, err)
		}
		if got := pc.delivered(f.endpointTask(t, pc.transport)); got != "" {
			t.Errorf("%s: a republished retired pin is delivered as %q, want none", pc.transport, got)
		}
		if pin, err := f.gw.EndpointKeyPin(f.ctx, "disp-1", pc.transport, pc.transport); err != nil || pin != "" {
			t.Errorf("%s: the reachability read serves %q (%v), want none", pc.transport, pin, err)
		}

		// An operator's :retry confirms the key: it is the pin again.
		var epID string
		if err := f.conn.QueryRow(f.ctx, `select id::text from endpoint where name = $1`, pc.transport).Scan(&epID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.gw.RetryEndpoint(f.ctx, f.actor, epID, pc.retry(planted), all, all); err != nil {
			t.Fatalf("%s: retry: %v", pc.transport, err)
		}
		if got := pc.delivered(f.endpointTask(t, pc.transport)); got != planted {
			t.Errorf("%s: after the operator's :retry the pin is %q, want the confirmed key", pc.transport, got)
		}
	}
}

// TestRetiredRowsReleaseTheConditionsTheyRaised: a condition, and the alarm
// with it, that a now-retired row raised (one under an instance naming no
// endpoint, one dated a year ahead) is released by the boot heal: the series
// with nothing left ends its condition, the series whose newest sample is
// the node's honest one is judged against it. A second heal changes nothing.
func TestRetiredRowsReleaseTheConditionsTheyRaised(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "edge", Severity: "high", ClearEventType: "condition-ended"})
	if _, err := r.f.conn.Exec(r.ctx, `insert into endpoint (name, transport, component, params)
		values ('tcp', 'tcp', (select id from component where name = 'bar-1'), '{"target":"127.0.0.1:1"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	up, _ := legacyBackfillSQL(t)

	// The node's honest up on tcp, then a down a year ahead; a down under an
	// instance bar-1 has no endpoint for.
	r.observe("tcp", "up")
	r.observe("tcp", "down")
	if _, err := r.f.conn.Exec(r.ctx, `update property set ts = now() + interval '1 year' where id = (select max(id) from property
		where property_type_id = (select id from property_type where name = 'endpoint-reachable') and instance = 'tcp')`); err != nil {
		t.Fatal(err)
	}
	r.observe("ghost", "down")
	if r.openAlarm("tcp") == "" || r.openAlarm("ghost") == "" || r.conditions() != 2 {
		t.Fatalf("the seed raised alarms tcp %q, ghost %q with %d conditions; want both open", r.openAlarm("tcp"), r.openAlarm("ghost"), r.conditions())
	}
	if _, err := r.f.conn.Exec(r.ctx, up); err != nil {
		t.Fatalf("run the backfill: %v", err)
	}
	n, err := r.f.gw.HealRetiredSeries(r.ctx)
	if err != nil {
		t.Fatalf("heal: %v", err)
	}
	if n != 2 {
		t.Errorf("the heal re-judged %d series, want 2", n)
	}
	if r.openAlarm("tcp") != "" || r.openAlarm("ghost") != "" || r.conditions() != 0 {
		t.Errorf("after the heal alarms tcp %q, ghost %q with %d conditions; want none", r.openAlarm("tcp"), r.openAlarm("ghost"), r.conditions())
	}
	events := len(r.events())
	if _, err := r.f.gw.HealRetiredSeries(r.ctx); err != nil {
		t.Fatalf("heal again: %v", err)
	}
	if got := len(r.events()); got != events || r.conditions() != 0 {
		t.Errorf("a second heal wrote %d more events", got-events)
	}
}
