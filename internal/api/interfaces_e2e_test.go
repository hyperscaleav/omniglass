package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestInterfaceAPI drives the interface CRUD surface over HTTP, proving BOTH
// authz layers: the permission gate (an all-scope viewer holds *:read but not
// interface:create, so POST is a capability 403) and the scope gate (an operator
// scoped to component B holds interface:create/update but reaches component A's
// interface as a non-disclosing 404, and is refused a create under A). Skipped
// under -short.
func TestInterfaceAPI(t *testing.T) {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "comp-a", ComponentType: "display"}, all); err != nil {
		t.Fatalf("create comp-a: %v", err)
	}
	compB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "comp-b", ComponentType: "display"}, all)
	if err != nil {
		t.Fatalf("create comp-b: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner (all scope + owner wildcard) runs full CRUD. Interfaces are id-addressed,
	// so capture the surrogate id the create returns.
	ifA := createInterface(c, ownerTok, map[string]any{
		"name": "if-a", "type": "tcp", "component": "comp-a", "params": map[string]any{"target": "10.0.0.1"},
	})
	ifB := createInterface(c, ownerTok, map[string]any{
		"name": "if-b", "type": "tcp", "component": "comp-b",
	})
	if got := listInterfaces(c, ownerTok); len(got) != 2 {
		t.Fatalf("owner interface list = %d, want 2", len(got))
	}
	c.do(ownerTok, http.MethodGet, "/interfaces/"+ifA.ID, nil, http.StatusOK)
	// An unknown interface_type is a 422.
	c.do(ownerTok, http.MethodPost, "/interfaces", map[string]any{"name": "bad", "type": "galaxy"}, http.StatusUnprocessableEntity)

	// PERMISSION GATE: an all-scope viewer can read (the *:read floor) but cannot
	// create (no interface:create) -> a capability 403.
	viewerAllTok := setupAllViewer(t, ctx, dsn, "viewer-all")
	c.do(viewerAllTok, http.MethodGet, "/interfaces/"+ifA.ID, nil, http.StatusOK)
	c.do(viewerAllTok, http.MethodPost, "/interfaces", map[string]any{"name": "if-x", "type": "tcp", "component": "comp-a"}, http.StatusForbidden)
	c.do(viewerAllTok, http.MethodPatch, "/interfaces/"+ifA.ID, map[string]any{"params": map[string]any{"target": "9.9.9.9"}}, http.StatusForbidden)

	// SCOPE GATE: an operator scoped to component B holds interface:create/update
	// but its scope cascades only through B. A's interface is a non-disclosing 404
	// on read AND on update; a create under A is a 403 (out of the create scope);
	// its own B interface is fully reachable.
	opBTok := setupScopedViewer(t, ctx, dsn, "op-b", "operator", "component", compB.ID)
	c.do(opBTok, http.MethodGet, "/interfaces/"+ifA.ID, nil, http.StatusNotFound)
	c.do(opBTok, http.MethodPatch, "/interfaces/"+ifA.ID, map[string]any{"params": map[string]any{"target": "9.9.9.9"}}, http.StatusNotFound)
	c.do(opBTok, http.MethodPost, "/interfaces", map[string]any{"name": "if-a2", "type": "tcp", "component": "comp-a"}, http.StatusForbidden)
	c.do(opBTok, http.MethodGet, "/interfaces/"+ifB.ID, nil, http.StatusOK)
	c.do(opBTok, http.MethodPost, "/interfaces", map[string]any{"name": "if-b2", "type": "tcp", "component": "comp-b"}, http.StatusCreated)
	// The scoped operator's list shows only B's interfaces (if-b, if-b2), never A's.
	for _, it := range listInterfaces(c, opBTok) {
		if it.Component != nil && *it.Component == "comp-a" {
			t.Fatalf("operator@B leaked comp-a interface %q", it.Name)
		}
	}
}

type interfaceResp struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Component *string `json:"component"`
	Node      *string `json:"node"`
}

func createInterface(c *apiClient, tok string, body map[string]any) interfaceResp {
	c.t.Helper()
	out := c.do(tok, http.MethodPost, "/interfaces", body, http.StatusCreated)
	var it interfaceResp
	if err := json.Unmarshal(out, &it); err != nil {
		c.t.Fatalf("decode interface: %v", err)
	}
	return it
}

func listInterfaces(c *apiClient, tok string) []interfaceResp {
	c.t.Helper()
	out := c.do(tok, http.MethodGet, "/interfaces", nil, http.StatusOK)
	var body struct {
		Interfaces []interfaceResp `json:"interfaces"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		c.t.Fatalf("decode interface list: %v", err)
	}
	return body.Interfaces
}
