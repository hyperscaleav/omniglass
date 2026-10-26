package bus

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A component's log lines from the node lane (#1145): a task's collected
// lines ride task_logs and land in log_line on the task's component, the
// collecting endpoint as their instance, never in node_log; the node's own
// self-logs in the same batch still land in node_log. The severity table and
// the caps every writer shares are the Storage Gateway's (logs_test.go).

func TestTaskLogWritesLandOnTheTasksComponent(t *testing.T) {
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	logs := taskLogWrites(&ogv1.TelemetryBatch{
		TaskLogs: []*ogv1.LogLine{
			{Message: "backlight driver reset", Severity: "WARN", Facility: "panel", Source: "nvp", Attributes: []byte(`{"zone":"front"}`), Ts: timestamppb.New(at)},
			{Message: "EVT SCENE presentation"},
		},
	}, nodeOwner())
	if len(logs) != 2 {
		t.Fatalf("got %d writes, want 2", len(logs))
	}
	first := logs[0]
	if first.OwnerKind != "component" || first.OwnerID != "disp-1" || first.Instance != "api" {
		t.Fatalf("owner = %s/%s instance %q, want the task's component and its endpoint", first.OwnerKind, first.OwnerID, first.Instance)
	}
	if first.Severity != "WARN" || first.Facility != "panel" || first.Source != "nvp" || !first.TS.Equal(at) {
		t.Fatalf("first = %+v", first)
	}
	if logs[1].Source != "icmp" {
		t.Fatalf("a line with no source took %q, want the endpoint's transport", logs[1].Source)
	}
}

// TestTaskLogsCountAsTaskBound: a batch carrying only a task's log lines
// still resolves its task's owner, unlike a logs-only self-log batch.
func TestTaskLogsCountAsTaskBound(t *testing.T) {
	if taskBound(&ogv1.TelemetryBatch{Logs: []*ogv1.LogLine{{Message: "self"}}}) != 0 {
		t.Fatal("a self-log batch counted as task-bound")
	}
	if taskBound(&ogv1.TelemetryBatch{TaskLogs: []*ogv1.LogLine{{Message: "m"}}}) != 1 {
		t.Fatal("a task-log batch did not count as task-bound")
	}
}

func TestLandFilesTaskLogsAndSelfLogsApart(t *testing.T) {
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
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('tcp', 'tcp', $1::uuid, (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.1:51325"}'::jsonb)`, comp.ID); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-nvp', 'listen', (select id from endpoint where name = 'tcp'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	owner, ok, err := gw.ResolveTaskOwner(ctx, "t-nvp", "node-a")
	if err != nil || !ok {
		t.Fatalf("resolve task owner: ok=%v err=%v", ok, err)
	}
	s := &Server{store: gw}
	at := time.Now().UTC().Truncate(time.Second)
	ev := &ogv1.TelemetryBatch{
		TaskId: "t-nvp", NodeId: "node-a",
		Logs:     []*ogv1.LogLine{{Message: "worklist pulled", Source: "node"}},
		TaskLogs: []*ogv1.LogLine{{Message: "backlight driver reset", Severity: "warn", Facility: "panel", Ts: timestamppb.New(at)}},
	}
	if err := s.land(ctx, "", ev, ingestBinding{LogNode: "node-a", SampleOwner: &owner, Writer: storage.NodeWriter(owner.ComponentID, owner.EndpointName)}); err != nil {
		t.Fatalf("land: %v", err)
	}
	lines, err := gw.ListComponentLogs(ctx, comp.ID, time.Hour, 10, storage.LogFilter{})
	if err != nil {
		t.Fatalf("list component logs: %v", err)
	}
	if len(lines) != 1 || lines[0].Message != "backlight driver reset" || lines[0].Severity != "warning" || lines[0].Instance != "tcp" || !lines[0].TS.Equal(at) {
		t.Fatalf("component log lines = %+v, want the task's one line on its component", lines)
	}
	if lines[0].ReceivedAt == nil || time.Since(*lines[0].ReceivedAt) > time.Minute {
		t.Fatalf("received_at = %v, want the server's receipt", lines[0].ReceivedAt)
	}
	self, err := gw.ListNodeLogs(ctx, "node-a", time.Hour, 10)
	if err != nil {
		t.Fatalf("list node logs: %v", err)
	}
	if len(self) != 1 || self[0].Message != "worklist pulled" {
		t.Fatalf("node logs = %+v, want the self-log alone", self)
	}

	// An orphan batch (its task not this node's) lands its task logs nowhere.
	if err := s.land(ctx, "", &ogv1.TelemetryBatch{TaskId: "t-other", TaskLogs: []*ogv1.LogLine{{Message: "stranger"}}}, ingestBinding{LogNode: "node-a"}); err != nil {
		t.Fatalf("land orphan: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from log_line where message = 'stranger'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("orphan task logs landed %d (%v)", n, err)
	}
	var attrs []byte
	_ = conn.QueryRow(ctx, `select attributes from log_line where message = 'backlight driver reset'`).Scan(&attrs)
	if len(attrs) > 0 {
		var m map[string]any
		if json.Unmarshal(attrs, &m) != nil {
			t.Fatalf("attributes are not an object: %s", attrs)
		}
	}
}
