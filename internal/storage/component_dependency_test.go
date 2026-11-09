package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// depFixture is a seeded gateway with an owner to issue commands as and one
// node to place endpoints on.
type depFixture struct {
	gw    *storage.PG
	dsn   string
	actor string
	node  string
}

func newDepFixture(t *testing.T) *depFixture {
	t.Helper()
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
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-dep"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	_, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	return &depFixture{gw: gw, dsn: dsn, actor: actor, node: "edge-dep"}
}

func (f *depFixture) component(t *testing.T, name string) {
	t.Helper()
	all := scope.Set{All: true}
	if _, err := f.gw.CreateComponent(context.Background(), "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
		t.Fatalf("create component %s: %v", name, err)
	}
}

// endpoint places a bare endpoint of the given transport on the fixture's node.
func (f *depFixture) endpoint(t *testing.T, comp, transport string, oob bool) string {
	t.Helper()
	node := f.node
	it, err := f.gw.CreateEndpoint(context.Background(), "", storage.EndpointSpec{
		Transport: transport, Component: &comp, Node: &node, OutOfBand: oob,
	}, scope.Set{All: true})
	if err != nil {
		t.Fatalf("create %s endpoint on %s: %v", transport, comp, err)
	}
	return it.ID
}

// attach attaches a driver to comp on the fixture's node, the endpoint a
// command is delivered through.
func (f *depFixture) attach(t *testing.T, comp, drv string) string {
	t.Helper()
	node := f.node
	it, err := f.gw.CreateEndpoint(context.Background(), "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &node, Inputs: map[string]string{"host": "10.20.9.10"},
	}, scope.Set{All: true})
	if err != nil {
		t.Fatalf("attach %s to %s: %v", drv, comp, err)
	}
	return it.ID
}

func (f *depFixture) depend(t *testing.T, comp, upstream, kind, outlet string) {
	t.Helper()
	all := scope.Set{All: true}
	if _, err := f.gw.CreateComponentDependency(context.Background(), f.actor, comp, storage.ComponentDependencySpec{
		Upstream: upstream, Kind: kind, Outlet: outlet,
	}, all, all); err != nil {
		t.Fatalf("depend %s on %s (%s): %v", comp, upstream, kind, err)
	}
}

func (f *depFixture) delivery(t *testing.T, commandType string) storage.CommandDelivery {
	t.Helper()
	got, err := f.gw.PendingNodeCommands(context.Background(), f.node)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	for _, d := range got {
		if d.CommandType == commandType {
			return d
		}
	}
	t.Fatalf("no %s delivery in %+v", commandType, got)
	return storage.CommandDelivery{}
}

func sorted(ids ...string) []string {
	slices.Sort(ids)
	return ids
}

