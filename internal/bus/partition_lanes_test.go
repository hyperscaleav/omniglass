package bus_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The ingest lanes (#1896), driven through a real JetStream and a real
// Postgres: the stream files each node's batches under a partition of its
// name, each partition has a durable of its own, so one node's slow landing
// holds only its own partition while every other node's batches land.

// laneNodes are two enrolled nodes the stream files under different
// partitions at the default count, each with its connection to srv.
type laneNodes struct {
	f    deliveryFixture
	a, b string
}

// newLaneNodes enrolls two nodes whose names hash to different partitions
// at the default count.
func newLaneNodes(t *testing.T) laneNodes {
	t.Helper()
	f := newDeliveryFixture(t)
	var names []string
	seen := map[int]bool{}
	for i := 0; len(names) < 2; i++ {
		name := fmt.Sprintf("lane-node-%d", i)
		if p := bus.PartitionOf(name, bus.DefaultIngestPartitions); !seen[p] {
			seen[p] = true
			names = append(names, name)
		}
	}
	all := scope.Set{All: true}
	for _, name := range names {
		if _, err := f.gw.CreateNode(f.ctx, "", storage.NodeSpec{Name: name}, all, all); err != nil {
			t.Fatalf("create node %s: %v", name, err)
		}
		if _, err := f.gw.SetEnrollmentToken(f.ctx, "", name, hashHex("token-"+name), all); err != nil {
			t.Fatalf("mint token %s: %v", name, err)
		}
	}
	return laneNodes{f: f, a: names[0], b: names[1]}
}

// connect is node's own connection to srv.
func (l laneNodes) connect(t *testing.T, srv *bus.Server, node string) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo(node, "token-"+node),
		nats.CustomInboxPrefix(collection.InboxPrefix(node)))
	if err != nil {
		t.Fatalf("node %s connect: %v", node, err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// selfLog is a node-lane batch carrying one self-log line, msg.
func selfLog(node, msg string) *ogv1.TelemetryBatch {
	return &ogv1.TelemetryBatch{NodeId: node,
		Logs: []*ogv1.LogLine{{Message: msg, Severity: "info", Ts: timestamppb.Now()}}}
}

// publishAs publishes ev on node's subject under the Nats-Msg-Id id and
// waits for the stream's ack.
func publishAs(t *testing.T, nc *nats.Conn, node string, ev *ogv1.TelemetryBatch, id string) {
	t.Helper()
	b, err := proto.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	if _, err := collection.PublishBatch(context.Background(), js, collection.TelemetrySubject(node), b, id, 5*time.Second); err != nil {
		t.Fatalf("publish batch %s as %s: %v", id, node, err)
	}
}

// waitLog waits until the store holds a self-log line msg.
func (l laneNodes) waitLog(t *testing.T, msg string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var n int
		if err := l.f.conn.QueryRow(l.f.ctx, `select count(*) from node_log where message = $1`, msg).Scan(&n); err != nil {
			t.Fatalf("count log lines: %v", err)
		}
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the self-log %q never landed", msg)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// heldStore holds every landing of node held until another node's batch
// has landed, and says whether it had to give up waiting.
type heldStore struct {
	*storage.PG
	held     string
	entered  chan struct{}
	other    chan struct{}
	enterOne sync.Once
	otherOne sync.Once
	mu       sync.Mutex
	gaveUp   bool
}

func (s *heldStore) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	if b.Node != s.held {
		landed, err := s.PG.LandBatch(ctx, b)
		s.otherOne.Do(func() { close(s.other) })
		return landed, err
	}
	s.enterOne.Do(func() { close(s.entered) })
	select {
	case <-s.other:
	case <-time.After(5 * time.Second):
		s.mu.Lock()
		s.gaveUp = true
		s.mu.Unlock()
	}
	return s.PG.LandBatch(ctx, b)
}

// TestOneNodesSlowLandingHoldsOnlyItsOwnPartition: a node's batch inside a
// landing that waits on another node's batch does not wait out its timeout,
// because the other node's partition lands beside it. One serial lane for
// every node would hold the second batch behind the first.
func TestOneNodesSlowLandingHoldsOnlyItsOwnPartition(t *testing.T) {
	l := newLaneNodes(t)
	store := &heldStore{PG: l.f.gw, held: l.a, entered: make(chan struct{}), other: make(chan struct{})}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, store)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()

	publishAs(t, l.connect(t, srv, l.a), l.a, selfLog(l.a, "held-a"), "held-a")
	select {
	case <-store.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("node a's batch never reached a landing")
	}
	publishAs(t, l.connect(t, srv, l.b), l.b, selfLog(l.b, "free-b"), "free-b")
	l.waitLog(t, "free-b")
	l.waitLog(t, "held-a")
	l.f.waitDrained(t, srv)

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.gaveUp {
		t.Errorf("node %s's batch (partition %d) waited behind node %s's (partition %d)",
			l.b, bus.PartitionOf(l.b, bus.DefaultIngestPartitions), l.a, bus.PartitionOf(l.a, bus.DefaultIngestPartitions))
	}
}

// orderStore records the order each node's self-logs reached a landing.
type orderStore struct {
	*storage.PG
	mu    sync.Mutex
	order map[string][]string
}

func (s *orderStore) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	landed, err := s.PG.LandBatch(ctx, b)
	if err == nil {
		s.mu.Lock()
		for _, w := range b.NodeLogs {
			s.order[b.Node] = append(s.order[b.Node], w.Message)
		}
		s.mu.Unlock()
	}
	return landed, err
}

