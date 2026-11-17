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

// TestOutpostLaneConfinesOutpostProbeKeysToItsEndpoint is point 4 of ADR-0161: a
// outpost writes an outpost-probe key only under the endpoint whose task it ran. On
// the outpost lane the instance of such a key is the task endpoint's name: an
// empty one is filled with it, the endpoint's own name lands, and any other
// (an endpoint of the same component another outpost holds, or one that does not
// exist) is refused rather than written into a series the console reads as
// that endpoint's. A driver's key keeps the instance it names.
func TestOutpostLaneConfinesOutpostProbeKeysToItsEndpoint(t *testing.T) {
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
	for _, n := range []string{"outpost-a", "outpost-b"} {
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: n}, all, all); err != nil {
			t.Fatalf("create outpost %s: %v", n, err)
		}
	}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	// ssh is outpost-a's; panel, on the same component, is outpost-b's.
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, outpost_id, params) values
		('ssh', 'ssh', $1::uuid, (select principal_id from outpost where name = 'outpost-a'), '{"target":"10.0.0.1:22"}'::jsonb),
		('panel', 'tcp', $1::uuid, (select principal_id from outpost where name = 'outpost-b'), '{"target":"10.0.0.2:23"}'::jsonb)`, comp.ID); err != nil {
		t.Fatalf("insert endpoints: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-ssh', 'poll', (select id from endpoint where name = 'ssh'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	owner, ok, err := gw.ResolveTaskOwner(ctx, "t-ssh", "outpost-a")
	if err != nil || !ok {
		t.Fatalf("resolve task owner: ok=%v err=%v", ok, err)
	}
	s := &Server{store: gw}
	land := func(ev *ogv1.TelemetryBatch) {
		t.Helper()
		ev.TaskId, ev.OutpostId = "t-ssh", "outpost-a"
		if err := s.land(ctx, "", ev, ingestBinding{LogOutpost: "outpost-a", SampleOwner: &owner, Arc: componentArc(owner), Writer: storage.OutpostWriter(owner.ComponentID, owner.EndpointName)}); err != nil {
			t.Fatalf("land: %v", err)
		}
	}
	rows := func(table, key, instance string) int {
		t.Helper()
		var n int
		from := table
		if table == "metric" {
			from = storagetest.MetricRows + " metric"
		}
		if err := conn.QueryRow(ctx, `select count(*) from `+from+` where instance = $2
			and `+table+`_type_id = (select id from `+table+`_type where name = $1)`, key, instance).Scan(&n); err != nil {
			t.Fatalf("count %s %s[%s]: %v", table, key, instance, err)
		}
		return n
	}

	// A foreign instance on every lane an outpost-probe key rides: refused.
	land(&ogv1.TelemetryBatch{
		Metrics:    []*ogv1.MetricSample{{Name: "tcp-connect-time", Instance: "panel", Value: 1}},
		Properties: []*ogv1.PropertySample{{Name: "endpoint-reachable", Instance: "panel", ValueJson: `"up"`}, {Name: "session-state", Instance: "nowhere", ValueJson: `"up"`}},
	})
	if n := rows("metric", "tcp-connect-time", "panel"); n != 0 {
		t.Errorf("outpost-a wrote tcp-connect-time under outpost-b's endpoint panel (%d rows)", n)
	}
	if n := rows("property", "endpoint-reachable", "panel"); n != 0 {
		t.Errorf("outpost-a wrote endpoint-reachable under outpost-b's endpoint panel (%d rows)", n)
	}
	if n := rows("property", "session-state", "nowhere"); n != 0 {
		t.Errorf("outpost-a wrote session-state under an instance no endpoint holds (%d rows)", n)
	}

	// Its own endpoint, named or left empty: lands under ssh.
	land(&ogv1.TelemetryBatch{
		Metrics:    []*ogv1.MetricSample{{Name: "tcp-connect-time", Instance: "ssh", Value: 1}},
		Properties: []*ogv1.PropertySample{{Name: "endpoint-reachable", ValueJson: `"up"`}},
	})
	if n := rows("metric", "tcp-connect-time", "ssh"); n != 1 {
		t.Errorf("tcp-connect-time under its own endpoint: %d rows, want 1", n)
	}
	if n := rows("property", "endpoint-reachable", "ssh"); n != 1 {
		t.Errorf("endpoint-reachable with no instance: %d rows under ssh, want 1", n)
	}

	// A driver's key keeps the instance it names.
	land(&ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "uptime", Instance: "if-3", Value: 5}}})
	if n := rows("metric", "uptime", "if-3"); n != 1 {
		t.Errorf("a driver's instanced uptime: %d rows under if-3, want 1", n)
	}
}
