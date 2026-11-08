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
	t.Parallel()
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
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	disp1, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	// A component-bound tcp endpoint placed on node-a, and a shared (no component)
	// endpoint on node-a. The node placement lives on the ENDPOINT; a task carries
	// no node column (its placement projects from its endpoint).
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('disp-1-tcp', 'tcp', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.1:22"}'::jsonb),
		('shared-tcp', 'tcp', null, (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.2:22"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-bound', 'poll', (select id from endpoint where name = 'disp-1-tcp'), true),
		('t-shared', 'poll', (select id from endpoint where name = 'shared-tcp'), true)`); err != nil {
		t.Fatalf("insert tasks: %v", err)
	}

	// Bound task queried by its own node: resolves to the component.
	owner, ok, err := gw.ResolveTaskOwner(ctx, "t-bound", "node-a")
	if err != nil || !ok {
		t.Fatalf("resolve t-bound for node-a: ok=%v err=%v", ok, err)
	}
	if owner.ComponentID != disp1.ID || owner.EndpointName != "disp-1-tcp" || owner.Transport != "tcp" {
		t.Fatalf("owner = %+v, want %s / disp-1-tcp / tcp", owner, disp1.ID)
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

// TestResolveTaskOwners is ResolveTaskOwner over many tasks in one read (#1582):
// each task the node owns resolves to exactly what the single-task read gives
// it, and a task the single read refuses (another node's, a shared
// endpoint's, an unknown one) is absent from the map, never an error.
func TestResolveTaskOwners(t *testing.T) {
	t.Parallel()
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
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	for _, name := range []string{"disp-1", "disp-2", "disp-3"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatalf("create component %s: %v", name, err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('disp-1-tcp', 'tcp', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.1:22"}'::jsonb),
		('disp-2-snmp', 'snmp', (select id from component where name = 'disp-2'), (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.2:161"}'::jsonb),
		('disp-3-tcp', 'tcp', (select id from component where name = 'disp-3'), (select principal_id from node where name = 'node-b'), '{"target":"10.0.0.3:22"}'::jsonb),
		('shared-tcp', 'tcp', null, (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.4:22"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-1', 'poll', (select id from endpoint where name = 'disp-1-tcp'), true),
		('t-2', 'poll', (select id from endpoint where name = 'disp-2-snmp'), true),
		('t-other', 'poll', (select id from endpoint where name = 'disp-3-tcp'), true),
		('t-shared', 'poll', (select id from endpoint where name = 'shared-tcp'), true)`); err != nil {
		t.Fatalf("insert tasks: %v", err)
	}

	asked := []string{"t-1", "t-2", "t-other", "t-shared", "nope", "t-1"}
	got, err := gw.ResolveTaskOwners(ctx, "node-a", asked)
	if err != nil {
		t.Fatalf("resolve owners: %v", err)
	}
	for _, id := range asked {
		single, ok, err := gw.ResolveTaskOwner(ctx, id, "node-a")
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		bulk, present := got[id]
		if present != ok {
			t.Fatalf("task %s: in the bulk map = %v, the single read ok = %v", id, present, ok)
		}
		if ok && bulk != single {
			t.Fatalf("task %s: bulk %+v, single %+v", id, bulk, single)
		}
	}
	if len(got) != 2 {
		t.Fatalf("bulk map = %+v, want t-1 and t-2 alone", got)
	}
	if none, err := gw.ResolveTaskOwners(ctx, "node-a", nil); err != nil || len(none) != 0 {
		t.Fatalf("no tasks: got %+v, %v; want an empty map", none, err)
	}
}
