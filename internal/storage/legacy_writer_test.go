package storage_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The one-writer ruling (ADR-0161) held against what was stored before it:
// a node's writer is bound to its task's component as well as its endpoint, a
// command's lifecycle event is the command's alone, a contract cannot declare
// a reserved key, a declared pin is trusted only from the audited :retry, and
// a stored spec or derived task cannot carry a reserved emit to a node.

func (f writerFixture) componentID(t *testing.T, name string) string {
	t.Helper()
	comp, err := f.gw.GetComponent(f.ctx, name, scope.Set{All: true})
	if err != nil {
		t.Fatalf("get component %s: %v", name, err)
	}
	return comp.ID
}

// TestNodeWriterBindsItsComponentAndEndpoint: a node's writer speaks for the
// component its task collects for and the endpoint it ran against, both. A
// batch landing on another component under the same endpoint name, or from a
// writer bound to no component or no endpoint, is refused whole.
func TestNodeWriterBindsItsComponentAndEndpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateComponent(f.ctx, "", storage.ComponentSpec{Name: "disp-2"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := f.conn.Exec(f.ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('ssh', 'ssh', (select id from component where name = 'disp-2'), (select principal_id from node where name = 'node-a'), '{"target":"127.0.0.2:22"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	d1, d2 := f.componentID(t, "disp-1"), f.componentID(t, "disp-2")
	node := storage.NodeWriter(d1, "ssh")
	prop := func(owner, key, instance, value string) []storage.PropertySampleWrite {
		return []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: owner, Key: key, Instance: instance, Value: value}}
	}

	if err := f.gw.InsertPropertySamples(f.ctx, node, prop(d1, "endpoint-reachable", "ssh", "up")); err != nil {
		t.Fatalf("the node's own verdict on its own endpoint: %v", err)
	}
	if err := f.gw.InsertPropertySamples(f.ctx, node, prop(d1, "video-input", "", "hdmi1")); err != nil {
		t.Fatalf("a driver emit on the node's own component: %v", err)
	}

	for name, write := range map[string]func() error{
		"a verdict on another component's same-named endpoint": func() error {
			return f.gw.InsertPropertySamples(f.ctx, node, prop(d2, "endpoint-reachable", "ssh", "down"))
		},
		"a probe metric on another component": func() error {
			return f.gw.InsertMetricSamples(f.ctx, node, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: d2, Key: "tcp-open", Instance: "ssh", Value: 0}})
		},
		"collection-failed on another component": func() error {
			return f.gw.InsertEvents(f.ctx, node, []storage.EventWrite{{OwnerKind: "component", OwnerID: d2, Key: "collection-failed", Instance: "ssh", Message: "forged"}})
		},
		"a driver emit on another component": func() error {
			return f.gw.InsertPropertySamples(f.ctx, node, prop(d2, "video-input", "", "hdmi2"))
		},
		"a batch that strays onto another component midway": func() error {
			return f.gw.InsertPropertySamples(f.ctx, node, append(prop(d1, "video-input", "", "hdmi3"), prop(d2, "video-input", "", "hdmi3")...))
		},
		"a node bound to no endpoint": func() error {
			return f.gw.InsertPropertySamples(f.ctx, storage.NodeWriter(d1, ""), prop(d1, "video-input", "", "hdmi4"))
		},
		"a node bound to no component": func() error {
			return f.gw.InsertMetricSamples(f.ctx, storage.NodeWriter("", "ssh"), []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: d1, Key: "tcp-open", Instance: "ssh", Value: 1}})
		},
	} {
		if err := write(); !refused(err) {
			t.Errorf("%s: %v, want it refused", name, err)
		}
	}
	if n := f.count(t, `select (select count(*) from property where component_id = $1::uuid)
		+ (select count(*) from metric where component_id = $1::uuid)
		+ (select count(*) from event where component_id = $1::uuid)`, d2); n != 0 {
		t.Errorf("%d rows landed on the component the node does not collect for", n)
	}
	if n := f.count(t, `select count(*) from property where component_id = $1::uuid and value #>> '{}' in ('hdmi3', 'hdmi4')`, d1); n != 0 {
		t.Errorf("a refused batch landed %d of its rows", n)
	}
}

