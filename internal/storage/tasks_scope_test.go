package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestTaskScopeCRUD covers the task tier's component-cascade scope: a task
// inherits its interface's owning component's read/action scope. A principal
// scoped to component A can list/get/create/update/delete A's tasks and is denied
// B's task and any component-less interface's task. Plus content-addressed dedup,
// FK faults, and audit.
func TestTaskScopeCRUD(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	compA := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-a", ComponentType: "display"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-b", ComponentType: "display"}, all)
	readA := scope.Set{IDs: []string{compA.ID}}

	// An interface on A, on B, and a component-less one; keep their surrogate ids
	// (tasks reference the interface by id now).
	ifID := map[string]string{}
	for _, s := range []storage.InterfaceSpec{
		{Name: "if-a", Type: "tcp", Component: strptr("comp-a")},
		{Name: "if-b", Type: "tcp", Component: strptr("comp-b")},
		{Name: "if-null", Type: "icmp"},
	} {
		it, err := gw.CreateInterface(ctx, "", s, all)
		if err != nil {
			t.Fatalf("create interface %s: %v", s.Name, err)
		}
		ifID[s.Name] = it.ID
	}

	// A task on each interface (owner/all).
	taskA, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "poll", InterfaceID: ifID["if-a"], Spec: []byte(`{"probe":"tcp"}`)}, all)
	if err != nil {
		t.Fatalf("create task-a: %v", err)
	}
	taskB, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "poll", InterfaceID: ifID["if-b"], Spec: []byte(`{"probe":"tcp"}`)}, all)
	if err != nil {
		t.Fatalf("create task-b: %v", err)
	}
	if _, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "poll", InterfaceID: ifID["if-null"]}, all); err != nil {
		t.Fatalf("create task-null: %v", err)
	}

	// Cascade READ: A-scope sees only A's task.
	got, err := gw.ListTasks(ctx, readA)
	if err != nil || len(got) != 1 || got[0].ID != taskA.ID {
		t.Fatalf("A-scope task list = %+v (err %v), want just task-a", got, err)
	}
	if _, err := gw.GetTask(ctx, taskA.ID, readA); err != nil {
		t.Fatalf("get task-a under A-scope: %v", err)
	}
	if _, err := gw.GetTask(ctx, taskB.ID, readA); !errors.Is(err, storage.ErrTaskNotFound) {
		t.Errorf("get task-b under A-scope = %v, want ErrTaskNotFound (non-disclosing)", err)
	}
	if all3, err := gw.ListTasks(ctx, all); err != nil || len(all3) != 3 {
		t.Fatalf("all-scope task list = %d (err %v), want 3", len(all3), err)
	}

	// Cascade CREATE: A-scope creates on A's interface, forbidden on B's and on the
	// component-less one.
	if _, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "listen", InterfaceID: ifID["if-a"]}, readA); err != nil {
		t.Errorf("create on if-a with A-scope = %v, want ok", err)
	}
	if _, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "poll", InterfaceID: ifID["if-b"], Spec: []byte(`{"x":1}`)}, readA); !errors.Is(err, storage.ErrTaskForbidden) {
		t.Errorf("create on if-b with A-scope = %v, want ErrTaskForbidden", err)
	}
	if _, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "poll", InterfaceID: ifID["if-null"], Spec: []byte(`{"x":1}`)}, readA); !errors.Is(err, storage.ErrTaskForbidden) {
		t.Errorf("create on component-less if-null with A-scope = %v, want ErrTaskForbidden", err)
	}

	// Cascade UPDATE/DELETE: out of read scope is 404; readable not actionable is 403.
	if _, err := gw.UpdateTask(ctx, "", taskB.ID, storage.TaskPatch{Enabled: boolptr(false)}, readA, readA); !errors.Is(err, storage.ErrTaskNotFound) {
		t.Errorf("update task-b under A-scope = %v, want ErrTaskNotFound", err)
	}
	if _, err := gw.UpdateTask(ctx, "", taskA.ID, storage.TaskPatch{Enabled: boolptr(false)}, readA, scope.Set{}); !errors.Is(err, storage.ErrTaskForbidden) {
		t.Errorf("update task-a in-read not-action = %v, want ErrTaskForbidden", err)
	}
	upd, err := gw.UpdateTask(ctx, "", taskA.ID, storage.TaskPatch{Enabled: boolptr(false), DisplayName: strptr("A probe")}, readA, readA)
	if err != nil || upd.Enabled || upd.DisplayName != "A probe" {
		t.Fatalf("update task-a = %+v (err %v), want enabled=false name='A probe'", upd, err)
	}
	if err := gw.DeleteTask(ctx, "", taskB.ID, readA, readA); !errors.Is(err, storage.ErrTaskNotFound) {
		t.Errorf("delete task-b under A-scope = %v, want ErrTaskNotFound", err)
	}
	if err := gw.DeleteTask(ctx, "", taskA.ID, all, all); err != nil {
		t.Errorf("delete task-a with all = %v, want ok", err)
	}

	// Content-addressed dedup: same (interface, mode, spec) collides on the id.
	if _, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "poll", InterfaceID: ifID["if-b"], Spec: []byte(`{"probe":"tcp"}`)}, all); !errors.Is(err, storage.ErrTaskExists) {
		t.Errorf("dup content task = %v, want ErrTaskExists", err)
	}

	// FK / value faults. A well-formed but unknown interface id is a 422.
	if _, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "poll", InterfaceID: "00000000-0000-0000-0000-000000000000"}, all); !errors.Is(err, storage.ErrTaskInterfaceNotFound) {
		t.Errorf("unknown interface = %v, want ErrTaskInterfaceNotFound", err)
	}
	if _, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "sideways", InterfaceID: ifID["if-a"]}, all); !errors.Is(err, storage.ErrInvalidTaskMode) {
		t.Errorf("bad mode = %v, want ErrInvalidTaskMode", err)
	}
	if _, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "poll", InterfaceID: ifID["if-a"], Node: strptr("no-node"), Spec: []byte(`{"n":1}`)}, all); !errors.Is(err, storage.ErrTaskNodeNotFound) {
		t.Errorf("unknown node = %v, want ErrTaskNodeNotFound", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("audit connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'task'`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n == 0 {
		t.Errorf("task audit rows = 0, want the create/update/delete trail")
	}
}

func boolptr(b bool) *bool { return &b }