// TestComponentDependencyHostWidensRebootHold pins #1648's host edge end to
// end: a reboot of a codec holds its own in-band endpoints and the in-band
// endpoints of the touch panel it hosts, and leaves both out-of-band
// endpoints (the BMCs, which answer through a reboot) and a contained child
// with no edge alone. Containment is not dependency.
func TestComponentDependencyHostWidensRebootHold(t *testing.T) {
	t.Parallel()
	f := newDepFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	f.component(t, "codec-d")
	f.component(t, "panel-d")
	parent := "codec-d"
	if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "card-d", ParentName: &parent}, all, all, all, all); err != nil {
		t.Fatalf("create contained card: %v", err)
	}
	if _, err := f.gw.CreateDriver(ctx, "", storage.Driver{Name: "reboot-proto", Label: "Reboot Proto", Version: "1.0.0", Spec: []byte(rebootProtoSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	codecAPI := f.attach(t, "codec-d", "reboot-proto")
	f.endpoint(t, "codec-d", "http", true) // the codec's BMC
	panelAPI := f.endpoint(t, "panel-d", "icmp", false)
	f.endpoint(t, "panel-d", "http", true)
	f.endpoint(t, "card-d", "icmp", false)
	f.depend(t, "panel-d", "codec-d", storage.DependencyHost, "")

	if _, err := f.gw.IssueCommand(ctx, f.actor, "component", "codec-d", "reboot", "", nil, nil); err != nil {
		t.Fatalf("issue reboot: %v", err)
	}
	d := f.delivery(t, "reboot")
	if want := sorted(codecAPI, panelAPI); !slices.Equal(d.HoldEndpoints, want) {
		t.Fatalf("reboot hold set = %v, want the codec's and the hosted panel's in-band endpoints %v", d.HoldEndpoints, want)
	}
}

// outletProtoSpec is a PDU's driver: one binding for the outlet-cycle power
// command, whose params name the outlet.
const outletProtoSpec = `{
	"version": 1,
	"transport": "tcp",
	"inputs": [{"name": "host", "kind": "string", "required": true}],
	"commands": [{"command_type": "outlet-cycle", "request": {"line": "CYCLE"}}]
}`

// TestComponentDependencyPowerCommandHoldsFedComponent pins the power edge:
// a command type marked with power_outlet_param, issued on a PDU with the
// outlet in its params, holds every endpoint of the component fed from that
// outlet (its BMC loses power too), never the PDU's own and never a
// component on another outlet.
func TestComponentDependencyPowerCommandHoldsFedComponent(t *testing.T) {
	t.Parallel()
	f := newDepFixture(t)
	ctx := context.Background()
	if _, err := f.gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
		Name: "outlet-cycle", Label: "Cycle an outlet",
		ParamsSchema: []byte(`{"type":"object","properties":{"outlet":{"type":"string"}},"required":["outlet"]}`),
		Disruptive:   true, QuietSeconds: 10, HoldSeconds: 120, PowerOutletParam: "outlet",
	}); err != nil {
		t.Fatalf("create power command type: %v", err)
	}
	if _, err := f.gw.CreateDriver(ctx, "", storage.Driver{Name: "outlet-proto", Label: "Outlet Proto", Version: "1.0.0", Spec: []byte(outletProtoSpec)}); err != nil {
		t.Fatalf("create driver: %v", err)
	}
	for _, c := range []string{"pdu-d", "amp-d", "dsp-d"} {
		f.component(t, c)
	}
	f.attach(t, "pdu-d", "outlet-proto")
	ampAPI := f.endpoint(t, "amp-d", "icmp", false)
	ampBMC := f.endpoint(t, "amp-d", "http", true)
	f.endpoint(t, "dsp-d", "icmp", false)
	f.depend(t, "amp-d", "pdu-d", storage.DependencyPower, "2")
	f.depend(t, "dsp-d", "pdu-d", storage.DependencyPower, "3")

	if _, err := f.gw.IssueCommand(ctx, f.actor, "component", "pdu-d", "outlet-cycle", "", nil, json.RawMessage(`{"outlet":"2"}`)); err != nil {
		t.Fatalf("issue outlet-cycle: %v", err)
	}
	d := f.delivery(t, "outlet-cycle")
	if !d.Disruptive || d.QuietSeconds != 10 || d.HoldSeconds != 120 {
		t.Errorf("delivery = %+v, want disruptive with quiet 10 and hold 120", d)
	}
	if want := sorted(ampAPI, ampBMC); !slices.Equal(d.HoldEndpoints, want) {
		t.Fatalf("power hold set = %v, want every endpoint of the amp on outlet 2 %v", d.HoldEndpoints, want)
	}
}

// TestComponentDependencyPowerOutletParamNeedsDisruptive pins the marker's
// rule: only a disruptive type is a power command.
func TestComponentDependencyPowerOutletParamNeedsDisruptive(t *testing.T) {
	t.Parallel()
	f := newDepFixture(t)
	_, err := f.gw.CreateCommandType(context.Background(), "", storage.CommandTypeSpec{
		Name: "outlet-label", Label: "Label an outlet", PowerOutletParam: "outlet",
	})
	if !errors.Is(err, storage.ErrCommandTypeInvalid) {
		t.Fatalf("create = %v, want ErrCommandTypeInvalid", err)
	}
}