// TestCommandLifecycleIsTheCommandsAlone: command-issued puts a command on a
// component's timeline, so only a command's issue writes it. A push, a node and
// an event rule (declared now, or stored before the refusal) are refused, and
// the command's own issue still lands it.
func TestCommandLifecycleIsTheCommandsAlone(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	d1 := f.componentID(t, "disp-1")
	issued := func() int {
		return f.count(t, `select count(*) from event where event_type_id = (select id from event_type where name = 'command-issued')`)
	}
	ev := []storage.EventWrite{{OwnerKind: "component", OwnerID: d1, Key: "command-issued", Instance: "ssh", Message: "forged"}}
	if err := f.gw.InsertEvents(f.ctx, storage.PushWriter(), ev); !refused(err) || !strings.Contains(err.Error(), "command-issued") {
		t.Errorf("a pushed command-issued = %v, want it refused naming the key", err)
	}
	if err := f.gw.InsertEvents(f.ctx, storage.NodeWriter(d1, "ssh"), ev); !refused(err) {
		t.Errorf("a node's command-issued = %v, want it refused", err)
	}
	if err := f.gw.WriteRuleEvent(f.ctx, uuidZero, "command-issued", d1, ""); !refused(err) {
		t.Errorf("an event rule's command-issued = %v, want it refused", err)
	}
	for _, rule := range []storage.EventRuleSpec{
		{Name: "forge-fire", SampleKey: "video-input", Fire: rules.Criteria{Expr: `value == "hdmi1"`}, FireEventType: "command-issued"},
		{Name: "forge-clear", SampleKey: "video-input", Fire: rules.Criteria{Expr: `value == "hdmi1"`}, FireEventType: "condition-started", ClearEventType: "command-issued"},
	} {
		if _, err := f.gw.CreateEventRule(f.ctx, "", rule, all); !errors.Is(err, storage.ErrEventRuleInvalid) || !strings.Contains(err.Error(), "command-issued") {
			t.Errorf("event rule %s = %v, want it refused naming command-issued", rule.Name, err)
		}
	}
	if _, err := f.gw.CreateEventType(f.ctx, "", storage.EventTypeSpec{Name: "Command-Issued"}); err == nil {
		t.Error("an operator created an event type under the command lifecycle's name")
	}

	// A rule stored before the refusal fires nothing, and the sample it
	// judged still lands.
	if _, err := f.gw.CreateEventRule(f.ctx, "", storage.EventRuleSpec{Name: "legacy-forge", SampleKey: "video-input",
		Fire: rules.Criteria{Expr: `value == "hdmi1"`}, FireEventType: "condition-started"}, all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(f.ctx, `update event_rule set fire_event_type_id = (select id from event_type where name = 'command-issued')
		where name = 'legacy-forge'`); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.InsertPropertySamples(f.ctx, storage.PushWriter(), []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: d1, Key: "video-input", Value: "hdmi1"}}); err != nil {
		t.Fatalf("the sample was refused for a legacy rule's sake: %v", err)
	}
	if n := issued(); n != 0 {
		t.Fatalf("%d command-issued events landed by a writer other than a command", n)
	}

	// The command's own issue (the seeded set-input) writes it, and points at
	// it.
	if _, err := f.gw.IssueCommand(f.ctx, f.actor, "component", d1, "set-input", "", json.RawMessage(`"hdmi2"`), nil); err != nil {
		t.Fatalf("issue a command: %v", err)
	}
	if n := issued(); n != 1 {
		t.Fatalf("the command's issue landed %d command-issued events, want 1", n)
	}
	if n := f.count(t, `select count(*) from command c join event e on e.id = c.caused_event_id
		where e.event_type_id = (select id from event_type where name = 'command-issued')`); n != 1 {
		t.Errorf("%d commands point at their command-issued event, want 1", n)
	}
}

