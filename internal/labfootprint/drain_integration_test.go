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
// A node that heartbeated within the live window before the time is waited
// for though it has not heartbeated since, and a node silent for longer than
// the window is not.
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
	nodeC, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-c"}, all, all)
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
		if err := conn.QueryRow(ctx, drainedQuery(at, liveWindow)).Scan(&flag, &epoch); err != nil {
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
	collectedC := storage.NodeWriter(comp.ID, "ssh")
	collectedC.Node = nodeC.PrincipalID

	write(collected, at)
	write(collected, at.Add(-time.Second))
	write(collectedB, at.Add(-time.Second))
	write(collectedSilent, at.Add(-time.Second))
	write(collectedC, at.Add(-time.Second))
	// node-a and node-b heartbeated after the time (a heartbeat reaches the
	// server outside the lanes); node-c last heartbeated just before it, inside
	// the live window, as a node whose next heartbeat is not due yet does;
	// node-silent last spoke longer than the window before it.
	for _, name := range []string{"node-a", "node-b"} {
		if err := gw.RecordHeartbeat(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	for name, hb := range map[string]time.Time{"node-c": at.Add(-time.Second), "node-silent": at.Add(-liveWindow - time.Second)} {
		if _, err := conn.Exec(ctx, `update node set last_heartbeat_at = $1 where name = $2`, hb, name); err != nil {
			t.Fatal(err)
		}
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
	if drained() {
		t.Fatal("drained before node-c's sample after the time landed, though node-c heartbeated inside the live window")
	}
	write(collectedC, at.Add(3*time.Second))
	if !drained() {
		t.Fatal("not drained once every live node's sample probed after the time landed")
	}
}

// A node that collected in an earlier run and now heartbeats with no worklist
// (its latest worklist-task-count is 0, as a node refused its first worklist
// reports while it waits, ADR-1882) runs no task, so it lands no sample and
// does not hold the drain; the measure goes on to read its pull loss. Once it
// installs a worklist it is waited for again (#2008).
func TestIntegrationDrainedQueryPassesOverANodeWithNoWorklist(t *testing.T) {
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
	busy, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-busy"}, all, all)
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-waiting"}, all, all)
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
		if err := conn.QueryRow(ctx, drainedQuery(at, liveWindow)).Scan(&flag, &epoch); err != nil {
			t.Fatal(err)
		}
		ok, _, err := parseDrained(flag + "|" + epoch)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	collect := func(nodeID string, ts time.Time) {
		t.Helper()
		w := storage.NodeWriter(comp.ID, "ssh")
		w.Node = nodeID
		if err := gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: comp.ID,
			Key: "tcp-connect-time", Instance: "ssh", Value: 1, TS: ts}}); err != nil {
			t.Fatal(err)
		}
	}
	// installed lands a worklist-task-count on the node's platform component
	// with no collector, as the node-self report lands under the platform
	// writer.
	installed := func(node string, tasks float64, ts time.Time) {
		t.Helper()
		if _, err := conn.Exec(ctx, storagetest.WideMetricInsert("ts, owner_kind, component_id, metric_type_id, value",
			`select $1::timestamptz, 'component', n.platform_component_id, mt.id, $3::float8
			from node n, metric_type mt where n.name = $2 and mt.name = 'worklist-task-count'`),
			ts, node, tasks); err != nil {
			t.Fatal(err)
		}
	}
	var platform *string
	if err := conn.QueryRow(ctx, `select platform_component_id::text from node where name = 'node-waiting'`).Scan(&platform); err != nil || platform == nil {
		t.Fatalf("node-waiting has no platform component to report on: %v", err)
	}

	// Both collected in an earlier run and heartbeat now; node-waiting was
	// restarted and is refused its first worklist, reporting none, after it
	// once ran tasks.
	collect(busy.PrincipalID, at.Add(-time.Second))
	collect(waiting.PrincipalID, at.Add(-time.Second))
	for _, name := range []string{"node-busy", "node-waiting"} {
		if err := gw.RecordHeartbeat(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	installed("node-busy", 4, at.Add(-time.Hour))
	installed("node-waiting", 4, at.Add(-time.Hour))
	installed("node-waiting", 0, at.Add(-time.Second))
	collect(busy.PrincipalID, at.Add(time.Second))
	if !drained() {
		t.Fatal("not drained while a node with no worklist, which can land no sample, held it")
	}

	// node-waiting installs its worklist: it runs tasks again, so its lane is
	// waited for until it lands a sample past the time.
	installed("node-waiting", 4, at.Add(2*time.Second))
	if drained() {
		t.Fatal("drained before a node that installed its worklist landed a sample past the time")
	}
	collect(waiting.PrincipalID, at.Add(3*time.Second))
	if !drained() {
		t.Fatal("not drained once every node with a worklist landed a sample past the time")
	}
}
