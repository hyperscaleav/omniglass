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

// TestTaskAPI drives the task CRUD surface over HTTP, proving BOTH authz layers:
// the permission gate (an all-scope viewer holds *:read but not task:create, so
// POST is a capability 403) and the scope gate (an operator scoped to component B
// reaches component A's task, which cascades through A's interface's component, as
// a non-disclosing 404, and is refused a create under A's interface). Skipped
// under -short.
func TestTaskAPI(t *testing.T) {
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
	// An interface on each component to hang tasks off.
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "if-a", Type: "tcp", Component: ptr("comp-a")}, all); err != nil {
		t.Fatalf("create if-a: %v", err)
	}
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "if-b", Type: "tcp", Component: ptr("comp-b")}, all); err != nil {
		t.Fatalf("create if-b: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Owner creates a task on each interface; the id is content-addressed server-side.
	taskA := createTask(c, ownerTok, map[string]any{"mode": "poll", "interface": "if-a", "spec": map[string]any{"probe": "tcp"}})
	createTask(c, ownerTok, map[string]any{"mode": "poll", "interface": "if-b", "spec": map[string]any{"probe": "tcp"}})
	if got := listTasks(c, ownerTok); len(got) != 2 {
		t.Fatalf("owner task list = %d, want 2", len(got))
	}
	c.do(ownerTok, http.MethodGet, "/tasks/"+taskA.ID, nil, http.StatusOK)
	// A task over an unknown interface is a 422.
	c.do(ownerTok, http.MethodPost, "/tasks", map[string]any{"mode": "poll", "interface": "ghost"}, http.StatusUnprocessableEntity)

	// PERMISSION GATE: an all-scope viewer reads (the *:read floor) but cannot
	// create (no task:create) -> a capability 403.
	viewerAllTok := setupAllViewer(t, ctx, dsn, "viewer-all")
	c.do(viewerAllTok, http.MethodGet, "/tasks/"+taskA.ID, nil, http.StatusOK)
	c.do(viewerAllTok, http.MethodPost, "/tasks", map[string]any{"mode": "poll", "interface": "if-a"}, http.StatusForbidden)
	c.do(viewerAllTok, http.MethodPatch, "/tasks/"+taskA.ID, map[string]any{"enabled": false}, http.StatusForbidden)

	// SCOPE GATE: an operator scoped to component B holds task:create/update but its
	// scope cascades only through B (via its interface's component). A's task is a
	// non-disclosing 404 on read AND update; a create over A's interface is a 403;
	// B's own task path is fully reachable.
	opBTok := setupScopedViewer(t, ctx, dsn, "op-b", "operator", "component", compB.ID)
	c.do(opBTok, http.MethodGet, "/tasks/"+taskA.ID, nil, http.StatusNotFound)
	c.do(opBTok, http.MethodPatch, "/tasks/"+taskA.ID, map[string]any{"enabled": false}, http.StatusNotFound)
	c.do(opBTok, http.MethodPost, "/tasks", map[string]any{"mode": "poll", "interface": "if-a", "spec": map[string]any{"x": 1}}, http.StatusForbidden)
	taskB2 := createTask(c, opBTok, map[string]any{"mode": "listen", "interface": "if-b"})
	c.do(opBTok, http.MethodGet, "/tasks/"+taskB2.ID, nil, http.StatusOK)
	// The scoped operator's list never shows A's task.
	if got := listTasks(c, opBTok); len(got) == 0 {
		t.Fatalf("operator@B task list empty, want its own B tasks")
	}
	for _, tk := range listTasks(c, opBTok) {
		if tk.ID == taskA.ID {
			t.Fatalf("operator@B leaked comp-a task %q", tk.ID)
		}
	}
}

type taskResp struct {
	ID        string `json:"id"`
	Mode      string `json:"mode"`
	Interface string `json:"interface"`
	Enabled   bool   `json:"enabled"`
}

func createTask(c *apiClient, tok string, body map[string]any) taskResp {
	c.t.Helper()
	out := c.do(tok, http.MethodPost, "/tasks", body, http.StatusCreated)
	var tk taskResp
	if err := json.Unmarshal(out, &tk); err != nil {
		c.t.Fatalf("decode task: %v", err)
	}
	return tk
}

func listTasks(c *apiClient, tok string) []taskResp {
	c.t.Helper()
	out := c.do(tok, http.MethodGet, "/tasks", nil, http.StatusOK)
	var body struct {
		Tasks []taskResp `json:"tasks"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		c.t.Fatalf("decode task list: %v", err)
	}
	return body.Tasks
}
