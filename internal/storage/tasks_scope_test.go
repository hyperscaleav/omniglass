package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestTaskDerivedScope covers the task tier as DERIVED read-only plumbing: a task
// is created automatically when its interface is created (one poll task per
// interface), has no operator write surface, projects its node placement from its
// interface, cascades away when its interface is deleted, and reads through its
// interface's owning component's scope (A-scope sees A's task, B's is a
// non-disclosing 404).
func TestTaskDerivedScope(t *testing.T) {
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

	// Each interface DERIVES exactly one poll task. The interface is protocol-named
	// (name = type), so these three differ by component / transport.
	ifA, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr("comp-a")}, all)
	if err != nil {
		t.Fatalf("create interface on comp-a: %v", err)
	}
	ifB, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr("comp-b")}, all)
	if err != nil {
		t.Fatalf("create interface on comp-b: %v", err)
	}
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "icmp"}, all); err != nil {
		t.Fatalf("create component-less interface: %v", err)
	}

	// Derive: each interface produced one poll task; all-scope sees all three.
	all3, err := gw.ListTasks(ctx, all)
	if err != nil || len(all3) != 3 {
		t.Fatalf("all-scope task list = %d (err %v), want 3 derived poll tasks", len(all3), err)
	}
	for _, task := range all3 {
		if task.Mode != "poll" {
			t.Errorf("derived task %s mode = %q, want poll", task.ID, task.Mode)
		}
	}
	taskA := taskForInterface(t, all3, ifA.ID)
	taskB := taskForInterface(t, all3, ifB.ID)

	// Cascade READ: A-scope sees only A's task; B's is a non-disclosing 404.
	got, err := gw.ListTasks(ctx, readA)
	if err != nil || len(got) != 1 || got[0].ID != taskA.ID {
		t.Fatalf("A-scope task list = %+v (err %v), want just A's derived task", got, err)
	}
	if _, err := gw.GetTask(ctx, taskA.ID, readA); err != nil {
		t.Fatalf("get A's task under A-scope: %v", err)
	}
	if _, err := gw.GetTask(ctx, taskB.ID, readA); !errors.Is(err, storage.ErrTaskNotFound) {
		t.Errorf("get B's task under A-scope = %v, want ErrTaskNotFound (non-disclosing)", err)
	}

	// Node PROJECTION: placing the interface on a node projects onto its task (the
	// task carries no node column of its own).
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-1"}, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := gw.UpdateInterface(ctx, "", ifA.ID, storage.InterfacePatch{Node: strptr("edge-1")}, all, all); err != nil {
		t.Fatalf("place interface on edge-1: %v", err)
	}
	placed, err := gw.GetTask(ctx, taskA.ID, all)
	if err != nil || placed.Node == nil || *placed.Node != "edge-1" {
		t.Fatalf("derived task node = %v (err %v), want projected edge-1", placed.Node, err)
	}

	// Cascade DELETE: deleting the interface removes its derived task.
	if err := gw.DeleteInterface(ctx, "", ifA.ID, all, all); err != nil {
		t.Fatalf("delete interface: %v", err)
	}
	if _, err := gw.GetTask(ctx, taskA.ID, all); !errors.Is(err, storage.ErrTaskNotFound) {
		t.Errorf("A's task after interface delete = %v, want ErrTaskNotFound (cascade)", err)
	}
	if remaining, err := gw.ListTasks(ctx, all); err != nil || len(remaining) != 2 {
		t.Fatalf("task list after cascade = %d (err %v), want 2", len(remaining), err)
	}
}

// taskForInterface finds the single derived task for an interface id.
func taskForInterface(t *testing.T, tasks []storage.Task, interfaceID string) storage.Task {
	t.Helper()
	for _, task := range tasks {
		if task.InterfaceID == interfaceID {
			return task
		}
	}
	t.Fatalf("no derived task for interface %s", interfaceID)
	return storage.Task{}
}
