package labfootprint

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The ingest lanes have drained past a time once every live node that
// collects has landed a sample it probed after it: a node's sample probed at
// or before the time, a sample no node collected (a webhook count the server
// writes outside the lanes), and a sample stamped in the database's future do
// not drain it, and one node's sample says nothing of another's lane (#1896).
// A node silent since the time (no heartbeat after it) is not waited for.
func TestIntegrationDrainedQueryWaitsForEveryLiveNodesSampleAfterTheTime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if _, err := gw.EnsureMetricPartitions(ctx); err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	node, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all, all)
	if err != nil {
		t.Fatal(err)
	}
	nodeB, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-b"}, all, all)
	if err != nil {
		t.Fatal(err)
	}
	silent, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-silent"}, all, all)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	var now time.Time
	if err := conn.QueryRow(ctx, `select now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	at := now.Add(-time.Minute)

	drained := func() bool {
		t.Helper()
		var flag, epoch string
		if err := conn.QueryRow(ctx, drainedQuery(at)).Scan(&flag, &epoch); err != nil {
			t.Fatal(err)
		}
		ok, clock, err := parseDrained(flag + "|" + epoch)
		if err != nil {
			t.Fatal(err)
		}
		if clock.Before(now) {
			t.Fatalf("the drain read the clock at %s, before the test's %s", clock, now)
		}
		return ok
	}
	write := func(w storage.KeyWriter, ts time.Time) {
		t.Helper()
		if err := gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: comp.ID,
			Key: "tcp-connect-time", Instance: "ssh", Value: 1, TS: ts}}); err != nil {
			t.Fatal(err)
		}
	}
	collected := storage.NodeWriter(comp.ID, "ssh")
	collected.Node = node.PrincipalID
	collectedB := storage.NodeWriter(comp.ID, "ssh")
	collectedB.Node = nodeB.PrincipalID
	collectedSilent := storage.NodeWriter(comp.ID, "ssh")
	collectedSilent.Node = silent.PrincipalID

	write(collected, at)
	write(collected, at.Add(-time.Second))
	write(collectedB, at.Add(-time.Second))
	write(collectedSilent, at.Add(-time.Second))
	// node-a and node-b are live (a heartbeat after the time, which reaches
	// the server outside the lanes); node-silent last spoke before it.
	for _, name := range []string{"node-a", "node-b"} {
		if err := gw.RecordHeartbeat(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := conn.Exec(ctx, `update node set last_heartbeat_at = $1 where name = 'node-silent'`, at.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	plant := func(instance string, ts time.Time) {
		t.Helper()
		if _, err := conn.Exec(ctx, storagetest.WideMetricInsert("ts, owner_kind, component_id, metric_type_id, instance, value",
			`select $1::timestamptz, 'component', $2::uuid, id, $3::text, 1 from metric_type where name = 'tcp-connect-time'`),
			ts, comp.ID, instance); err != nil {
			t.Fatal(err)
		}
	}
	// A sample no node collected, as the server writes a webhook's counts.
	plant("server", at.Add(time.Second))
	// A node's sample dated an hour ahead, on a key the gateway does not
	// cap at its clock.
	plant("ahead", now.Add(time.Hour))
	if _, err := conn.Exec(ctx, `update metric_series set collector_node_id = $1 where instance = 'ahead'`, node.PrincipalID); err != nil {
		t.Fatal(err)
	}
	if drained() {
		t.Fatal("drained before any node's sample probed after the time landed")
	}
	write(collected, at.Add(time.Second))
	if drained() {
		t.Fatal("drained once node-a's sample after the time landed, while node-b's lane may still hold its batches")
	}
	write(collectedB, at.Add(2*time.Second))
	if !drained() {
		t.Fatal("not drained once every live node's sample probed after the time landed")
	}
}
