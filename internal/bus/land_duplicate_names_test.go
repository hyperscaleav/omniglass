package bus

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
)

func landStrptr(s string) *string { return &s }

// TestLandSurvivesDuplicateComponentNames is the node ingest lane's regression
// test for the #627 review's finding 1: ResolveTaskOwner used to discard the
// component id it already had (i.component, an FK right there in the row) in
// favor of a name derived from it, which every downstream sink then had to
// re-resolve, ambiguously, the moment a second component shared that name.
// Under the OLD code this batch would be Term'd after five silent Naks, with a
// single slog.Warn and no other operator-visible error: the data is just gone.
//
// This drives the real path: two same-named components in different rooms, a
// task bound to ONE of them, ResolveTaskOwner resolving that task's owner for
// real (proving the storage-layer fix), and Server.land (the one write path
// both ingest lanes share) writing a real TelemetryBatch through it, asserting
// the sample lands on the task's own component and not its same-named sibling.
func TestLandSurvivesDuplicateComponentNames(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	// component_name_key is dropped so this test's own database can hold two
	// same-named components, the condition #627's DDL (not yet landed) will
	// make legal for real; this proves the ingest lane is already safe for it.
	if _, err := conn.Exec(ctx, `alter table component drop constraint component_name_key`); err != nil {
		t.Fatalf("drop component name constraint: %v", err)
	}

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	roomA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-a: %v", err)
	}
	roomB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-b", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-b: %v", err)
	}
	compA, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", LocationName: landStrptr(roomA.Name)}, all)
	if err != nil {
		t.Fatalf("component in room-a: %v", err)
	}
	compB, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", LocationName: landStrptr(roomB.Name)}, all)
	if err != nil {
		t.Fatalf("component in room-b: %v", err)
	}
	if compA.ID == compB.ID {
		t.Fatalf("the two components did not actually land on different rows")
	}

	// A task bound to compA's interface, on node-a.
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, node_name, params) values
		('display-1-tcp', (select id from interface_type where name = 'tcp'), $1::uuid, (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.1:22"}'::jsonb)`,
		compA.ID); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_id, enabled) values
		('t-a', 'poll', (select id from interface where name = 'display-1-tcp'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// ResolveTaskOwner must resolve compA's id directly, not "display-1" (now
	// ambiguous between compA and compB).
	owner, ok, err := gw.ResolveTaskOwner(ctx, "t-a", "node-a")
	if err != nil {
		t.Fatalf("resolve task owner: %v", err)
	}
	if !ok {
		t.Fatalf("resolve task owner: want ok=true")
	}
	if owner.ComponentID != compA.ID {
		t.Fatalf("resolved owner = %q, want compA's id %s", owner.ComponentID, compA.ID)
	}

	// land is the one write path both ingest lanes share; driving it directly
	// (rather than through JetStream) is a lighter-weight but equally real
	// exercise of the storage writes finding 1 was about.
	s := &Server{store: gw}
	ev := &ogv1.TelemetryBatch{
		TaskId:  "t-a",
		NodeId:  "node-a",
		Metrics: []*ogv1.MetricSample{{Name: "tcp-open", Value: 1}},
	}
	bind := ingestBinding{SampleOwner: &owner}
	if err := s.land(ctx, ev, bind); err != nil {
		t.Fatalf("land: %v", err)
	}

	dpA, err := gw.LatestMetric(ctx, compA.ID, "tcp-open")
	if err != nil {
		t.Fatalf("latest metric compA: %v", err)
	}
	if dpA == nil || dpA.Value != 1 {
		t.Fatalf("compA tcp-open = %+v, want 1", dpA)
	}
	dpB, err := gw.LatestMetric(ctx, compB.ID, "tcp-open")
	if err != nil {
		t.Fatalf("latest metric compB: %v", err)
	}
	if dpB != nil {
		t.Fatalf("compB tcp-open = %+v, want nil (cross-contaminated by the shared name)", dpB)
	}
}
