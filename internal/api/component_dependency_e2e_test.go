package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Component dependencies over HTTP (#1648): an edge says a component depends
// on another, by power (fed from one of its outlets), host or path. The edge
// is a fact about the dependent, so a write costs component:update on it and
// only a read on the upstream. Each test drives the routes as an operator
// would.

type dependencyWire struct {
	Component   string `json:"component"`
	ComponentID string `json:"component_id"`
	Upstream    string `json:"upstream"`
	UpstreamID  string `json:"upstream_id"`
	Kind        string `json:"kind"`
	Outlet      string `json:"outlet"`
}

type dependencyListWire struct {
	Component  string           `json:"component"`
	Upstreams  []dependencyWire `json:"upstreams"`
	Dependents []dependencyWire `json:"dependents"`
	Hidden     int              `json:"hidden"`
}

type dependencyE2E struct {
	*staffingFixture
	ids map[string]string
}

// newDependencyE2E is a PDU, a codec on its outlet, a touch panel the codec
// hosts, and a role that reads and writes components, inserted before the
// first request builds the role index.
func newDependencyE2E(t *testing.T) *dependencyE2E {
	f := &dependencyE2E{staffingFixture: newStaffingFixture(t), ids: map[string]string{}}
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(f.ctx, `insert into role (name, official, permissions, inherits) values ('dependency-editor', false, '{"component:read,update"}', '{}')`); err != nil {
		t.Fatalf("insert role: %v", err)
	}
	conn.Close(f.ctx)
	for _, c := range []string{"pdu-1", "codec-1", "panel-1"} {
		f.component(c, f.display)
		f.ids[c] = entityID(t, f.c, f.owner, "/components", c)
	}
	return f
}

func depBody(upstream, kind, outlet string) map[string]any {
	b := map[string]any{"upstream": upstream, "kind": kind}
	if outlet != "" {
		b["outlet"] = outlet
	}
	return b
}

func (f *dependencyE2E) list(tok, component string) dependencyListWire {
	f.t.Helper()
	var w dependencyListWire
	if err := json.Unmarshal(f.c.do(tok, http.MethodGet, "/components/"+component+"/dependencies", nil, http.StatusOK), &w); err != nil {
		f.t.Fatalf("decode dependencies: %v", err)
	}
	return w
}

// An edge round-trips naming both ends, shows on both ends' lists, and a
// delete by kind and upstream removes it once.
func TestComponentDependencyRoundTripsOverHTTP(t *testing.T) {
	t.Parallel()
	f := newDependencyE2E(t)
	var d dependencyWire
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/components/codec-1/dependencies", depBody("pdu-1", "power", "3"), http.StatusCreated), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Component != "codec-1" || d.ComponentID != f.ids["codec-1"] || d.Upstream != "pdu-1" || d.UpstreamID != f.ids["pdu-1"] ||
		d.Kind != "power" || d.Outlet != "3" {
		t.Fatalf("create = %+v, want codec-1 on pdu-1 outlet 3", d)
	}
	f.c.do(f.owner, http.MethodPost, "/components/panel-1/dependencies", depBody(f.ids["codec-1"], "host", ""), http.StatusCreated)

	codec := f.list(f.owner, "codec-1")
	if len(codec.Upstreams) != 1 || codec.Upstreams[0].Upstream != "pdu-1" ||
		len(codec.Dependents) != 1 || codec.Dependents[0].Component != "panel-1" || codec.Hidden != 0 {
		t.Fatalf("codec list = %+v, want pdu-1 upstream and panel-1 dependent", codec)
	}
	f.c.do(f.owner, http.MethodDelete, "/components/codec-1/dependencies/power/pdu-1", nil, http.StatusNoContent)
	f.c.do(f.owner, http.MethodDelete, "/components/codec-1/dependencies/power/pdu-1", nil, http.StatusNotFound)
	if l := f.list(f.owner, "pdu-1"); len(l.Dependents) != 0 {
		t.Fatalf("pdu list after delete = %+v, want no dependents", l)
	}
}

// The edge's shape is checked: a power edge names its outlet, no other kind
// does, nothing depends on itself, an edge is recorded once, and an edge that
// would close a cycle within its kind is refused.
func TestComponentDependencyRefusesBadEdgesOverHTTP(t *testing.T) {
	t.Parallel()
	f := newDependencyE2E(t)
	post := func(component string, body map[string]any, want int) string {
		t.Helper()
		st, raw := f.c.send(f.owner, http.MethodPost, "/components/"+component+"/dependencies", body)
		if st != want {
			t.Fatalf("post %s %v = %d %s, want %d", component, body, st, raw, want)
		}
		return string(raw)
	}
	if raw := post("codec-1", depBody("pdu-1", "power", ""), http.StatusUnprocessableEntity); !strings.Contains(raw, "outlet") {
		t.Errorf("power with no outlet = %s, want a 422 naming the outlet", raw)
	}
	post("codec-1", depBody("pdu-1", "host", "1"), http.StatusUnprocessableEntity)
	post("codec-1", depBody("pdu-1", "cooling", ""), http.StatusUnprocessableEntity)
	post("codec-1", depBody("codec-1", "host", ""), http.StatusUnprocessableEntity)
	post("codec-1", depBody("no-such", "host", ""), http.StatusNotFound)
	post("no-such", depBody("codec-1", "host", ""), http.StatusNotFound)

	post("panel-1", depBody("codec-1", "host", ""), http.StatusCreated)
	post("panel-1", depBody("codec-1", "host", ""), http.StatusConflict)
	if raw := post("codec-1", depBody("panel-1", "host", ""), http.StatusUnprocessableEntity); !strings.Contains(raw, "cycle") {
		t.Errorf("closing a host cycle = %s, want a 422 naming the cycle", raw)
	}
	// The same pair in the other direction under another kind is no cycle.
	post("codec-1", depBody("panel-1", "path", ""), http.StatusCreated)
}

// A write costs update on the dependent and read on the upstream: an upstream
// outside the caller's read is the non-disclosing 404, a dependent it reads
// but cannot write is a 403, and a list counts an edge whose other end it
// cannot read without naming it.
func TestComponentDependencyScopeOverHTTP(t *testing.T) {
	t.Parallel()
	f := newDependencyE2E(t)
	codecOnly := principalWithGrants(t, f.ctx, f.dsn, "codec-only",
		[]grant{{role: "dependency-editor", scopeKind: "component", scopeID: f.ids["codec-1"]}})
	readAll := principalWithGrants(t, f.ctx, f.dsn, "panel-editor-reads-all",
		[]grant{{role: "dependency-editor", scopeKind: "component", scopeID: f.ids["panel-1"]}, {role: "viewer", scopeKind: "all"}})

	if st, raw := f.c.send(codecOnly, http.MethodPost, "/components/codec-1/dependencies", depBody("pdu-1", "power", "1")); st != http.StatusNotFound {
		t.Errorf("upstream outside read = %d %s, want 404", st, raw)
	}
	if st, raw := f.c.send(readAll, http.MethodPost, "/components/codec-1/dependencies", depBody("pdu-1", "power", "1")); st != http.StatusForbidden {
		t.Errorf("dependent read but not writable = %d %s, want 403", st, raw)
	}
	f.c.do(readAll, http.MethodPost, "/components/panel-1/dependencies", depBody("codec-1", "host", ""), http.StatusCreated)
	f.c.do(f.owner, http.MethodPost, "/components/codec-1/dependencies", depBody("pdu-1", "power", "1"), http.StatusCreated)

	l := f.list(codecOnly, "codec-1")
	if len(l.Upstreams) != 0 || len(l.Dependents) != 0 || l.Hidden != 2 {
		t.Fatalf("scoped list = %+v, want both edges hidden and counted", l)
	}
	if st, _ := f.c.send(codecOnly, http.MethodDelete, "/components/codec-1/dependencies/power/pdu-1", nil); st != http.StatusNotFound {
		t.Errorf("delete naming an unreadable upstream = %d, want 404", st)
	}
}

// The two markers a hold set reads ride the API: an endpoint's out_of_band
// flag and a disruptive command type's power_outlet_param, which a type that
// is not disruptive cannot carry.
func TestDependencyMarkersOverHTTP(t *testing.T) {
	t.Parallel()
	f := newDependencyE2E(t)
	var ep struct {
		ID        string `json:"id"`
		OutOfBand bool   `json:"out_of_band"`
	}
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/endpoints", map[string]any{
		"transport": "http", "component": "codec-1", "out_of_band": true,
	}, http.StatusCreated), &ep); err != nil {
		t.Fatalf("decode endpoint: %v", err)
	}
	if !ep.OutOfBand {
		t.Fatalf("endpoint create = %+v, want out_of_band", ep)
	}
	json.Unmarshal(f.c.do(f.owner, http.MethodPatch, "/endpoints/"+ep.ID, map[string]any{"out_of_band": false}, http.StatusOK), &ep)
	if ep.OutOfBand {
		t.Fatalf("endpoint patch = %+v, want out_of_band cleared", ep)
	}
	json.Unmarshal(f.c.do(f.owner, http.MethodPatch, "/endpoints/"+ep.ID, map[string]any{"out_of_band": true}, http.StatusOK), &ep)
	if !ep.OutOfBand {
		t.Fatalf("endpoint patch = %+v, want out_of_band set again", ep)
	}

	var ct struct {
		PowerOutletParam string `json:"power_outlet_param"`
	}
	json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/command-types", map[string]any{
		"name": "outlet-cycle", "disruptive": true, "quiet_seconds": 10, "hold_seconds": 60, "power_outlet_param": "outlet",
	}, http.StatusCreated), &ct)
	if ct.PowerOutletParam != "outlet" {
		t.Fatalf("command type create = %+v, want power_outlet_param outlet", ct)
	}
	if raw := f.c.do(f.owner, http.MethodPost, "/command-types", map[string]any{
		"name": "outlet-set", "power_outlet_param": "outlet",
	}, http.StatusUnprocessableEntity); !strings.Contains(string(raw), "disruptive") {
		t.Fatalf("power marker on a non-disruptive type = %s, want a 422 naming disruptive", raw)
	}
	// The cleared marker is omitted on the wire, so decode into a fresh value.
	ct.PowerOutletParam = ""
	json.Unmarshal(f.c.do(f.owner, http.MethodPatch, "/command-types/outlet-cycle", map[string]any{"power_outlet_param": ""}, http.StatusOK), &ct)
	if ct.PowerOutletParam != "" {
		t.Fatalf("command type patch = %+v, want the marker cleared", ct)
	}
}
