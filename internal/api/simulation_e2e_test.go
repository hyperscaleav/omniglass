package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type simulationBody struct {
	Connected    bool                       `json:"connected"`
	LastSeen     *time.Time                 `json:"last_seen"`
	Capabilities collection.SimCapabilities `json:"capabilities"`
	Devices      []struct {
		ComponentID    string         `json:"component_id"`
		Component      string         `json:"component"`
		Product        string         `json:"product"`
		Address        string         `json:"address"`
		Serving        bool           `json:"serving"`
		Hits           int            `json:"hits"`
		HitsByProtocol map[string]int `json:"hits_by_protocol"`
		LastHitAt      *time.Time     `json:"last_hit_at"`
		LastProtocol   string         `json:"last_protocol"`
	} `json:"devices"`
}

// TestOutpostSimulationAPI drives GET /outposts/{name}/simulation: the devices a
// simulator beside the outpost serves, joined to the hits the server has seen,
// gated by outpost:read, and honest (connected false, no hits) when no
// simulator has pulled.
func TestOutpostSimulationAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	for _, n := range []string{"site-a", "site-b"} {
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []string{"dsp-1", "elsewhere"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, outpost_id, params) values ('dsp-1-tcp', 'tcp', (select id from component where name = 'dsp-1'), (select user_id from outpost where name = 'site-a'), '{"target":"127.22.0.5:80"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	var dsp1 string
	if err := conn.QueryRow(ctx, `select id from component where name = 'dsp-1'`).Scan(&dsp1); err != nil {
		t.Fatal(err)
	}
	// A reader of every outpost whose component scope is one component: the
	// view lists only components that scope covers.
	scopedTo := func(component string) string {
		tok, hash, prefix, err := auth.NewBearerToken()
		if err != nil {
			t.Fatal(err)
		}
		var pid string
		if err := conn.QueryRow(ctx, `insert into users (kind) values ('machine') returning id`).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`insert into machine (user_id, name) values ($1, $2)`, []any{pid, "sim-scoped-" + component}},
			{`insert into credential (user_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, []any{pid, hash, prefix}},
			{`insert into user_grant (user_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'sim-outpost-reader'), 'all', null)`, []any{pid}},
			{`insert into user_grant (user_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = 'sim-component-reader'), 'component', (select id::text from component where name = $2))`, []any{pid, component}},
		} {
			if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
				t.Fatalf("%s: %v", q.sql, err)
			}
		}
		return tok
	}
	if _, err := conn.Exec(ctx, `insert into role (name, permissions) values ('sim-outpost-reader', '{"outpost:read"}'), ('sim-component-reader', '{"component:read"}')`); err != nil {
		t.Fatal(err)
	}
	seesDsp1, seesElsewhere := scopedTo("dsp-1"), scopedTo("elsewhere")
	conn.Close(ctx)

	reg := simhub.New()
	reg.Pulled("site-a", collection.SimCapabilities{LowPorts: true}, []string{dsp1})
	reg.Report("site-a", collection.SimHitBatch{Serving: []string{dsp1}, Hits: []collection.SimHit{
		{ComponentID: dsp1, Protocol: "snmp", At: time.Now()},
		{ComponentID: dsp1, Protocol: "http", At: time.Now()},
	}})

	srv := httptest.NewServer(api.NewHandler(gw, api.WithSimStatus(reg.Status)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	var got simulationBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/outposts/site-a/simulation", nil, http.StatusOK), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Connected || got.LastSeen == nil || !got.Capabilities.LowPorts {
		t.Fatalf("simulation = %+v, want a connected simulator with its capabilities", got)
	}
	if len(got.Devices) != 1 {
		t.Fatalf("devices = %+v, want dsp-1", got.Devices)
	}
	d := got.Devices[0]
	if d.Component != "dsp-1" || d.Address != "127.22.0.5" || !d.Serving || d.Hits != 2 ||
		d.HitsByProtocol["snmp"] != 1 || d.LastHitAt == nil || d.LastProtocol == "" {
		t.Fatalf("dsp-1 = %+v", d)
	}

	// An outpost with no simulator is honest about it.
	var idle simulationBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/outposts/site-b/simulation", nil, http.StatusOK), &idle); err != nil {
		t.Fatal(err)
	}
	if idle.Connected || idle.LastSeen != nil || len(idle.Devices) != 0 {
		t.Fatalf("idle outpost = %+v", idle)
	}

	// An all-scope viewer may read it; no credential may not; an unknown
	// outpost is a 404.
	viewer := setupAllViewer(t, ctx, dsn, "sim-viewer")
	c.do(viewer, http.MethodGet, "/outposts/site-a/simulation", nil, http.StatusOK)
	c.do("", http.MethodGet, "/outposts/site-a/simulation", nil, http.StatusUnauthorized)
	for tok, want := range map[string]int{seesDsp1: 1, seesElsewhere: 0} {
		var v simulationBody
		if err := json.Unmarshal(c.do(tok, http.MethodGet, "/outposts/site-a/simulation", nil, http.StatusOK), &v); err != nil {
			t.Fatal(err)
		}
		if len(v.Devices) != want {
			t.Errorf("a component-scoped reader sees %d devices, want %d", len(v.Devices), want)
		}
	}
	c.do(ownerTok, http.MethodGet, "/outposts/ghost/simulation", nil, http.StatusNotFound)
}
