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

// TestTaskAPI drives the read-only task surface over HTTP. A task is DERIVED (one
// poll task per interface), never authored, so there is no create route; the test
// proves the read surface plus both authz layers on it: the permission gate (an
// all-scope viewer holds *:read and reads a task) and the scope gate (an operator
// scoped to component B reads B's derived task, while A's, cascading through A's
// interface's component, is a non-disclosing 404 and never leaks into its list).
// Skipped under -short.
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "comp-a"}, all); err != nil {
		t.Fatalf("create comp-a: %v", err)
	}
	compB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "comp-b"}, all)
	if err != nil {
		t.Fatalf("create comp-b: %v", err)
	}
	// Each interface DERIVES one poll task; that is the only way a task exists.
	ifA, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: ptr("comp-a")}, all)
	if err != nil {
		t.Fatalf("create interface on comp-a: %v", err)
	}
	ifB, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: ptr("comp-b")}, all)
	if err != nil {
		t.Fatalf("create interface on comp-b: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// The task surface is READ-ONLY: the owner sees both derived tasks, matched to
	// their interfaces.
	tasks := listTasks(c, ownerTok)
	if len(tasks) != 2 {
		t.Fatalf("owner task list = %d, want 2 derived", len(tasks))
	}
	taskA := taskByInterface(c, tasks, ifA.ID)
	taskB := taskByInterface(c, tasks, ifB.ID)
	c.do(ownerTok, http.MethodGet, "/tasks/"+taskA.ID, nil, http.StatusOK)

	// PERMISSION GATE: an all-scope viewer holds *:read -> can read a task.
	viewerAllTok := setupAllViewer(t, ctx, dsn, "viewer-all")
	c.do(viewerAllTok, http.MethodGet, "/tasks/"+taskA.ID, nil, http.StatusOK)

	// SCOPE GATE: an operator scoped to component B reads B's derived task; A's, which
	// cascades through A's interface's component, is a non-disclosing 404 and never
	// shows in its list.
	opBTok := setupScopedViewer(t, ctx, dsn, "op-b", "operator", "component", compB.ID)
	c.do(opBTok, http.MethodGet, "/tasks/"+taskA.ID, nil, http.StatusNotFound)
	c.do(opBTok, http.MethodGet, "/tasks/"+taskB.ID, nil, http.StatusOK)
	if got := listTasks(c, opBTok); len(got) == 0 {
		t.Fatalf("operator@B task list empty, want B's derived task")
	}
	for _, tk := range listTasks(c, opBTok) {
		if tk.ID == taskA.ID {
			t.Fatalf("operator@B leaked comp-a task %q", tk.ID)
		}
	}
}

type taskResp struct {
	ID          string `json:"id"`
	Mode        string `json:"mode"`
	InterfaceID string `json:"interface_id"`
	Enabled     bool   `json:"enabled"`
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

func taskByInterface(c *apiClient, tasks []taskResp, interfaceID string) taskResp {
	c.t.Helper()
	for _, tk := range tasks {
		if tk.InterfaceID == interfaceID {
			return tk
		}
	}
	c.t.Fatalf("no derived task for interface %s", interfaceID)
	return taskResp{}
}
