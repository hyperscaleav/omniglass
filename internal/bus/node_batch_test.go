package bus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

// One tick of a node lands as one batch of task groups (#1582): the handler
// resolves every group's task in one read, lands every group in one
// LandBatch, and stamps every landed group's endpoint in one write. A group
// the gateway refuses, or one the confinement fence orphans, drops alone.

// tickStore is the real gateway, counted at the three calls a tick costs, and
// able to stand in for a gateway that refuses one task's group.
type tickStore struct {
	*storage.PG
	mu         sync.Mutex
	resolves   int
	lands      []storage.IngestBatch
	stamps     [][]string
	refuseTask string
}

func (s *tickStore) ResolveTaskOwners(ctx context.Context, node string, ids []string) (map[string]storage.TaskOwner, error) {
	s.mu.Lock()
	s.resolves++
	s.mu.Unlock()
	return s.PG.ResolveTaskOwners(ctx, node, ids)
}

// LandBatch records the batch and, when a task is to be refused, lands the
// batch without that task's group and reports it refused at its index, as
// the gateway does for a group one of whose rows it refuses.
func (s *tickStore) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	s.mu.Lock()
	s.lands = append(s.lands, b)
	refuse := s.refuseTask
	s.mu.Unlock()
	var refused []storage.GroupRefusal
	if refuse != "" {
		kept := b.Groups[:0:0]
		for i, g := range b.Groups {
			if g.TaskID == refuse {
				refused = append(refused, storage.GroupRefusal{Index: i, TaskID: g.TaskID,
					Err: fmt.Errorf("group refused: %w", storage.ErrKeyWriterRefused)})
				continue
			}
			kept = append(kept, g)
		}
		b.Groups = kept
	}
	res, err := s.PG.LandBatch(ctx, b)
	res.Refused = append(res.Refused, refused...)
	return res, err
}

func (s *tickStore) StampEndpointsSeen(ctx context.Context, ids []string) error {
	s.mu.Lock()
	s.stamps = append(s.stamps, slices.Clone(ids))
	s.mu.Unlock()
	return s.PG.StampEndpointsSeen(ctx, ids)
}

// tickFixture is node-a holding three tasks, each on its own component's
// endpoint, and node-b holding one node-a must not reach.
type tickFixture struct {
	ctx       context.Context
	dsn       string
	gw        *storage.PG
	store     *tickStore
	srv       *Server
	endpoints map[string]string // endpoint name -> id
	seq       int
}

