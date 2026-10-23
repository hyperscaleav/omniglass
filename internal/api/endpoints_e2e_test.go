package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestEndpointAPI drives the endpoint CRUD surface over HTTP, proving BOTH
// authz layers: the permission gate (an all-scope viewer holds *:read but not
// endpoint:create, so POST is a capability 403) and the scope gate (an operator
// scoped to component B holds endpoint:create/update but reaches component A's
// endpoint as a non-disclosing 404, and is refused a create under A). Skipped
// under -short.
func TestEndpointAPI(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "comp-a"}, all, all, all, all); err != nil {
		t.Fatalf("create comp-a: %v", err)
	}
	compB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "comp-b"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create comp-b: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner (all scope + owner wildcard) runs full CRUD. The endpoint is protocol-
	// named (name = type), so the create body carries no name; capture the surrogate
	// id the create returns.
	ifA := createEndpoint(c, ownerTok, map[string]any{
		"transport": "tcp", "component": "comp-a", "params": map[string]any{"target": "10.0.0.1"},
	})
	ifB := createEndpoint(c, ownerTok, map[string]any{
		"transport": "tcp", "component": "comp-b",
	})
	if got := listEndpoints(c, ownerTok); len(got) != 2 {
		t.Fatalf("owner endpoint list = %d, want 2", len(got))
	}
	c.do(ownerTok, http.MethodGet, "/endpoints/"+ifA.ID, nil, http.StatusOK)
	// An unknown transport is a 422.
	c.do(ownerTok, http.MethodPost, "/endpoints", map[string]any{"transport": "galaxy"}, http.StatusUnprocessableEntity)

	// PERMISSION GATE: an all-scope viewer can read (the *:read floor) but cannot
	// create (no endpoint:create) -> a capability 403.
	viewerAllTok := setupAllViewer(t, ctx, dsn, "viewer-all")
	c.do(viewerAllTok, http.MethodGet, "/endpoints/"+ifA.ID, nil, http.StatusOK)
	c.do(viewerAllTok, http.MethodPost, "/endpoints", map[string]any{"transport": "http", "component": "comp-a"}, http.StatusForbidden)
	c.do(viewerAllTok, http.MethodPatch, "/endpoints/"+ifA.ID, map[string]any{"params": map[string]any{"target": "9.9.9.9"}}, http.StatusForbidden)

	// SCOPE GATE: an operator scoped to component B holds endpoint:create/update
	// but its scope cascades only through B. A's endpoint is a non-disclosing 404
	// on read AND on update; a create under A is a 403 (out of the create scope);
	// its own B endpoint is fully reachable (a different transport, so no collision).
	opBTok := setupScopedViewer(t, ctx, dsn, "op-b", "operator", "component", compB.ID)
	c.do(opBTok, http.MethodGet, "/endpoints/"+ifA.ID, nil, http.StatusNotFound)
	c.do(opBTok, http.MethodPatch, "/endpoints/"+ifA.ID, map[string]any{"params": map[string]any{"target": "9.9.9.9"}}, http.StatusNotFound)
	c.do(opBTok, http.MethodPost, "/endpoints", map[string]any{"transport": "http", "component": "comp-a"}, http.StatusForbidden)
	c.do(opBTok, http.MethodGet, "/endpoints/"+ifB.ID, nil, http.StatusOK)
	c.do(opBTok, http.MethodPost, "/endpoints", map[string]any{"transport": "icmp", "component": "comp-b"}, http.StatusCreated)
	// The scoped operator's list shows only B's endpoints (if-b, if-b2), never A's.
	for _, it := range listEndpoints(c, opBTok) {
		if it.Component != nil && *it.Component == "comp-a" {
			t.Fatalf("operator@B leaked comp-a endpoint %q", it.Name)
		}
	}

	// :retry (#1049) wakes a rejected session for one attempt. It is gated by
	// endpoint:retry, which the viewer floor lacks (a capability 403), scoped
	// through the component (A's endpoint is a non-disclosing 404 to the
	// operator scoped to B), and held by the operator in scope.
	c.do(viewerAllTok, http.MethodPost, "/endpoints/"+ifA.ID+":retry", nil, http.StatusForbidden)
	c.do(opBTok, http.MethodPost, "/endpoints/"+ifA.ID+":retry", nil, http.StatusNotFound)
	c.do(opBTok, http.MethodPost, "/endpoints/"+ifB.ID+":retry", nil, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/endpoints/"+ifA.ID+":retry", nil, http.StatusOK)
	// A driver-attached endpoint's connection inputs edit in place (#1061
	// ruling): its port moves its derived target; a value the driver refuses
	// is a 400 naming the field; the viewer floor is refused.
	nvp := "newtron-nvp"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "comp-d"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	att := createEndpoint(c, ownerTok, map[string]any{"driver": nvp, "component": "comp-d", "inputs": map[string]any{"host": "10.0.0.7"}})
	var edited struct {
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPatch, "/endpoints/"+att.ID, map[string]any{"inputs": map[string]any{"port": "4000"}}, http.StatusOK), &edited); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(edited.Params), "10.0.0.7:4000") {
		t.Fatalf("the edited endpoint's params = %s, want the new port in its target", edited.Params)
	}
	bad := c.do(ownerTok, http.MethodPatch, "/endpoints/"+att.ID, map[string]any{"inputs": map[string]any{"port": "forty"}}, http.StatusBadRequest)
	if !strings.Contains(string(bad), "port") {
		t.Fatalf("a refused input value does not name the field: %s", bad)
	}
	c.do(ownerTok, http.MethodPatch, "/endpoints/"+att.ID, map[string]any{"transport": "ssh"}, http.StatusBadRequest)
	c.do(ownerTok, http.MethodPatch, "/endpoints/"+att.ID, map[string]any{"params": map[string]any{"target": "9.9.9.9:1"}}, http.StatusBadRequest)
	c.do(viewerAllTok, http.MethodPatch, "/endpoints/"+att.ID, map[string]any{"inputs": map[string]any{"port": "4001"}}, http.StatusForbidden)

	// A host key is an ssh endpoint's alone (#1032): a tcp endpoint refuses one.
	c.do(ownerTok, http.MethodPost, "/endpoints/"+ifA.ID+":retry",
		map[string]any{"host_key": "SHA256:" + strings.Repeat("A", 43)}, http.StatusUnprocessableEntity)
}

