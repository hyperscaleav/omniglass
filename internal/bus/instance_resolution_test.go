package bus_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
)

// TestInstanceResolvesTheSameWayInIngestReconciliationAndSettlement is
// ADR-0456 end to end: a node publishes a reading with no instance over the
// real bus, ingest stamps it with the endpoint whose task collected it, and
// the two readers that ask the owner-level question (reconciliation's is, and
// the settlement of an un-instanced command no endpoint carried) resolve that
// same row rather than looking for an instance-less one.
func TestInstanceResolvesTheSameWayInIngestReconciliationAndSettlement(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "secret-a"
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-a", hashHex(token), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('panel-ip', 'tcp', $1::uuid, (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.9:23"}'::jsonb)`, comp.ID); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-panel', 'poll', (select id from endpoint where name = 'panel-ip'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}

	// want hdmi1, owner-level; an un-instanced set-input to hdmi2 that no
	// endpoint carries (no driver binds it), so it is an owner-level intent.
	if _, err := gw.SetProperty(ctx, "", "component", "panel-1", "video-input", "", json.RawMessage(`"hdmi1"`), all, all); err != nil {
		t.Fatalf("declare video-input: %v", err)
	}
	cmd, err := gw.IssueCommand(ctx, actor, "component", comp.ID, "set-input", "", json.RawMessage(`"hdmi2"`), nil)
	if err != nil {
		t.Fatalf("issue set-input: %v", err)
	}

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo("node-a", token), nats.CustomInboxPrefix(collection.InboxPrefix("node-a")))
	if err != nil {
		t.Fatalf("node connect: %v", err)
	}
	defer nc.Close()

	// The node's reading names no instance: ingest stamps the endpoint.
	publishEvent(t, nc, "node-a", &ogv1.TelemetryBatch{
		TaskId:     "t-panel",
		NodeId:     "node-a",
		Properties: []*ogv1.PropertySample{{Name: "video-input", ValueJson: `"hdmi2"`}},
	})
	waitProperty(t, ctx, gw, "panel-1", "video-input", "panel-ip", func(d *storage.PropertySample) bool { return d != nil && d.Value == "hdmi2" })

	// Reconciliation reads that row as is, names its endpoint, and judges it
	// against want.
	recs, err := gw.Reconciliation(ctx, "component", "panel-1", all)
	if err != nil {
		t.Fatalf("reconciliation: %v", err)
	}
	var found bool
	for _, r := range recs {
		if r.PropertyTypeName != "video-input" {
			continue
		}
		found = true
		if string(r.Is) != `"hdmi2"` || r.IsInstance != "panel-ip" || !r.Drift || string(r.Told) != `"hdmi2"` {
			t.Fatalf("video-input pivot: want is=hdmi2 from panel-ip, told=hdmi2, drift; got is=%s from %q told=%s drift=%v",
				r.Is, r.IsInstance, r.Told, r.Drift)
		}
	}
	if !found {
		t.Fatalf("video-input not in the reconciliation: %+v", recs)
	}

	// Settlement resolved the same row: the landing settled the command, and
	// the computed verdict agrees.
	rec, err := gw.GetComponentCommand(ctx, comp.ID, cmd.ID)
	if err != nil {
		t.Fatalf("read command: %v", err)
	}
	if rec.Status != "settled" || rec.SettledBy != storage.SettledByObservation {
		t.Fatalf("command: status %q by %q, want settled by the observation", rec.Status, rec.SettledBy)
	}
	verdict, err := gw.CommandSettlement(ctx, "component", comp.ID, "set-input", "")
	if err != nil {
		t.Fatalf("settlement: %v", err)
	}
	if verdict != storage.SettlementSettled {
		t.Fatalf("computed verdict = %q, want settled", verdict)
	}
}
