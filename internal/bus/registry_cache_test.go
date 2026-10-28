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

// TestRegistryFollowsTheCatalogBetweenBatches is #1279's acceptance: the
// ingest consumer keeps its sample registry between batches instead of
// re-reading the catalogs every time, and still honors a catalog change on the
// very next batch. A metric type created between two batches is accepted by
// the second; a calc rule that starts writing that metric moves the key's
// writer class (ADR-0161), so the next node sample of it is dropped by the
// consumer's lane check rather than landed.
func TestRegistryFollowsTheCatalogBetweenBatches(t *testing.T) {
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
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('ssh', 'ssh', $1::uuid, (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.1:22"}'::jsonb)`, comp.ID); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-ssh', 'poll', (select id from endpoint where name = 'ssh'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	owner, ok, err := gw.ResolveTaskOwner(ctx, "t-ssh", "node-a")
	if err != nil || !ok {
		t.Fatalf("resolve task owner: ok=%v err=%v", ok, err)
	}
	s := &Server{store: gw, orphans: newOrphanTally(orphanWindow)}
	land := func() error {
		return s.land(ctx, "", &ogv1.TelemetryBatch{TaskId: "t-ssh", NodeId: "node-a",
			Metrics: []*ogv1.MetricSample{{Name: "cache-probe", Value: 1}}},
			ingestBinding{LogNode: "node-a", SampleOwner: &owner, Arc: componentArc(owner), Writer: storage.NodeWriter(owner.ComponentID, owner.EndpointName)})
	}
	rows := func() int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from metric m join metric_type mt on mt.id = m.metric_type_id
			where mt.name = 'cache-probe'`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	// An unregistered name is dropped; the registry is now cached.
	if err := land(); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	if n := rows(); n != 0 {
		t.Fatalf("an unregistered metric landed %d rows", n)
	}

	// The type is created between batches: the next batch honors it.
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "cache-probe", DataType: "float"}); err != nil {
		t.Fatalf("create metric type: %v", err)
	}
	if err := land(); err != nil {
		t.Fatalf("second batch: %v", err)
	}
	if n := rows(); n != 1 {
		t.Fatalf("after the type was created, the next batch landed %d rows, want 1", n)
	}

	// A calc rule starts writing the metric: its writer class moves, and the
	// next node sample of it is refused, not landed.
	if _, err := conn.Exec(ctx, `insert into calc_rule (name, owner_kind, reduce, output_lane, output_metric_type_id)
		values ('cache-probe-calc', 'component', '{}', 'metric', (select id from metric_type where name = 'cache-probe'))`); err != nil {
		t.Fatalf("insert calc rule: %v", err)
	}
	if err := land(); err != nil {
		t.Fatalf("third batch: %v", err)
	}
	if n := rows(); n != 1 {
		t.Fatalf("a node sample of a calc rule's output landed: %d rows, want 1 (its writer class drops it)", n)
	}
}
