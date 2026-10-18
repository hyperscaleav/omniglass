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
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type systemPlanBody struct {
	Standard string `json:"standard"`
	Roles    []struct {
		Role    string `json:"role"`
		Product string `json:"product"`
		Count   int    `json:"count"`
		Missing int    `json:"missing"`
		Reason  string `json:"reason"`
	} `json:"roles"`
	Components int `json:"components"`
}

// TestSimulatedSystemAPI drives adding a whole simulated system to a
// standard: the plan previews each role staffed with a simulated product
// (or left short, saying why), and adding it creates the system and its
// components, each at its own free address on the node, as one write.
// Adding needs simulation:update and the creates it makes.
func TestSimulatedSystemAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	for _, n := range []string{"site-a", "site-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "loc-s", LocationType: "campus"}, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	// A builder who may create components and endpoints but not systems.
	if _, err := conn.Exec(ctx, `insert into role (name, permissions) values ('sim-builder', '{"simulation:update","simulation:read","component:create","endpoint:create","system:update"}')`); err != nil {
		t.Fatal(err)
	}
	builderTok, bhash, bprefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	var pid string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`insert into service (principal_id, name) values ($1, 'sim-builder')`, []any{pid}},
		{`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, []any{pid, bhash, bprefix}},
		{`insert into principal_grant (principal_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'sim-builder'), 'all', null)`, []any{pid}},
	} {
		if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}

	// A builder whose component and endpoint creates reach only loc-s: the
	// system's components are created without a parent, which needs an
	// all-scope component:create, so the add is refused as a 403 naming
	// the component tier, not a 500.
	if _, err := conn.Exec(ctx, `insert into role (name, permissions) values
		('sim-sys-all', '{"system:create","system:update","system:read","location:read","node:read","simulation:read"}'),
		('sim-sys-loc', '{"component:create","endpoint:create","simulation:update"}'),
		('sim-sys-noread', '{"simulation:update","system:create","system:update","component:create","endpoint:create","node:read"}')`); err != nil {
		t.Fatal(err)
	}
	locTok, lhash, lprefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	var lpid string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('service') returning id`).Scan(&lpid); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`insert into service (principal_id, name) values ($1, 'sim-sys-loc')`, []any{lpid}},
		{`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, []any{lpid, lhash, lprefix}},
		{`insert into principal_grant (principal_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'sim-sys-all'), 'all', null)`, []any{lpid}},
		{`insert into principal_grant (principal_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'sim-sys-loc'), 'location', (select id::text from location where name = 'loc-s'))`, []any{lpid}},
	} {
		if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}

	noReadTok, nhash, nprefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	var npid string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('service') returning id`).Scan(&npid); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`insert into service (principal_id, name) values ($1, 'sim-sys-noread')`,
		`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`,
		`insert into principal_grant (principal_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'sim-sys-noread'), 'all', null)`,
	} {
		args := []any{npid}
		if strings.Contains(q, "credential") {
			args = append(args, nhash, nprefix)
		}
		if _, err := conn.Exec(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	hub := &fakeHub{Registry: simhub.New()}
	hub.Pulled("site-a", collection.SimCapabilities{PortOffset: 10000}, nil)
	srv := httptest.NewServer(api.NewHandler(gw, api.WithSimHub(hub)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	viewer := setupAllViewer(t, ctx, dsn, "simsystem-viewer")
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}

	// The plan: every role of the standard, staffed with a product the
	// simulator serves, or short with a reason.
	var plan systemPlanBody
	if err := json.Unmarshal(c.do(viewer, http.MethodGet, "/simulation/standards/meeting-room:plan", nil, http.StatusOK), &plan); err != nil {
		t.Fatal(err)
	}
	roles, err := gw.ListSystemRoles(ctx, "standard", "meeting-room")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Standard != "meeting-room" || len(plan.Roles) != len(roles) {
		t.Fatalf("plan = %+v, want every one of the standard's %d roles", plan, len(roles))
	}
	staffed, sum := 0, 0
	for _, r := range plan.Roles {
		sum += r.Count
		if r.Count > 0 {
			staffed++
			if _, _, ok := store.ForProduct(r.Product); !ok {
				t.Errorf("role %s staffed with %s, which nothing simulates", r.Role, r.Product)
			}
		} else if r.Reason == "" || r.Missing == 0 {
			t.Errorf("role %s unstaffed with no reason: %+v", r.Role, r)
		}
	}
	if staffed < 3 || plan.Components != sum {
		t.Fatalf("plan staffs %d roles and %d components (says %d), want at least the bar, display and panel", staffed, sum, plan.Components)
	}
	c.do(viewer, http.MethodGet, "/simulation/standards/no-such-standard:plan", nil, http.StatusNotFound)

	body := map[string]any{"standard": "meeting-room", "system_type": "meeting", "location": "loc-s"}

	// Refusals come before anything is created.
	componentsBefore := func() int {
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from component`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := componentsBefore()
	c.do(viewer, http.MethodPost, "/nodes/site-a/simulation/systems", body, http.StatusForbidden)
	out := c.do(builderTok, http.MethodPost, "/nodes/site-a/simulation/systems", body, http.StatusForbidden)
	if !strings.Contains(string(out), "system:create") {
		t.Fatalf("a builder without system:create was refused with %s, want it to say so", out)
	}
	c.do(ownerTok, http.MethodPost, "/nodes/site-b/simulation/systems", body, http.StatusConflict)
	c.do(locTok, http.MethodPost, "/nodes/site-a/simulation/systems", body, http.StatusForbidden)
	// The add reads too: naming a place needs location:read. A builder
	// missing it is refused up front, saying so, not told the place does
	// not exist.
	if out := c.do(noReadTok, http.MethodPost, "/nodes/site-a/simulation/systems", body, http.StatusForbidden); !strings.Contains(string(out), "location:read") {
		t.Fatalf("a builder without location:read was refused with %s, want it to say so", out)
	}
	if n := componentsBefore(); n != before {
		t.Fatalf("refused adds left %d components", n-before)
	}

	var added struct {
		System     string `json:"system"`
		SystemID   string `json:"system_id"`
		Components []struct {
			Component string `json:"component"`
			Role      string `json:"role"`
			Product   string `json:"product"`
			Address   string `json:"address"`
		} `json:"components"`
		Unstaffed []struct {
			Role   string `json:"role"`
			Reason string `json:"reason"`
		} `json:"unstaffed"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/nodes/site-a/simulation/systems", body, http.StatusCreated), &added); err != nil {
		t.Fatal(err)
	}
	if added.System == "" || added.SystemID == "" || len(added.Components) != plan.Components {
		t.Fatalf("added = %+v, want a system and the plan's %d components", added, plan.Components)
	}
	seen := map[string]bool{}
	for _, comp := range added.Components {
		if !strings.HasPrefix(comp.Address, "127.60.") || seen[comp.Address] || comp.Component == "" {
			t.Fatalf("component %+v: want a named component at its own simulated address", comp)
		}
		seen[comp.Address] = true
	}
	if len(added.Unstaffed) != len(plan.Roles)-staffed {
		t.Fatalf("unstaffed = %+v, want the plan's short roles", added.Unstaffed)
	}
	var assigned int
	if err := conn.QueryRow(ctx, `select count(*) from system_role_assignment where system_id = (select id from system where name = $1)`, added.System).Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if assigned != plan.Components {
		t.Fatalf("%d role assignments, want %d", assigned, plan.Components)
	}
	var view simulationBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/nodes/site-a/simulation", nil, http.StatusOK), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Devices) != plan.Components {
		t.Fatalf("site-a lists %d simulated components, want %d", len(view.Devices), plan.Components)
	}
}
