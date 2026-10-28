package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// instRoles are the custom roles these tests grant, inserted before the
// server reads its role index (it reads it once, on the first request).
var instRoles = map[string][]string{
	"annex-places":         {"system:create,read,update", "component:create,update,read", "standard:create"},
	"annex-places-placed":  {"location:create,read"},
	"sys-only":             []string{"system:create,read,update", "component:update,read", "location:read", "standard:create"},
	"no-fork":              []string{"system:create,read,update", "component:create,update,read", "location:read"},
	"loc-only":             []string{"location:create,read"},
	"annex-builder":        []string{"component:create,read"},
	"annex-builder-placed": []string{"location:read"},
	"annex-placer":         []string{"component:read", "location:read"},
	"annex-placer-placed":  []string{"component:create"},
	"annex-comp":           []string{"system:create,read,update", "location:read", "standard:create"},
	"annex-comp-placed":    []string{"component:create,update,read"},
	"annex-reader":         []string{"system:create,read,update", "component:create,update,read"},
	"annex-reader-placed":  []string{"location:read"},
}

type instFixture struct {
	t     *testing.T
	ctx   context.Context
	dsn   string
	gw    storage.Gateway
	c     *apiClient
	owner string
	conn  *pgx.Conn
}

func newInstFixture(t *testing.T) *instFixture {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	owner := bootstrapOwnerTok(t, ctx, gw)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	for name, perms := range instRoles {
		if _, err := conn.Exec(ctx, `insert into role (name, official, permissions, inherits) values ($1, false, $2, '{}')`, name, perms); err != nil {
			t.Fatalf("role %s: %v", name, err)
		}
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	t.Cleanup(srv.Close)
	f := &instFixture{t: t, ctx: ctx, dsn: dsn, gw: gw, c: &apiClient{t: t, ctx: ctx, base: srv.URL}, owner: owner, conn: conn}
	all := scope.Set{All: true}
	for _, l := range []storage.LocationSpec{
		{Name: "hq", LocationType: "building"},
		{Name: "annex", LocationType: "building"},
	} {
		if _, err := gw.CreateLocation(ctx, "", l, all); err != nil {
			t.Fatalf("create %s: %v", l.Name, err)
		}
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-1"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	return f
}

func (f *instFixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.conn.QueryRow(f.ctx, q, args...).Scan(&n); err != nil {
		f.t.Fatalf("count: %v", err)
	}
	return n
}

// principal makes a principal holding the custom role label (instRoles) at
// the grant given, returning its token.
func (f *instFixture) principal(label string, g grant) string {
	f.t.Helper()
	g.role = label
	return principalWithGrants(f.t, f.ctx, f.dsn, label, []grant{g})
}

// split makes a principal holding role label fleet-wide and role
// label-placed only under the place given.
func (f *instFixture) split(label, place string) string {
	f.t.Helper()
	tok := f.principal(label, grant{scopeKind: "all"})
	if _, err := f.conn.Exec(f.ctx, `insert into principal_grant (principal_id, role_id, scope_kind, scope_id, scope_op)
		select principal_id, (select id from role where name = $1), 'location', $2, 'subtree' from principal_grant
		where role_id = (select id from role where name = $3) limit 1`, label+"-placed", f.locationID(place), label); err != nil {
		f.t.Fatal(err)
	}
	return tok
}

func (f *instFixture) locationID(name string) string {
	f.t.Helper()
	var id string
	if err := f.conn.QueryRow(f.ctx, `select id from location where name = $1 and parent_id is null`, name).Scan(&id); err != nil {
		f.t.Fatalf("location %s: %v", name, err)
	}
	return id
}

func TestCreateComponentFromTemplateAPI(t *testing.T) {
	f := newInstFixture(t)
	type out struct {
		Template  string `json:"template"`
		Component string `json:"component"`
		Product   string `json:"product"`
		Attached  bool   `json:"attached"`
	}
	var got out
	// Without an address it lands unattached, and its audit row names the template.
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/components:createFromTemplate",
		map[string]any{"template": "newtron-panel-7", "location": "hq"}, http.StatusCreated), &got); err != nil {
		t.Fatal(err)
	}
	if got.Attached || got.Product != "newtron-panel-7" || got.Component == "" {
		t.Fatalf("unattached instantiate = %+v", got)
	}
	if n := f.count(`select count(*) from audit_log where verb = 'create' and resource = 'component' and new->>'from_template' = 'newtron-panel-7'`); n != 1 {
		t.Fatalf("component create audit rows naming the template = %d, want 1", n)
	}

	// With an address and a node it attaches through the driver, its preset under the inputs.
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/components:createFromTemplate",
		map[string]any{"template": "newtron-panel-7", "location": "hq", "node": "edge-1", "inputs": map[string]string{"host": "10.20.0.7"}}, http.StatusCreated), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Attached {
		t.Fatalf("attached instantiate = %+v", got)
	}
	var inputs []byte
	if err := f.conn.QueryRow(f.ctx, `select e.inputs from endpoint e join component c on c.id = e.component where c.name = $1 and c.location_id = $2`,
		got.Component, f.locationID("hq")).Scan(&inputs); err != nil {
		t.Fatalf("read the attached endpoint: %v", err)
	}
	if !strings.Contains(string(inputs), `"host": "10.20.0.7"`) || !strings.Contains(string(inputs), `"port": "51325"`) {
		t.Fatalf("endpoint inputs = %s, want the address over the preset", inputs)
	}

	// A secret reference the caller cannot use is refused as attach refuses it, and nothing lands.
	before := f.count(`select count(*) from component`)
	detail := string(f.c.do(f.owner, http.MethodPost, "/components:createFromTemplate",
		map[string]any{"template": "boreal-edge-55", "location": "hq", "node": "edge-1", "inputs": map[string]string{"host": "10.20.0.8", "community": "nobody-has-this"}}, http.StatusUnprocessableEntity))
	if !strings.Contains(detail, "does not exist or is not yours to use") {
		t.Fatalf("secret refusal = %s", detail)
	}
	// Inputs with no address, and an attach with no node, are 422s.
	f.c.do(f.owner, http.MethodPost, "/components:createFromTemplate", map[string]any{"template": "newtron-panel-7", "location": "hq", "inputs": map[string]string{"port": "1"}}, http.StatusUnprocessableEntity)
	f.c.do(f.owner, http.MethodPost, "/components:createFromTemplate", map[string]any{"template": "newtron-panel-7", "location": "hq", "inputs": map[string]string{"host": "10.20.0.9"}}, http.StatusUnprocessableEntity)
	f.c.do(f.owner, http.MethodPost, "/components:createFromTemplate", map[string]any{"template": "mr55-system", "location": "hq"}, http.StatusNotFound)
	if n := f.count(`select count(*) from component`); n != before {
		t.Fatalf("refused instantiates left %d components", n-before)
	}

	// Scope: a caller whose component:create reaches only annex is refused at hq,
	// and a caller who cannot read hq gets a non-disclosing 404.
	annexReader := f.split("annex-builder", "annex")
	f.c.do(annexReader, http.MethodPost, "/components:createFromTemplate", map[string]any{"template": "newtron-panel-7", "location": "hq"}, http.StatusNotFound)
	annexBuilder := f.split("annex-placer", "annex")
	f.c.do(annexBuilder, http.MethodPost, "/components:createFromTemplate", map[string]any{"template": "newtron-panel-7", "location": "hq"}, http.StatusForbidden)
	if n := f.count(`select count(*) from component`); n != before {
		t.Fatalf("scope-refused instantiates left %d components", n-before)
	}
	viewer := principalWithGrants(t, f.ctx, f.dsn, "inst-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	f.c.do(viewer, http.MethodPost, "/components:createFromTemplate", map[string]any{"template": "newtron-panel-7", "location": "hq"}, http.StatusForbidden)
}

