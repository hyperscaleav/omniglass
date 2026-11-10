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

// GET /nodes/{name}/simulation/observation (#1639): the three bracketed
// reads a scale lab reconciles, gated simulation:read and refused to any
// caller whose component read scope is not all, since the node-wide totals
// cover components a scoped caller cannot see.
func TestNodeSimulationObservationAPI(t *testing.T) {
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
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "obs-loc", LocationType: "campus"}, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into role (name, permissions) values
		('obs-no-sim', '{"component:read","node:read"}'),
		('obs-sim-reader', '{"simulation:read","component:read","node:read"}')`); err != nil {
		t.Fatal(err)
	}
	granted := func(label, role, scopeKind string, scopeID *string) string {
		tok, h, p, err := auth.NewBearerToken()
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
			{`insert into service (principal_id, name) values ($1, $2)`, []any{pid, label}},
			{`insert into credential (principal_id, kind, secret_hash, prefix) values ($1, 'bearer', $2, $3)`, []any{pid, h, p}},
			{`insert into principal_grant (principal_id, role_id, scope_kind, scope_id) values ($1, (select id from role where name = $2), $3, $4)`, []any{pid, role, scopeKind, scopeID}},
		} {
			if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
				t.Fatalf("%s: %v", q.sql, err)
			}
		}
		return tok
	}
	var locID string
	if err := conn.QueryRow(ctx, `select id::text from location where name = 'obs-loc'`).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	noSim := granted("obs-no-sim", "obs-no-sim", "all", nil)
	scoped := granted("obs-scoped", "obs-sim-reader", "location", &locID)
	reader := granted("obs-reader", "obs-sim-reader", "all", nil)

	at := time.Now().UTC().Truncate(time.Millisecond)
	hub := &fakeHub{Registry: simhub.New()}
	hub.observation = map[string]collection.EndpointObservation{"site-a": {
		SimBefore: collection.SimObservation{Node: "site-a", At: at, Protocols: []collection.SimProtocolObservation{{Protocol: "http", Received: 4, Answered: 4}}},
		Node:      collection.RequestTallySnapshot{Node: "site-a", At: at.Add(time.Millisecond), Transports: []collection.TallyCounters{{Transport: "http", Sent: 5, Answered: 4, InFlight: 1}}},
		SimAfter:  collection.SimObservation{Node: "site-a", At: at.Add(2 * time.Millisecond), Protocols: []collection.SimProtocolObservation{{Protocol: "http", Received: 5, Answered: 5}}},
	}}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithSimHub(hub)))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}
	const path = "/nodes/site-a/simulation/observation"

	c.do("", http.MethodGet, path, nil, http.StatusUnauthorized)
	c.do(noSim, http.MethodGet, path, nil, http.StatusForbidden)
	c.do(scoped, http.MethodGet, path, nil, http.StatusForbidden)

	var obs collection.EndpointObservation
	if err := json.Unmarshal(c.do(reader, http.MethodGet, path, nil, http.StatusOK), &obs); err != nil {
		t.Fatal(err)
	}
	if obs.SimBefore.Protocol("http").Received != 4 || obs.Node.Transports[0].Sent != 5 || obs.SimAfter.Protocol("http").Received != 5 {
		t.Fatalf("observation = %+v, want the three reads as the bus took them", obs)
	}
	if !obs.SimBefore.At.Before(obs.Node.At) || !obs.Node.At.Before(obs.SimAfter.At) {
		t.Fatalf("reads out of order: %v, %v, %v", obs.SimBefore.At, obs.Node.At, obs.SimAfter.At)
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, path, nil, http.StatusOK), &obs); err != nil {
		t.Fatal(err)
	}

	// A node with no simulator answering is the 503 a fired operation gets,
	// a node that is not connected the 503 a run gets, and a node that does
	// not exist the 404 get-node-simulation gives.
	c.do(reader, http.MethodGet, "/nodes/site-b/simulation/observation", nil, http.StatusServiceUnavailable)
	hub.observeErr = collection.ErrNodeAbsent
	c.do(reader, http.MethodGet, path, nil, http.StatusServiceUnavailable)
	c.do(reader, http.MethodGet, "/nodes/nowhere/simulation/observation", nil, http.StatusNotFound)
}