// TestEndpointCreateWithAmbiguousComponentIs409 closes I1's other half: the
// gateway resolves the create body's "component" field through
// resolveScopedRef (ruling 2, #627), and on more than one in-scope match that
// surfaces storage.ErrAmbiguousName, which mapEndpointErr translates through
// its mapRefErr check into a 409 naming the reference. Before that check
// existed, it fell through to the handler's default, an unmapped 500, for a
// state #627 makes routine (two same-named components in different placement
// buckets), not exceptional. Skipped under -short.
func TestEndpointCreateWithAmbiguousComponentIs409(t *testing.T) {
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

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "holder"}, all, all, all, all); err != nil {
		t.Fatalf("create holder: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-cmp"}, all, all, all, all); err != nil {
		t.Fatalf("create root dup-cmp: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dup-cmp", ParentName: ptr("holder")}, all, all, all, all); err != nil {
		t.Fatalf("create nested dup-cmp: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	status, body := c.send(ownerTok, http.MethodPost, "/endpoints", map[string]any{"transport": "tcp", "component": "dup-cmp"})
	if status != http.StatusConflict {
		t.Fatalf("create endpoint with ambiguous component status = %d, want 409\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "dup-cmp") {
		t.Fatalf("409 body = %s, want it to name the ambiguous reference", body)
	}
}

type endpointResp struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"transport"`
	Component *string `json:"component"`
	Node      *string `json:"node"`
}

func createEndpoint(c *apiClient, tok string, body map[string]any) endpointResp {
	c.t.Helper()
	out := c.do(tok, http.MethodPost, "/endpoints", body, http.StatusCreated)
	var it endpointResp
	if err := json.Unmarshal(out, &it); err != nil {
		c.t.Fatalf("decode endpoint: %v", err)
	}
	return it
}

func listEndpoints(c *apiClient, tok string) []endpointResp {
	c.t.Helper()
	out := c.do(tok, http.MethodGet, "/endpoints", nil, http.StatusOK)
	var body struct {
		Endpoints []endpointResp `json:"endpoints"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		c.t.Fatalf("decode endpoint list: %v", err)
	}
	return body.Endpoints
}
