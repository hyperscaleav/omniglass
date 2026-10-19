package bus

import (
	"context"
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

// TestLandStampsEndpointSeen pins an endpoint's freshness to the server's
// receipt of any batch its task delivered (#931, #933), not to a sample's own
// timestamp or series: a driver's instanced series counts, a property-only
// driver whose value never changes (the property lane writes transitions
// only) stays fresh, and a sample dated ahead by the node's clock does not
// carry the stamp into the future.
func TestLandStampsEndpointSeen(t *testing.T) {
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
		('snmp', 'snmp', $1::uuid, (select principal_id from node where name = 'node-a'), '{"target":"10.0.0.1"}'::jsonb)`, comp.ID); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-snmp', 'poll', (select id from endpoint where name = 'snmp'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	owner, ok, err := gw.ResolveTaskOwner(ctx, "t-snmp", "node-a")
	if err != nil || !ok {
		t.Fatalf("resolve task owner: ok=%v err=%v", ok, err)
	}
	s := &Server{store: gw}
	land := func(ev *ogv1.TelemetryBatch) {
		t.Helper()
		ev.TaskId, ev.NodeId = "t-snmp", "node-a"
		if err := s.land(ctx, ev, ingestBinding{SampleOwner: &owner}); err != nil {
			t.Fatalf("land: %v", err)
		}
	}
	seen := func() *time.Time {
		t.Helper()
		eps, err := gw.ListComponentEndpoints(ctx, comp.ID)
		if err != nil || len(eps) != 1 {
			t.Fatalf("endpoints: %v %+v", err, eps)
		}
		return eps[0].LastSeenAt
	}
	age := func() {
		t.Helper()
		if _, err := conn.Exec(ctx, `update endpoint_live set last_seen_at = now() - interval '1 hour'`); err != nil {
			t.Fatalf("age the stamp: %v", err)
		}
	}
	fresh := func(what string) {
		t.Helper()
		got := seen()
		if got == nil || time.Since(*got) > time.Minute || time.Until(*got) > 5*time.Second {
			t.Fatalf("%s: last seen %v, want the server's now", what, got)
		}
	}

	if got := seen(); got != nil {
		t.Fatalf("before any batch: last seen %v, want none", got)
	}

	// A driver's instanced series: the sample's instance is not the endpoint.
	land(&ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "uptime", Instance: "if-3", Value: 1}}})
	fresh("an instanced driver series")

	// A property-only driver whose value never changes: the second batch
	// writes no property row, yet the endpoint delivered it.
	up := &ogv1.PropertySample{Name: "endpoint-reachable", ValueJson: `"up"`}
	land(&ogv1.TelemetryBatch{Properties: []*ogv1.PropertySample{up}})
	age()
	land(&ogv1.TelemetryBatch{Properties: []*ogv1.PropertySample{up}})
	fresh("an unchanged property")

	// A sample dated an hour ahead by the node's clock.
	age()
	land(&ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "uptime", Value: 2, Ts: timestamppb.New(time.Now().Add(time.Hour))}}})
	fresh("a future-dated sample")
}