func TestCreateSystemFromTemplateAPI(t *testing.T) {
	f := newInstFixture(t)
	type comp struct {
		Component string `json:"component"`
		Role      string `json:"role"`
		Attached  bool   `json:"attached"`
	}
	type out struct {
		System           string   `json:"system"`
		Standard         string   `json:"standard"`
		StandardsCreated []string `json:"standards_created"`
		Components       []comp   `json:"components"`
		ToAttach         []string `json:"to_attach"`
	}

	// A caller lacking component:create is refused, and no system, standard or component lands.
	noComp := f.principal("sys-only", grant{scopeKind: "all"})
	f.c.do(noComp, http.MethodPost, "/systems:createFromTemplate", map[string]any{"template": "mr55-system", "location": "hq"}, http.StatusForbidden)
	// One whose component:create reaches only annex gets past the fork and the
	// system, is refused at the first component, and still nothing lands.
	annexComp := f.split("annex-comp", "annex")
	f.c.do(annexComp, http.MethodPost, "/systems:createFromTemplate", map[string]any{"template": "mr55-system", "location": "hq"}, http.StatusForbidden)
	if n := f.count(`select count(*) from system`) + f.count(`select count(*) from standard`) + f.count(`select count(*) from component`); n != 0 {
		t.Fatalf("a refused instantiate left %d rows", n)
	}

	// The owner lands the whole system on a fresh fleet: the standard and its
	// parent forked, a component per role, one attached.
	var got out
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/systems:createFromTemplate", map[string]any{
		"template": "mr55-system", "location": "hq", "node": "edge-1",
		"attach": map[string][]map[string]string{"touch-control": {{"host": "10.30.0.5"}}},
	}, http.StatusCreated), &got); err != nil {
		t.Fatal(err)
	}
	if got.Standard != "mr55" || strings.Join(got.StandardsCreated, ",") != "meeting-room,mr55" {
		t.Fatalf("standard = %s forked %v, want mr55 forked with its parent", got.Standard, got.StandardsCreated)
	}
	if len(got.Components) != 4 || len(got.ToAttach) != 3 {
		t.Fatalf("components = %+v to_attach %v, want 4 with 3 to attach", got.Components, got.ToAttach)
	}
	var roles struct {
		Roles []struct {
			Name         string `json:"name"`
			Understaffed int    `json:"understaffed"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodGet, "/systems/"+got.System+"/roles", nil, http.StatusOK), &roles); err != nil {
		t.Fatal(err)
	}
	for _, r := range roles.Roles {
		if r.Understaffed != 0 {
			t.Errorf("role %s understaffed %d, want the system staffed complete", r.Name, r.Understaffed)
		}
	}
	if n := f.count(`select count(*) from audit_log where new->>'from_template' = 'mr55-system'`); n < 10 {
		t.Fatalf("audit rows naming the template = %d, want every piece", n)
	}

	// A second instantiate builds to the held standard: no fork.
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/systems:createFromTemplate", map[string]any{"template": "mr55-system", "location": "annex"}, http.StatusCreated), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.StandardsCreated) != 0 {
		t.Fatalf("second instantiate forked %v, want none", got.StandardsCreated)
	}

	// A caller who cannot fork, on a template whose standard is absent, is refused and nothing lands.
	noFork := f.principal("no-fork", grant{scopeKind: "all"})
	before := f.count(`select count(*) from system`)
	f.c.do(noFork, http.MethodPost, "/systems:createFromTemplate", map[string]any{"template": "ds55-system", "location": "hq"}, http.StatusForbidden)
	if n := f.count(`select count(*) from system`); n != before {
		t.Fatal("a refused fork left a system")
	}
	// A place the caller cannot read is a 404; an unknown template or standard a 404.
	hqOnly := f.split("annex-reader", "annex")
	f.c.do(hqOnly, http.MethodPost, "/systems:createFromTemplate", map[string]any{"template": "mr55-system", "location": "hq"}, http.StatusNotFound)
	f.c.do(f.owner, http.MethodPost, "/systems:createFromTemplate", map[string]any{"template": "nope", "location": "hq"}, http.StatusNotFound)
	f.c.do(f.owner, http.MethodPost, "/systems:createFromTemplate", map[string]any{"template": "mr55-system", "location": "hq", "standard": "nope"}, http.StatusNotFound)
	viewer := principalWithGrants(t, f.ctx, f.dsn, "sys-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	f.c.do(viewer, http.MethodPost, "/systems:createFromTemplate", map[string]any{"template": "mr55-system", "location": "hq"}, http.StatusForbidden)
}

func TestCreateLocationsFromTemplateAPI(t *testing.T) {
	f := newInstFixture(t)
	type out struct {
		Places []struct {
			Location string `json:"location"`
			Type     string `json:"type"`
			Parent   string `json:"parent"`
		} `json:"places"`
		Systems []struct {
			System   string `json:"system"`
			Location string `json:"location"`
		} `json:"systems"`
		ToAttach []string `json:"to_attach"`
	}
	var got out
	if err := json.Unmarshal(f.c.do(f.owner, http.MethodPost, "/locations:createFromTemplate",
		map[string]any{"template": "meeting-floor", "parent": "hq", "name": "f3", "label": "Third floor"}, http.StatusCreated), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Places) != 3 || got.Places[0].Location != "f3" || got.Places[0].Parent != "hq" || got.Places[1].Parent != "f3" {
		t.Fatalf("places = %+v, want f3 under hq with two rooms under it", got.Places)
	}
	if len(got.Systems) != 3 || len(got.ToAttach) != 10 {
		t.Fatalf("systems = %+v to_attach %d, want three systems, every component to attach", got.Systems, len(got.ToAttach))
	}

	// The root's type must allow the parent's: a floor under a room is refused, naming the pair.
	room := got.Places[1].Location
	detail := string(f.c.do(f.owner, http.MethodPost, "/locations:createFromTemplate",
		map[string]any{"template": "meeting-floor", "parent": "hq.f3." + room, "name": "f9"}, http.StatusUnprocessableEntity))
	if !strings.Contains(detail, "floor is not allowed under room") {
		t.Fatalf("placement refusal = %s", detail)
	}
	before := f.count(`select count(*) from location`)
	// An operator who forks floor to allow it under a room is checked against their fork.
	f.c.do(f.owner, http.MethodPatch, "/location-types/floor", map[string]any{"allowed_parent_types": []string{"building", "campus", "room"}}, http.StatusOK)
	f.c.do(f.owner, http.MethodPost, "/locations:createFromTemplate",
		map[string]any{"template": "meeting-floor", "parent": "hq.f3." + room, "name": "f9"}, http.StatusCreated)
	if n := f.count(`select count(*) from location`); n != before+3 {
		t.Fatalf("after the fork the subtree landed %d places, want 3", n-before)
	}
	// A parent the caller cannot read is a 404 whether it exists or not.
	annexPlacer := f.split("annex-places", "annex")
	f.c.do(annexPlacer, http.MethodPost, "/locations:createFromTemplate", map[string]any{"template": "meeting-floor", "parent": "hq", "name": "f1"}, http.StatusNotFound)
	f.c.do(annexPlacer, http.MethodPost, "/locations:createFromTemplate", map[string]any{"template": "meeting-floor", "parent": "nowhere", "name": "f1"}, http.StatusNotFound)
	// The root needs a name: no shipped type mints one.
	f.c.do(f.owner, http.MethodPost, "/locations:createFromTemplate", map[string]any{"template": "meeting-floor", "parent": "annex"}, http.StatusUnprocessableEntity)
	// A location-only caller cannot land the systems the subtree carries.
	locOnly := f.principal("loc-only", grant{scopeKind: "all"})
	f.c.do(locOnly, http.MethodPost, "/locations:createFromTemplate", map[string]any{"template": "meeting-floor", "parent": "annex", "name": "f1"}, http.StatusForbidden)
}
