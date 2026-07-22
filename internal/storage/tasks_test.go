package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestResolveTaskOwner is the owner-binding + confinement fence in its storage
// core: a task's owner is its interface's component, but ONLY when the task
// belongs to the querying node. A task on another node, an unknown task, or a
// shared interface (no component) resolves to ok=false (an orphan the consumer
// drops), never an error.
func TestResolveTaskOwner(t *testing.T) {
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
	all := scope.Set{All: true}

	for _, name := range []string{"node-a", "node-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	// A component-bound tcp interface placed on node-a, and a shared (no component)
	// interface on node-a. The node placement lives on the INTERFACE; a task carries
	// no node column (its placement projects from its interface).
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, node_name, params) values
		('disp-1-tcp', 'tcp', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.1:22"}'::jsonb),
		('shared-tcp', 'tcp', null, (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.2:22"}'::jsonb)`); err != nil {
		t.Fatalf("insert interfaces: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_id, enabled) values
		('t-bound', 'poll', (select id from interface where name = 'disp-1-tcp'), true),
		('t-shared', 'poll', (select id from interface where name = 'shared-tcp'), true)`); err != nil {
		t.Fatalf("insert tasks: %v", err)
	}

	// Bound task queried by its own node: resolves to the component.
	owner, ok, err := gw.ResolveTaskOwner(ctx, "t-bound", "node-a")
	if err != nil || !ok {
		t.Fatalf("resolve t-bound for node-a: ok=%v err=%v", ok, err)
	}
	if owner.Component != "disp-1" || owner.InterfaceName != "disp-1-tcp" || owner.InterfaceType != "tcp" {
		t.Fatalf("owner = %+v, want disp-1 / disp-1-tcp / tcp", owner)
	}

	// CONFINEMENT: the same task queried by node-b resolves to nothing.
	if _, ok, err := gw.ResolveTaskOwner(ctx, "t-bound", "node-b"); err != nil || ok {
		t.Fatalf("confinement: resolve t-bound for node-b: want ok=false, got ok=%v err=%v", ok, err)
	}

	// Shared interface (no component): no pre-bound owner.
	if _, ok, err := gw.ResolveTaskOwner(ctx, "t-shared", "node-a"); err != nil || ok {
		t.Fatalf("shared interface: want ok=false, got ok=%v err=%v", ok, err)
	}

	// Unknown task: ok=false, no error.
	if _, ok, err := gw.ResolveTaskOwner(ctx, "nope", "node-a"); err != nil || ok {
		t.Fatalf("unknown task: want ok=false no error, got ok=%v err=%v", ok, err)
	}
}