// TestEachNodesBatchesLandInTheOrderItSentThem: two nodes on different
// partitions interleave their publishes, and each node's batches land in the
// order it sent them, which the property lane's transition guard relies on
// (ADR-0311).
func TestEachNodesBatchesLandInTheOrderItSentThem(t *testing.T) {
	l := newLaneNodes(t)
	store := &orderStore{PG: l.f.gw, order: map[string][]string{}}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, store)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	conns := map[string]*nats.Conn{l.a: l.connect(t, srv, l.a), l.b: l.connect(t, srv, l.b)}
	const n = 20
	for i := range n {
		for _, node := range []string{l.a, l.b} {
			msg := fmt.Sprintf("%s-%02d", node, i)
			publishAs(t, conns[node], node, selfLog(node, msg), msg)
		}
	}
	l.waitLog(t, fmt.Sprintf("%s-%02d", l.a, n-1))
	l.waitLog(t, fmt.Sprintf("%s-%02d", l.b, n-1))
	l.f.waitDrained(t, srv)

	store.mu.Lock()
	defer store.mu.Unlock()
	for _, node := range []string{l.a, l.b} {
		got := store.order[node]
		if len(got) != n {
			t.Fatalf("node %s landed %d batches, want %d: %v", node, len(got), n, got)
		}
		for i, msg := range got {
			if want := fmt.Sprintf("%s-%02d", node, i); msg != want {
				t.Fatalf("node %s's batch %d landed as %q, want %q: %v", node, i, msg, want, got)
			}
		}
	}
}

// TestABatchsReceiptNamesItsNodesOwnSubject: the receipt a node's batch
// leaves is keyed by the node's own subject and its id, not the partition the
// stream filed it under, so a receipt reads the same at any lane count.
func TestABatchsReceiptNamesItsNodesOwnSubject(t *testing.T) {
	l := newLaneNodes(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, l.f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	publishAs(t, l.connect(t, srv, l.a), l.a, selfLog(l.a, "keyed"), "keyed-1")
	l.waitLog(t, "keyed")
	l.f.waitDrained(t, srv)

	want := collection.TelemetrySubject(l.a) + " keyed-1"
	var n int
	if err := l.f.conn.QueryRow(l.f.ctx, `select count(*) from telemetry_receipt where batch_id = $1`, want).Scan(&n); err != nil {
		t.Fatalf("read receipt: %v", err)
	}
	if n != 1 {
		var got []string
		rows, _ := l.f.conn.Query(l.f.ctx, `select batch_id from telemetry_receipt`)
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			got = append(got, id)
		}
		t.Errorf("no receipt keyed %q; the store holds %v", want, got)
	}
}

