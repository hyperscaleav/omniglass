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
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
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

// TestNodeSimulationAPI drives GET /nodes/{name}/simulation: the devices a
// simulator beside the node serves, joined to the hits the server has seen,
// gated by node:read, and honest (connected false, no hits) when no
// simulator has pulled.
func TestNodeSimulationAPI(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dsp-1"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, node_name, params) values ('dsp-1-tcp', (select id from interface_type where name = 'tcp'), (select id from component where name = 'dsp-1'), (select principal_id from node where name = 'site-a'), '{"target":"127.22.0.5:80"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	var dsp1 string
	if err := conn.QueryRow(ctx, `select id from component where name = 'dsp-1'`).Scan(&dsp1); err != nil {
		t.Fatal(err)
	}
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
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/nodes/site-a/simulation", nil, http.StatusOK), &got); err != nil {
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

	// A node with no simulator is honest about it.
	var idle simulationBody
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/nodes/site-b/simulation", nil, http.StatusOK), &idle); err != nil {
		t.Fatal(err)
	}
	if idle.Connected || idle.LastSeen != nil || len(idle.Devices) != 0 {
		t.Fatalf("idle node = %+v", idle)
	}

	// An all-scope viewer may read it; no credential may not; an unknown
	// node is a 404.
	viewer := setupAllViewer(t, ctx, dsn, "sim-viewer")
	c.do(viewer, http.MethodGet, "/nodes/site-a/simulation", nil, http.StatusOK)
	c.do("", http.MethodGet, "/nodes/site-a/simulation", nil, http.StatusUnauthorized)
	c.do(ownerTok, http.MethodGet, "/nodes/ghost/simulation", nil, http.StatusNotFound)
}