// TestContractsRefuseAReservedKey: a product, standard or location-type
// contract cannot declare a reserved key, on the operator's path or the
// seed's, so no default ever stands in for a node's observation or the
// platform's verdict. A contract stored before the refusal keeps its line but
// lends the key no default.
func TestContractsRefuseAReservedKey(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateProduct(f.ctx, "", storage.Product{Name: "acme-widget", Label: "Acme Widget", Kind: "device"}); err != nil {
		t.Fatalf("create product: %v", err)
	}
	if _, err := f.gw.CreateStandard(f.ctx, "", storage.Standard{Name: "acme-room", Label: "Acme Room"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	if _, err := f.gw.CreateLocationType(f.ctx, "", storage.LocationType{Name: "acme-site", Label: "Acme Site", AllowedParentTypes: []string{storage.RootPlacement}}); err != nil {
		t.Fatalf("create location type: %v", err)
	}
	def := json.RawMessage(`"up"`)
	num := json.RawMessage(`1`)
	reservedContract := func(err error, key string) bool {
		return errors.Is(err, storage.ErrContractKeyReserved) && strings.Contains(err.Error(), key)
	}
	for _, key := range []string{"endpoint-reachable", "ssh-host-key", "session-state", "health"} {
		for name, write := range map[string]func() error{
			"product": func() error {
				_, err := f.gw.SetProductProperty(f.ctx, "", "acme-widget", storage.ProductPropertySpec{PropertyTypeName: key, DefaultValue: def})
				return err
			},
			"product seed": func() error {
				return f.gw.UpsertProductProperty(f.ctx, "acme-widget", storage.ProductPropertySpec{PropertyTypeName: key, DefaultValue: def})
			},
			"standard": func() error {
				_, err := f.gw.SetStandardProperty(f.ctx, "", "acme-room", storage.StandardPropertySpec{PropertyTypeName: key, DefaultValue: def})
				return err
			},
			"standard seed": func() error {
				return f.gw.UpsertStandardProperty(f.ctx, "acme-room", storage.StandardPropertySpec{PropertyTypeName: key, DefaultValue: def})
			},
			"location type": func() error {
				_, err := f.gw.SetLocationTypeProperty(f.ctx, "", "acme-site", storage.LocationTypePropertySpec{PropertyTypeName: key, Required: true})
				return err
			},
			"location type seed": func() error {
				return f.gw.UpsertLocationTypeProperty(f.ctx, "acme-site", storage.LocationTypePropertySpec{PropertyTypeName: key})
			},
		} {
			if err := write(); !reservedContract(err, key) {
				t.Errorf("a %s contract on %s = %v, want ErrContractKeyReserved naming it", name, key, err)
			}
		}
	}
	for _, key := range []string{"tcp-open", "icmp-rtt-avg"} {
		for name, write := range map[string]func() error{
			"product": func() error {
				_, err := f.gw.SetProductMetric(f.ctx, "", "acme-widget", storage.ProductMetricSpec{MetricTypeName: key, DefaultValue: num})
				return err
			},
			"product seed": func() error {
				return f.gw.UpsertProductMetric(f.ctx, "acme-widget", storage.ProductMetricSpec{MetricTypeName: key, DefaultValue: num})
			},
			"standard": func() error {
				_, err := f.gw.SetStandardMetric(f.ctx, "", "acme-room", storage.StandardMetricSpec{MetricTypeName: key, DefaultValue: num})
				return err
			},
			"standard seed": func() error {
				return f.gw.UpsertStandardMetric(f.ctx, "acme-room", storage.StandardMetricSpec{MetricTypeName: key, DefaultValue: num})
			},
			"location type": func() error {
				_, err := f.gw.SetLocationTypeMetric(f.ctx, "", "acme-site", storage.LocationTypeMetricSpec{MetricTypeName: key, DefaultValue: num})
				return err
			},
			"location type seed": func() error {
				return f.gw.UpsertLocationTypeMetric(f.ctx, "acme-site", storage.LocationTypeMetricSpec{MetricTypeName: key, DefaultValue: num})
			},
		} {
			if err := write(); !reservedContract(err, key) {
				t.Errorf("a %s contract on %s = %v, want ErrContractKeyReserved naming it", name, key, err)
			}
		}
	}
	if n := f.count(t, `select (select count(*) from product_property) + (select count(*) from standard_property)
		+ (select count(*) from location_type_property) + (select count(*) from product_metric where product_id = (select id from product where name = 'acme-widget'))
		+ (select count(*) from standard_metric where standard_id = (select id from standard where name = 'acme-room'))
		+ (select count(*) from location_type_metric)`); n != f.count(t, `select count(*) from product_property`) {
		t.Errorf("a refused contract landed: %d rows beyond the seeded products'", n)
	}
	// A collected key is still a contract's to declare.
	if _, err := f.gw.SetProductProperty(f.ctx, "", "acme-widget", storage.ProductPropertySpec{PropertyTypeName: "firmware-version", DefaultValue: json.RawMessage(`"1.0"`)}); err != nil {
		t.Errorf("a contract on firmware-version: %v", err)
	}
	if _, err := f.gw.SetProductMetric(f.ctx, "", "acme-widget", storage.ProductMetricSpec{MetricTypeName: "uptime", DefaultValue: num}); err != nil {
		t.Errorf("a contract on uptime: %v", err)
	}

	// Lines stored before the refusal lend no default.
	if _, err := f.conn.Exec(f.ctx, `
		insert into product_property (product_id, property_type_id, default_value, required)
		select p.id, t.id, '"up"'::jsonb, false from product p, property_type t where p.name = 'acme-widget' and t.name = 'endpoint-reachable';
		insert into product_metric (product_id, metric_type_id, default_value, required)
		select p.id, t.id, '1'::jsonb, false from product p, metric_type t where p.name = 'acme-widget' and t.name = 'tcp-open'`); err != nil {
		t.Fatalf("plant legacy contract lines: %v", err)
	}
	product := "acme-widget"
	if _, err := f.gw.CreateComponent(f.ctx, "", storage.ComponentSpec{Name: "widget-1", ProductName: &product}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	props, err := f.gw.EffectiveProperties(f.ctx, "component", "widget-1", all)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range props {
		switch p.PropertyTypeName {
		case "endpoint-reachable":
			if p.Value != nil || p.DefaultValue != nil {
				t.Errorf("a legacy contract default stands in for endpoint-reachable: %+v", p)
			}
		case "firmware-version":
			if string(p.Value) != `"1.0"` {
				t.Errorf("firmware-version = %s, want its contract default", p.Value)
			}
		}
	}
	metrics, err := f.gw.EffectiveMetrics(f.ctx, "component", "widget-1", all)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range metrics {
		if m.MetricTypeName == "tcp-open" && (m.Value != nil || m.DefaultValue != nil) {
			t.Errorf("a legacy contract default stands in for tcp-open: %+v", m)
		}
	}
	vs, err := f.gw.WriterRulingViolations(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"endpoint-reachable", "tcp-open"} {
		if !slices.ContainsFunc(vs, func(v storage.RulingViolation) bool { return v.Key == key && strings.Contains(v.Where, "acme-widget") }) {
			t.Errorf("the boot audit does not flag the legacy contract on %s: %+v", key, vs)
		}
	}
}

// pinFixture is the writer fixture's ssh and tls endpoints, each with a task,
// so the node's worklist carries both.
func pinFixture(t *testing.T) writerFixture {
	t.Helper()
	f := newWriterFixture(t)
	if _, err := f.conn.Exec(f.ctx, `insert into task (id, mode, endpoint_id, spec)
		select 't-' || name || '-probe', 'poll', id, '{}'::jsonb from endpoint where name in ('ssh', 'tls')`); err != nil {
		t.Fatalf("insert tasks: %v", err)
	}
	return f
}

// pinCase is one transport's pin: the property it rides, the worklist field
// that delivers it, and the :retry that sets it.
type pinCase struct {
	transport, property string
	delivered           func(storage.WorklistTask) string
	retry               func(key string) storage.RetryPin
}

var pinCases = []pinCase{
	{"ssh", "ssh-host-key", func(t storage.WorklistTask) string { return t.HostKey },
		func(key string) storage.RetryPin { return storage.RetryPin{HostKey: key} }},
	{"tls", "tls-certificate", func(t storage.WorklistTask) string { return t.CertificatePin },
		func(key string) storage.RetryPin { return storage.RetryPin{Certificate: key} }},
}

func (f writerFixture) endpointTask(t *testing.T, transport string) storage.WorklistTask {
	t.Helper()
	wl, err := f.gw.NodeWorklist(f.ctx, "node-a")
	if err != nil {
		t.Fatalf("worklist: %v", err)
	}
	for _, task := range wl.Tasks {
		if task.Transport == transport {
			return task
		}
	}
	t.Fatalf("no %s task on the worklist: %+v", transport, wl.Tasks)
	return storage.WorklistTask{}
}

// plantPin writes a declared pin as the plain property route did before the
// writer check refused it: declared, no source, no :retry audit row beside
// it.
func (f writerFixture) plantPin(t *testing.T, property, instance, key string) {
	t.Helper()
	if _, err := f.conn.Exec(f.ctx, `
		insert into property (ts, owner_kind, component_id, property_type_id, instance, provenance, value)
		values (clock_timestamp(), 'component', (select id from component where name = 'disp-1'),
		        (select id from property_type where name = $1), $2, 'declared', to_jsonb($3::text))`, property, instance, key); err != nil {
		t.Fatalf("plant a pin: %v", err)
	}
}

// TestPlantedPinIsNeverDelivered: the worklist delivers a declared pin (an
// ssh host key, a tls certificate's public key) only when it came through the
// endpoint's audited :retry. A pin planted through the plain property route
// before the writer check is never delivered and never moves the fingerprint,
// so the next connection trusts on first use (or the key the node already
// learned); a :retry pin is delivered over a planted one, before or after it.
func TestPlantedPinIsNeverDelivered(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := pinFixture(t)
	all := scope.Set{All: true}
	d1 := f.componentID(t, "disp-1")
	for _, pc := range pinCases {
		var epID string
		if err := f.conn.QueryRow(f.ctx, `select id::text from endpoint where name = $1`, pc.transport).Scan(&epID); err != nil {
			t.Fatalf("endpoint id: %v", err)
		}
		key := func(c string) string { return "SHA256:" + strings.Repeat(c, 43) }
		planted, learned, trusted, next := key("p"), key("l"), key("r"), key("n")

		base := f.endpointTask(t, pc.transport)
		if got := pc.delivered(base); got != "" {
			t.Fatalf("%s: a fresh endpoint delivers pin %q", pc.transport, got)
		}
		f.plantPin(t, pc.property, pc.transport, planted)
		if got := f.endpointTask(t, pc.transport); pc.delivered(got) != "" || got.Fingerprint != base.Fingerprint {
			t.Fatalf("%s: a planted pin is delivered: pin %q, fingerprint moved %v; want trust on first use and nothing moved",
				pc.transport, pc.delivered(got), got.Fingerprint != base.Fingerprint)
		}
		if pin, err := f.gw.EndpointKeyPin(f.ctx, "disp-1", pc.transport, pc.transport); err != nil || pin != "" {
			t.Fatalf("%s: the reachability read serves pin %q (%v), want none", pc.transport, pin, err)
		}

		// The key the node learned on first use is still the pin.
		if err := f.gw.InsertPropertySamples(f.ctx, storage.NodeWriter(d1, pc.transport), []storage.PropertySampleWrite{
			{OwnerKind: "component", OwnerID: d1, Key: pc.property, Instance: pc.transport, Value: learned}}); err != nil {
			t.Fatalf("%s: the node publishes its key: %v", pc.transport, err)
		}
		if got := f.endpointTask(t, pc.transport); pc.delivered(got) != learned || got.Fingerprint != base.Fingerprint {
			t.Fatalf("%s: pin %q (fingerprint moved %v), want the learned key and the fingerprint unmoved",
				pc.transport, pc.delivered(got), got.Fingerprint != base.Fingerprint)
		}

		// The :retry pin wins, and a pin planted after it changes nothing.
		if _, err := f.gw.RetryEndpoint(f.ctx, f.actor, epID, pc.retry(trusted), all, all); err != nil {
			t.Fatalf("%s: retry with a key: %v", pc.transport, err)
		}
		afterRetry := f.endpointTask(t, pc.transport)
		if pc.delivered(afterRetry) != trusted {
			t.Fatalf("%s: pin %q after :retry, want %q", pc.transport, pc.delivered(afterRetry), trusted)
		}
		f.plantPin(t, pc.property, pc.transport, planted)
		if got := f.endpointTask(t, pc.transport); pc.delivered(got) != trusted || got.Fingerprint != afterRetry.Fingerprint {
			t.Fatalf("%s: a pin planted after :retry: pin %q, fingerprint moved %v; want the :retry pin and nothing moved",
				pc.transport, pc.delivered(got), got.Fingerprint != afterRetry.Fingerprint)
		}
		if pin, err := f.gw.EndpointKeyPin(f.ctx, "disp-1", pc.transport, pc.transport); err != nil || pin != trusted {
			t.Fatalf("%s: the reachability read serves pin %q (%v), want the :retry pin", pc.transport, pin, err)
		}
		// The pin a later trust replaces, as its audit row records it, is the
		// :retry pin, never the planted one.
		if _, err := f.gw.RetryEndpoint(f.ctx, f.actor, epID, pc.retry(next), all, all); err != nil {
			t.Fatalf("%s: retry with the next key: %v", pc.transport, err)
		}
		var replaced string
		if err := f.conn.QueryRow(f.ctx, `select coalesce(old->>'host_key', old->>'certificate', '') from audit_log
			where resource = 'endpoint' and verb = 'retry' and $1 in (new->>'host_key', new->>'certificate')`, next).Scan(&replaced); err != nil {
			t.Fatal(err)
		}
		if replaced != trusted {
			t.Errorf("%s: the audit row records replacing %q, want the :retry pin %q", pc.transport, replaced, trusted)
		}
	}
}

// TestStoredSpecsAndTasksCarryNoReservedEmit: a driver spec stored before the
// emit refusal, and tasks derived from it, never hand a node an emit its
// writer class refuses (a reserved key, a calc rule's output): the worklist
// strips it and keeps the rest, a re-derivation from the stored spec bakes
// none, and the boot audit flags the spec and the task.
func TestStoredSpecsAndTasksCarryNoReservedEmit(t *testing.T) {
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
	spec := `{"version":1,"transport":"tcp","inputs":[{"name":"host","kind":"string","required":true}],
		"polls":[{"name":"status","schedule":{"every":"30s"},"request":{"line":"GET STATUS"},
		"emits":[{"name":"video-input","extract":{"regex":"^INPUT (\\S+)$"}}]}]}`
	if _, err := f.gw.CreateDriver(f.ctx, "", storage.Driver{Name: "legacy-bar", Label: "Legacy Bar", Spec: []byte(spec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	comp, drv, node := "disp-1", "legacy-bar", "node-a"
	ep, err := f.gw.CreateEndpoint(f.ctx, "", storage.EndpointSpec{Driver: &drv, Component: &comp, Node: &node,
		Inputs: map[string]string{"host": "10.0.0.9"}, SecretRead: all, CanAdmin: true}, all)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	// What a release before the refusal could have stored: the spec and the
	// task derived from it (its content address its own) both emit the
	// node's verdict, the platform's health and a calc rule's output beside
	// the honest emit.
	forged := []string{"endpoint-reachable", "health", "avg-uptime"}
	if _, err := f.conn.Exec(f.ctx, `
		update driver set spec = jsonb_set(spec, '{polls,0,emits}', spec #> '{polls,0,emits}' ||
			'[{"name":"endpoint-reachable","extract":{"regex":"^R (\\S+)$"}},{"name":"health","extract":{"regex":"^H (\\S+)$"}},{"name":"avg-uptime","extract":{"regex":"^A (\\S+)$"}}]'::jsonb)
		where name = 'legacy-bar'`); err != nil {
		t.Fatalf("plant legacy emits in the spec: %v", err)
	}
	if _, err := f.conn.Exec(f.ctx, `
		update task set id = 'legacy-' || id, spec = jsonb_set(spec, '{emits}', spec->'emits' ||
			'[{"name":"endpoint-reachable","lane":"property","extract":{"regex":"^R (\\S+)$"}},{"name":"health","lane":"property","extract":{"regex":"^H (\\S+)$"}},{"name":"avg-uptime","lane":"metric","extract":{"regex":"^A (\\S+)$"}}]'::jsonb)
		where endpoint_id = $1::uuid and spec->>'driver' is not null`, ep.ID); err != nil {
		t.Fatalf("plant legacy emits in the task: %v", err)
	}
	var taskID string
	if err := f.conn.QueryRow(f.ctx, `select id from task where endpoint_id = $1::uuid and spec->>'driver' is not null`, ep.ID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}

	emitsOnTheWorklist := func() []string {
		t.Helper()
		wl, err := f.gw.NodeWorklist(f.ctx, "node-a")
		if err != nil {
			t.Fatalf("worklist: %v", err)
		}
		var names []string
		for _, task := range wl.Tasks {
			if !driver.IsBaked(task.Spec) {
				continue
			}
			fn, err := driver.ParseBaked(task.Spec)
			if err != nil {
				t.Fatalf("task %s spec does not parse: %v", task.ID, err)
			}
			for _, em := range fn.Emits {
				names = append(names, em.Name)
			}
		}
		return names
	}
	got := emitsOnTheWorklist()
	for _, key := range forged {
		if slices.Contains(got, key) {
			t.Errorf("the worklist hands the node an emit of %s: %v", key, got)
		}
	}
	if !slices.Contains(got, "video-input") {
		t.Errorf("the honest emit was dropped with the forged ones: %v", got)
	}

	// The boot audit names the stored spec and the derived task.
	vs, err := f.gw.WriterRulingViolations(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range forged {
		for _, where := range []string{"driver legacy-bar", "task " + taskID} {
			if !slices.ContainsFunc(vs, func(v storage.RulingViolation) bool { return v.Key == key && strings.Contains(v.Where, where) }) {
				t.Errorf("the boot audit does not flag %s in %s: %+v", key, where, vs)
			}
		}
	}
	for _, v := range vs {
		if v.Key == "video-input" {
			t.Errorf("the boot audit flags the honest emit: %+v", v)
		}
		if c, _ := writerclass.Reserved(v.Key); v.Key != "avg-uptime" && v.Key != "tcp-open" && !c.Reserved() {
			t.Errorf("the boot audit flags %s, which no ruling refuses", v.Key)
		}
	}

	// A re-derivation from the stored spec (an unrelated edit) bakes none.
	label := "Legacy Bar (relabelled)"
	if _, err := f.gw.UpdateDriver(f.ctx, "", "legacy-bar", storage.DriverPatch{Label: &label}); err != nil {
		t.Fatalf("relabel the driver: %v", err)
	}
	if n := f.count(t, `select count(*) from task t, jsonb_array_elements(t.spec->'emits') e
		where t.endpoint_id = $1::uuid and e->>'name' = any($2)`, ep.ID, forged); n != 0 {
		t.Errorf("a re-derivation from the stored spec baked %d forged emits", n)
	}
	got = emitsOnTheWorklist()
	if !slices.Contains(got, "video-input") || slices.ContainsFunc(got, func(n string) bool { return slices.Contains(forged, n) }) {
		t.Errorf("after the re-derivation the worklist carries %v", got)
	}
}

// TestARuleWhoseEventBecameReservedCanStillLetGo: a rule stored with a clear
// event type the ruling now reserves (command-issued) still ends the
// condition it holds, writing no event, so it can be deleted; and the boot
// audit names it, with a command type stored targeting a reserved key.
func TestARuleWhoseEventBecameReservedCanStillLetGo(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWriterFixture(t)
	all := scope.Set{All: true}
	d1 := f.componentID(t, "disp-1")
	if _, err := f.gw.CreateEventRule(f.ctx, "", storage.EventRuleSpec{Name: "legacy-clear", SampleKey: "video-input",
		Fire: rules.Criteria{Expr: `value == "hdmi1"`}, FireEventType: "condition-started", ClearEventType: "condition-ended"}, all); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.InsertPropertySamples(f.ctx, storage.PushWriter(), []storage.PropertySampleWrite{
		{OwnerKind: "component", OwnerID: d1, Key: "video-input", Value: "hdmi1"}}); err != nil {
		t.Fatal(err)
	}
	conditions := func() int {
		return f.count(t, `select count(*) from event_rule_condition c join event_rule r on r.id = c.rule_id where r.name = 'legacy-clear'`)
	}
	if n := conditions(); n != 1 {
		t.Fatalf("the rule holds %d conditions, want 1 in force", n)
	}
	if _, err := f.conn.Exec(f.ctx, `update event_rule set clear_event_type_id = (select id from event_type where name = 'command-issued')
		where name = 'legacy-clear'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(f.ctx, `insert into command_type (name, description, settle_window_seconds, target_property_type_id, official)
		values ('legacy-forge', '', 30, (select id from property_type where name = 'endpoint-reachable'), false)`); err != nil {
		t.Fatal(err)
	}
	vs, err := f.gw.WriterRulingViolations(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for where, key := range map[string]string{"event rule legacy-clear": "command-issued", "command type legacy-forge": "endpoint-reachable"} {
		if !slices.ContainsFunc(vs, func(v storage.RulingViolation) bool { return v.Key == key && strings.Contains(v.Where, where) }) {
			t.Errorf("the boot audit does not flag %s on %s: %+v", key, where, vs)
		}
	}
	if err := f.gw.DeleteEventRule(f.ctx, f.actor, "legacy-clear", all); err != nil {
		t.Fatalf("delete the legacy rule: %v", err)
	}
	if n := conditions(); n != 0 {
		t.Errorf("%d conditions outlive the rule's delete", n)
	}
	if n := f.count(t, `select count(*) from event where event_type_id = (select id from event_type where name = 'command-issued')`); n != 0 {
		t.Errorf("the rule's clear wrote %d command-issued events", n)
	}
}