func newTickFixture(t *testing.T) *tickFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	for _, n := range []string{"node-a", "node-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: n}, all, all); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []string{"disp-1", "disp-2", "disp-3", "disp-b"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `
		insert into endpoint (name, transport, component, node_name, params)
		select c.name || '-tcp', 'tcp', c.id, n.principal_id, '{"target":"127.0.0.1:1"}'::jsonb
		from component c join node n on n.name = case when c.name = 'disp-b' then 'node-b' else 'node-a' end
		where c.name like 'disp-%';
		insert into task (id, mode, endpoint_id, enabled)
		select 't-' || substr(e.name, 6, 1), 'poll', e.id, true from endpoint e`); err != nil {
		t.Fatalf("endpoints and tasks: %v", err)
	}
	f := &tickFixture{ctx: ctx, dsn: dsn, gw: gw, endpoints: map[string]string{}}
	rows, err := conn.Query(ctx, `select name, id::text from endpoint`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name, id string
		if err := rows.Scan(&name, &id); err != nil {
			t.Fatal(err)
		}
		f.endpoints[name] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	f.store = &tickStore{PG: gw}
	f.srv = &Server{store: f.store, orphans: newOrphanTally(orphanWindow)}
	return f
}

// group is one task's share of a tick: a distinct tcp-connect-time value (the probe timing that stays on the metric lane after #1592) and an
// endpoint verdict, so each group's rows read back apart.
func group(task string, v float64) *ogv1.TaskGroup {
	return &ogv1.TaskGroup{TaskId: task,
		Metrics:    []*ogv1.MetricSample{{Name: "tcp-connect-time", Value: v}},
		Properties: []*ogv1.PropertySample{{Name: "endpoint-reachable", ValueJson: `"up"`}},
	}
}

// asTick is the node tick a one-task batch is (#1582): the node and its
// self-logs stay on the tick, and the task with its ts and every row it owns
// becomes the tick's one group. It lets a test written about one task's rows
// publish them the way a node does.
func asTick(ev *ogv1.TelemetryBatch) *ogv1.TelemetryBatch {
	return &ogv1.TelemetryBatch{NodeId: ev.GetNodeId(), Logs: ev.GetLogs(), Groups: []*ogv1.TaskGroup{{
		TaskId:     ev.GetTaskId(),
		Ts:         ev.GetTs(),
		Metrics:    ev.GetMetrics(),
		Properties: ev.GetProperties(),
		Events:     ev.GetEvents(),
		TaskLogs:   ev.GetTaskLogs(),
	}}}
}

// deliver hands node-a's batch to the real handler as JetStream would.
func (f *tickFixture) deliver(t *testing.T, ev *ogv1.TelemetryBatch) *deliveredMsg {
	t.Helper()
	data, err := proto.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	f.seq++
	m := &deliveredMsg{subject: collection.TelemetrySubject("node-a"), data: data,
		header: nats.Header{jetstream.MsgIDHeader: []string{fmt.Sprintf("tick-%d", f.seq)}}}
	f.srv.handleTelemetry(m)
	return m
}

func (f *tickFixture) tcpConnectTime(t *testing.T, comp string) *storage.MetricSample {
	t.Helper()
	got, err := f.gw.LatestMetricInstance(f.ctx, comp, "tcp-connect-time", comp+"-tcp")
	if err != nil {
		t.Fatalf("latest %s tcp-connect-time: %v", comp, err)
	}
	return got
}

func acked(t *testing.T, m *deliveredMsg) {
	t.Helper()
	if !m.acked || m.termed || m.naked {
		t.Fatalf("acked=%v termed=%v naked=%v, want the batch acked", m.acked, m.termed, m.naked)
	}
}

func TestNodeTickLandsAsOneBatch(t *testing.T) {
	f := newTickFixture(t)
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{NodeId: "node-a",
		Groups: []*ogv1.TaskGroup{group("t-1", 1), group("t-2", 0), group("t-3", 1)}}))

	if f.store.resolves != 1 {
		t.Errorf("owner resolutions = %d, want one for the whole tick", f.store.resolves)
	}
	if len(f.store.lands) != 1 || len(f.store.lands[0].Groups) != 3 {
		t.Fatalf("LandBatch calls = %d, want one carrying 3 groups: %+v", len(f.store.lands), f.store.lands)
	}
	for i, g := range f.store.lands[0].Groups {
		if want := fmt.Sprintf("t-%d", i+1); g.TaskID != want || len(g.Metrics) != 1 || len(g.Properties) != 1 {
			t.Errorf("group %d = task %q, %d metrics, %d properties; want %s with one of each", i, g.TaskID, len(g.Metrics), len(g.Properties), want)
		}
		if g.Writer.Node == "" {
			t.Errorf("group %d writer names no node: %+v", i, g.Writer)
		}
	}
	for comp, want := range map[string]float64{"disp-1": 1, "disp-2": 0, "disp-3": 1} {
		if got := f.tcpConnectTime(t, comp); got == nil || got.Value != want {
			t.Errorf("%s tcp-connect-time under %s-tcp = %+v, want %v", comp, comp, got, want)
		}
	}
	if len(f.store.stamps) != 1 {
		t.Fatalf("seen stamps = %d calls, want one for the tick: %v", len(f.store.stamps), f.store.stamps)
	}
	got := slices.Sorted(slices.Values(f.store.stamps[0]))
	want := slices.Sorted(slices.Values([]string{f.endpoints["disp-1-tcp"], f.endpoints["disp-2-tcp"], f.endpoints["disp-3-tcp"]}))
	if !slices.Equal(got, want) {
		t.Errorf("stamped %v, want every group's endpoint %v", got, want)
	}
}

// captureWarnings routes the default logger to a buffer for the test.
func captureWarnings(t *testing.T) func(msg string) []map[string]any {
	t.Helper()
	var mu sync.Mutex
	var buf bytes.Buffer
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	}), nil)))
	t.Cleanup(func() { slog.SetDefault(prior) })
	return func(msg string) []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		var out []map[string]any
		for _, line := range strings.Split(buf.String(), "\n") {
			var r map[string]any
			if json.Unmarshal([]byte(line), &r) == nil && r["msg"] == msg {
				out = append(out, r)
			}
		}
		return out
	}
}

type writerFunc func([]byte) (int, error)

func (w writerFunc) Write(p []byte) (int, error) { return w(p) }

func TestNodeBatchRefusedGroupAcks(t *testing.T) {
	f := newTickFixture(t)
	f.store.refuseTask = "t-2"
	logs := captureWarnings(t)
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{NodeId: "node-a",
		Groups: []*ogv1.TaskGroup{group("t-1", 1), group("t-2", 1), group("t-3", 1)}}))

	if got := f.tcpConnectTime(t, "disp-1"); got == nil || got.Value != 1 {
		t.Errorf("disp-1 tcp-connect-time = %+v, want its group landed beside the refused one", got)
	}
	if got := f.tcpConnectTime(t, "disp-3"); got == nil || got.Value != 1 {
		t.Errorf("disp-3 tcp-connect-time = %+v, want its group landed beside the refused one", got)
	}
	if got := f.tcpConnectTime(t, "disp-2"); got != nil {
		t.Errorf("disp-2 tcp-connect-time = %+v, want nothing: its group was refused", got)
	}
	warned := logs(refusedGroupLog)
	if len(warned) != 1 || warned[0]["task"] != "t-2" || !strings.Contains(fmt.Sprint(warned[0]["error"]), "writer class") {
		t.Errorf("refusal warnings = %v, want one naming t-2 and its cause", warned)
	}
	if len(f.store.stamps) != 1 || slices.Contains(f.store.stamps[0], f.endpoints["disp-2-tcp"]) || len(f.store.stamps[0]) != 2 {
		t.Errorf("seen stamps = %v, want one call stamping the two landed groups' endpoints only", f.store.stamps)
	}
}

func TestNodeBatchTopLevelTaskSamplesAreTermed(t *testing.T) {
	f := newTickFixture(t)
	for name, ev := range map[string]*ogv1.TelemetryBatch{
		"task id":    {NodeId: "node-a", TaskId: "t-1", Groups: []*ogv1.TaskGroup{group("t-1", 1)}},
		"metrics":    {NodeId: "node-a", Metrics: []*ogv1.MetricSample{{Name: "tcp-connect-time", Value: 1}}},
		"properties": {NodeId: "node-a", Properties: []*ogv1.PropertySample{{Name: "endpoint-reachable", ValueJson: `"up"`}}},
		"events":     {NodeId: "node-a", Events: []*ogv1.EventSample{{Name: "call-started", Message: "x"}}},
		"task logs":  {NodeId: "node-a", TaskLogs: []*ogv1.LogLine{{Message: "x"}}},
	} {
		m := f.deliver(t, ev)
		if !m.termed || m.acked || m.naked {
			t.Errorf("%s at the top level: acked=%v termed=%v naked=%v, want termed", name, m.acked, m.termed, m.naked)
		}
	}
	if len(f.store.lands) != 0 {
		t.Fatalf("a termed batch reached LandBatch: %+v", f.store.lands)
	}
	if got := f.tcpConnectTime(t, "disp-1"); got != nil {
		t.Fatalf("disp-1 tcp-connect-time = %+v, want nothing from a termed batch", got)
	}
	// A node's self-logs ride the top level and still land.
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{NodeId: "node-a", Logs: []*ogv1.LogLine{{Message: "self"}}}))
}

func TestNodeBatchOrphanGroupDoesNotBlockSiblings(t *testing.T) {
	f := newTickFixture(t)
	acked(t, f.deliver(t, &ogv1.TelemetryBatch{NodeId: "node-a",
		Groups: []*ogv1.TaskGroup{group("t-1", 1), group("t-b", 1), group("t-3", 1)}}))

	for _, comp := range []string{"disp-1", "disp-3"} {
		if got := f.tcpConnectTime(t, comp); got == nil {
			t.Errorf("%s tcp-connect-time missing: an orphan sibling blocked its group", comp)
		}
	}
	if got, err := f.gw.LatestMetric(f.ctx, "disp-b", "tcp-connect-time"); err != nil || got != nil {
		t.Errorf("disp-b tcp-connect-time = %+v (%v), want none: node-b's task is not node-a's to write", got, err)
	}
	if n := f.srv.orphans.drain()["node-a"].Batches; n != 1 {
		t.Errorf("orphans counted on node-a = %d, want the one orphaned group", n)
	}
	if len(f.store.lands) != 1 || len(f.store.lands[0].Groups) != 2 {
		t.Errorf("LandBatch = %+v, want one call with the two resolved groups", f.store.lands)
	}
	if len(f.store.stamps) != 1 || len(f.store.stamps[0]) != 2 || slices.Contains(f.store.stamps[0], f.endpoints["disp-b-tcp"]) {
		t.Errorf("seen stamps = %v, want the two resolved groups' endpoints", f.store.stamps)
	}
}

// A group Postgres itself refuses (a task log line whose attributes are not
// JSON passes the bus untouched and fails its jsonb cast) drops alone on the
// real gateway: the tick acks, its sibling's rows land under the tick's one
// receipt, and only the sibling's endpoint is stamped seen.
func TestNodeBatchPostgresRefusedGroupAcks(t *testing.T) {
	f := newTickFixture(t)
	logs := captureWarnings(t)
	bad := group("t-2", 1)
	bad.TaskLogs = []*ogv1.LogLine{{Message: "codec reset", Attributes: []byte("{not json")}}
	m := f.deliver(t, &ogv1.TelemetryBatch{NodeId: "node-a", Groups: []*ogv1.TaskGroup{group("t-1", 1), bad}})
	acked(t, m)

	if got := f.tcpConnectTime(t, "disp-1"); got == nil || got.Value != 1 {
		t.Errorf("disp-1 tcp-connect-time = %+v, want its group landed beside the refused one", got)
	}
	if got := f.tcpConnectTime(t, "disp-2"); got != nil {
		t.Errorf("disp-2 tcp-connect-time = %+v, want nothing: its group was refused", got)
	}
	if warned := logs(refusedGroupLog); len(warned) != 1 || warned[0]["task"] != "t-2" {
		t.Errorf("refusal warnings = %v, want one naming t-2", warned)
	}
	conn, err := pgx.Connect(f.ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(f.ctx)
	var receipts int
	if err := conn.QueryRow(f.ctx, `select count(*) from telemetry_receipt where batch_id = $1`,
		f.srv.batchID(m)).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Errorf("receipts for the tick = %d, want one", receipts)
	}
	if len(f.store.stamps) != 1 || !slices.Equal(f.store.stamps[0], []string{f.endpoints["disp-1-tcp"]}) {
		t.Errorf("seen stamps = %v, want one call stamping disp-1-tcp only", f.store.stamps)
	}
}