// TestFewerPartitionsDrainWhatMoreLeftBehind: batches the stream filed
// under four partitions, never taken, land after the server restarts with
// one, so changing the lane count loses nothing the stream held.
func TestFewerPartitionsDrainWhatMoreLeftBehind(t *testing.T) {
	l := newLaneNodes(t)
	storeDir := t.TempDir()
	first, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, StoreDir: storeDir, IngestPartitions: 4}, l.f.gw)
	if err != nil {
		t.Fatalf("start the first bus: %v", err)
	}
	first.StopTelemetryLanes()
	for _, node := range []string{l.a, l.b} {
		publishAs(t, l.connect(t, first, node), node, selfLog(node, "left-"+node), "left-"+node)
	}
	if n, err := first.TelemetryBacklog(l.f.ctx); err != nil || n != 2 {
		t.Fatalf("the stream holds %d batches (%v), want the 2 left behind", n, err)
	}
	first.Shutdown()

	second, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, StoreDir: storeDir, IngestPartitions: 1}, l.f.gw)
	if err != nil {
		t.Fatalf("restart the bus with one partition: %v", err)
	}
	defer second.Shutdown()
	l.waitLog(t, "left-"+l.a)
	l.waitLog(t, "left-"+l.b)
	l.f.waitDrained(t, second)
}

// TestABatchUnderAPastPartitionWithNoDurableLands: a batch a larger count
// filed under a partition past the new count lands though its durable is
// gone, since the start drains every partition the prior count filed under,
// not only those whose durable is left.
func TestABatchUnderAPastPartitionWithNoDurableLands(t *testing.T) {
	l := newLaneNodes(t)
	storeDir := t.TempDir()
	first, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, StoreDir: storeDir, IngestPartitions: 4}, l.f.gw)
	if err != nil {
		t.Fatalf("start the first bus: %v", err)
	}
	first.StopTelemetryLanes()
	for _, node := range []string{l.a, l.b} {
		publishAs(t, l.connect(t, first, node), node, selfLog(node, "orphan-"+node), "orphan-"+node)
	}
	if err := first.DeletePartitionDurables(l.f.ctx); err != nil {
		t.Fatalf("delete the partition durables: %v", err)
	}
	first.Shutdown()

	second, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, StoreDir: storeDir, IngestPartitions: 1}, l.f.gw)
	if err != nil {
		t.Fatalf("restart the bus with one partition: %v", err)
	}
	defer second.Shutdown()
	l.waitLog(t, "orphan-"+l.a)
	l.waitLog(t, "orphan-"+l.b)
	l.f.waitDrained(t, second)
}

// TestABatchStoredBeforeThePartitionsLands: a batch the stream stored under
// the node's own subject, before it filed batches under partitions, lands
// through the durable that keeps that subject, so the first start of a
// partitioned server loses nothing an older one left behind.
func TestABatchStoredBeforeThePartitionsLands(t *testing.T) {
	l := newLaneNodes(t)
	storeDir := t.TempDir()
	first, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, StoreDir: storeDir}, l.f.gw)
	if err != nil {
		t.Fatalf("start the first bus: %v", err)
	}
	first.StopTelemetryLanes()
	if err := first.UnpartitionTelemetryStream(l.f.ctx); err != nil {
		t.Fatalf("drop the stream's partition transform: %v", err)
	}
	publishAs(t, l.connect(t, first, l.a), l.a, selfLog(l.a, "residue"), "residue-1")
	first.Shutdown()

	second, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, StoreDir: storeDir}, l.f.gw)
	if err != nil {
		t.Fatalf("restart the bus: %v", err)
	}
	defer second.Shutdown()
	l.waitLog(t, "residue")
	l.f.waitDrained(t, second)
}

// TestThePushedLaneLandsBesideThePartitions: the API's push lane keeps its
// own subject and lands while the node partitions are in place.
func TestThePushedLaneLandsBesideThePartitions(t *testing.T) {
	f := newDeliveryFixture(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	if err := srv.PublishTelemetry(f.ctx, pushBatch("pushed-beside")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	f.waitLanded(t, "pushed-beside")
	f.waitDrained(t, srv)
}