// TestComponentDependencyWriteRules pins the gateway's refusals and the
// edge's lifecycle: a self edge, a power edge with no outlet and a host edge
// with one are invalid; a duplicate is exists; an edge closing a cycle in
// its kind is refused; the list names both directions; a delete is audited
// and a second delete is not found; deleting a component deletes its edges.
func TestComponentDependencyWriteRules(t *testing.T) {
	t.Parallel()
	f := newDepFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	for _, c := range []string{"rack-w", "chassis-w", "card-w", "pdu-w"} {
		f.component(t, c)
	}
	create := func(comp, upstream, kind, outlet string) error {
		_, err := f.gw.CreateComponentDependency(ctx, f.actor, comp, storage.ComponentDependencySpec{Upstream: upstream, Kind: kind, Outlet: outlet}, all, all)
		return err
	}
	for _, c := range []struct {
		name                   string
		comp, up, kind, outlet string
		want                   error
	}{
		{"self", "card-w", "card-w", "host", "", storage.ErrDependencyInvalid},
		{"power without outlet", "card-w", "pdu-w", "power", "", storage.ErrDependencyInvalid},
		{"host with outlet", "card-w", "chassis-w", "host", "4", storage.ErrDependencyInvalid},
		{"unknown kind", "card-w", "chassis-w", "contains", "", storage.ErrDependencyInvalid},
		{"no such upstream", "card-w", "ghost-w", "host", "", storage.ErrDependencyUpstreamNotFound},
		{"no such component", "ghost-w", "card-w", "host", "", storage.ErrComponentNotFound},
	} {
		if err := create(c.comp, c.up, c.kind, c.outlet); !errors.Is(err, c.want) {
			t.Errorf("%s: create = %v, want %v", c.name, err, c.want)
		}
	}

	f.depend(t, "card-w", "chassis-w", storage.DependencyHost, "")
	f.depend(t, "chassis-w", "rack-w", storage.DependencyHost, "")
	f.depend(t, "chassis-w", "pdu-w", storage.DependencyPower, "1")
	if err := create("card-w", "chassis-w", "host", ""); !errors.Is(err, storage.ErrDependencyExists) {
		t.Errorf("duplicate: create = %v, want ErrDependencyExists", err)
	}
	if err := create("rack-w", "card-w", "host", ""); !errors.Is(err, storage.ErrDependencyCycle) {
		t.Errorf("cycle: create = %v, want ErrDependencyCycle", err)
	}
	// Another kind is another graph: a path edge back up is not a host cycle.
	if err := create("rack-w", "card-w", "path", ""); err != nil {
		t.Errorf("path edge across a host chain: %v", err)
	}

	list, err := f.gw.ListComponentDependencies(ctx, "chassis-w", all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var ups, downs []string
	for _, e := range list.Upstreams {
		ups = append(ups, e.Kind+":"+e.UpstreamName+":"+e.Outlet)
	}
	for _, e := range list.Dependents {
		downs = append(downs, e.Kind+":"+e.ComponentName)
	}
	slices.Sort(ups)
	if want := []string{"host:rack-w:", "power:pdu-w:1"}; !slices.Equal(ups, want) {
		t.Errorf("chassis upstreams = %v, want %v", ups, want)
	}
	if want := []string{"host:card-w"}; !slices.Equal(downs, want) {
		t.Errorf("chassis dependents = %v, want %v", downs, want)
	}

	if err := f.gw.DeleteComponentDependency(ctx, f.actor, "chassis-w", "power", "pdu-w", all, all); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := f.gw.DeleteComponentDependency(ctx, f.actor, "chassis-w", "power", "pdu-w", all, all); !errors.Is(err, storage.ErrDependencyNotFound) {
		t.Errorf("second delete = %v, want ErrDependencyNotFound", err)
	}
	conn, err := pgx.Connect(ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var audits []string
	rows, err := conn.Query(ctx, `select verb from audit_log where resource = 'component_dependency' order by ts, id`)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		audits = append(audits, v)
	}
	if strings.Join(audits, ",") != "create,create,create,create,delete" {
		t.Errorf("audit verbs = %v, want four creates then one delete", audits)
	}

	if err := f.gw.DeleteComponent(ctx, "", "card-w", all, all); err != nil {
		t.Fatalf("delete card: %v", err)
	}
	list, err = f.gw.ListComponentDependencies(ctx, "chassis-w", all)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list.Dependents) != 0 {
		t.Errorf("chassis dependents after the card's delete = %+v, want none: the edge goes with its end", list.Dependents)
	}
}

// TestComponentDependencyUpstreamOutOfScope pins the scope seam: an upstream
// outside the caller's read is the non-disclosing upstream not-found, and a
// dependent the caller reads but may not update is forbidden.
func TestComponentDependencyUpstreamOutOfScope(t *testing.T) {
	t.Parallel()
	f := newDepFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	f.component(t, "near-s")
	f.component(t, "far-s")
	comps, err := f.gw.ListComponents(ctx, all)
	if err != nil {
		t.Fatalf("list components: %v", err)
	}
	var nearID string
	for _, c := range comps {
		if c.Name == "near-s" {
			nearID = c.ID
		}
	}
	nearOnly := scope.Set{IDs: []string{nearID}}
	_, err = f.gw.CreateComponentDependency(ctx, f.actor, "near-s", storage.ComponentDependencySpec{Upstream: "far-s", Kind: "host"}, nearOnly, nearOnly)
	if !errors.Is(err, storage.ErrDependencyUpstreamNotFound) {
		t.Errorf("out-of-scope upstream: create = %v, want ErrDependencyUpstreamNotFound", err)
	}
	_, err = f.gw.CreateComponentDependency(ctx, f.actor, "near-s", storage.ComponentDependencySpec{Upstream: "far-s", Kind: "host"}, all, scope.Set{})
	if !errors.Is(err, storage.ErrComponentForbidden) {
		t.Errorf("no update on the dependent: create = %v, want ErrComponentForbidden", err)
	}
}
