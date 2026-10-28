package bus

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
)

// TestACommandTypeCreatedBetweenBatchesSettlesOnTheNext is #1299's acceptance:
// the consumer carries the command-targeted property names on the registry it
// keeps between batches, and a command type created between two batches is
// honored by the second. A batch lands lamp-mode while nothing commands it;
// then a command type targeting lamp-mode is created and a command issued, and
// the next batch's lamp-mode observation settles it. A registry kept past the
// command type's creation would not know lamp-mode is commanded, and the
// command would wait out its window instead.
func TestACommandTypeCreatedBetweenBatchesSettlesOnTheNext(t *testing.T) {
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
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "projector-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('pj-ip', 'tcp', $1::uuid, (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.7:4352"}'::jsonb)`, comp.ID); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-pj', 'poll', (select id from endpoint where name = 'pj-ip'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "lamp-mode", Label: "Lamp mode", DataType: "string"}); err != nil {
		t.Fatalf("property type: %v", err)
	}
	owner, ok, err := gw.ResolveTaskOwner(ctx, "t-pj", "node-a")
	if err != nil || !ok {
		t.Fatalf("resolve task owner: ok=%v err=%v", ok, err)
	}
	s := &Server{store: gw, orphans: newOrphanTally(orphanWindow)}
	land := func(id, mode string) {
		t.Helper()
		if err := s.land(ctx, id, &ogv1.TelemetryBatch{TaskId: "t-pj", NodeId: "node-a",
			Properties: []*ogv1.PropertySample{{Name: "lamp-mode", ValueJson: `"` + mode + `"`}}},
			ingestBinding{LogNode: "node-a", SampleOwner: &owner, Arc: componentArc(owner),
				Writer: storage.NodeWriter(owner.ComponentID, owner.EndpointName)}); err != nil {
			t.Fatalf("land %s: %v", id, err)
		}
	}

	// Nothing commands lamp-mode yet; the registry is built and kept.
	land("b-1", "normal")

	if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{Name: "set-lamp-mode", Label: "Set lamp mode",
		TargetPropertyType: "lamp-mode", SettleWindowSeconds: 3600}); err != nil {
		t.Fatalf("command type: %v", err)
	}
	cmd, err := gw.IssueCommand(ctx, actor, "component", comp.ID, "set-lamp-mode", "", json.RawMessage(`"eco"`), nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	land("b-2", "eco")
	rec, err := gw.GetComponentCommand(ctx, comp.ID, cmd.ID)
	if err != nil {
		t.Fatalf("read command: %v", err)
	}
	if rec.Status != "settled" || rec.SettledBy != storage.SettledByObservation {
		t.Fatalf("the next batch's eco: command %q by %q, want settled by the observation", rec.Status, rec.SettledBy)
	}
}
