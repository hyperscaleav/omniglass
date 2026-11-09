package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestProbeStateLandsOnChangeOnly pins the probe states' move to the property
// lane (#1592): a node's reachability result (icmp-reachable up/down, tcp-open
// open/closed) is a transition-only property, so a steady endpoint probed
// every tick writes one row for its state while its timing companion
// (icmp-rtt-avg, tcp-connect-time) lands a metric row every tick; a flip
// writes the state once more; the latest read serves the current word; and
// the endpoint's liveness stamp still advances on a batch whose state the
// guard did not write, since the stamp is the receipt, not a sample row.
func TestProbeStateLandsOnChangeOnly(t *testing.T) {
	t.Parallel()
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
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-1"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	var telnetID string
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('icmp', 'icmp', $1::uuid, (select principal_id from node where name = 'edge-1'), '{"target":"10.0.0.1"}'::jsonb)`, comp.ID); err != nil {
		t.Fatalf("icmp endpoint: %v", err)
	}
	if err := conn.QueryRow(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('telnet', 'telnet', $1::uuid, (select principal_id from node where name = 'edge-1'), '{"target":"10.0.0.1:23"}'::jsonb) returning id`, comp.ID).Scan(&telnetID); err != nil {
		t.Fatalf("telnet endpoint: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-10 * time.Minute)
	tick := 0
	// probe lands one tick of an endpoint's probe as the node's batch carries
	// it: the state in the property lane, the timing in the metric lane.
	probe := func(endpoint, stateKey, state, timingKey string, ms float64) {
		t.Helper()
		tick++
		at := base.Add(time.Duration(tick) * 30 * time.Second)
		b := storage.IngestBatch{
			BatchID: endpoint + "-" + at.Format(time.RFC3339Nano),
			Writer:  storage.NodeWriter(comp.ID, endpoint),
			Properties: []storage.PropertySampleWrite{{
				OwnerKind: "component", OwnerID: comp.ID, Key: stateKey, Instance: endpoint, Value: state, Source: "node", TS: at,
			}},
			Metrics: []storage.MetricSampleWrite{{
				OwnerKind: "component", OwnerID: comp.ID, Key: timingKey, Instance: endpoint, Value: ms, Source: "node", TS: at,
			}},
		}
		if _, err := gw.LandBatch(ctx, b); err != nil {
			t.Fatalf("tick %d (%s %s=%s): %v", tick, endpoint, stateKey, state, err)
		}
	}
	rows := func(key string) (properties, metrics int) {
		t.Helper()
		if err := conn.QueryRow(ctx, `select
			(select count(*) from property p join property_type pt on pt.id = p.property_type_id where pt.name = $1 and p.provenance = 'observed'),
			(select count(*) from `+storagetest.MetricRows+` m join metric_type mt on mt.id = m.metric_type_id where mt.name = $2)`, key, timingOf[key]).Scan(&properties, &metrics); err != nil {
			t.Fatalf("count %s: %v", key, err)
		}
		return properties, metrics
	}

	// Five steady pings: one state row, five timing rows.
	for i := 0; i < 5; i++ {
		probe("icmp", "icmp-reachable", "up", "icmp-rtt-avg", 1.5)
	}
	if p, m := rows("icmp-reachable"); p != 1 || m != 5 {
		t.Fatalf("five steady pings: %d state rows and %d timing rows, want 1 and 5", p, m)
	}
	// The host drops: one more state row, and a steady down writes nothing.
	probe("icmp", "icmp-reachable", "down", "icmp-rtt-avg", 0)
	probe("icmp", "icmp-reachable", "down", "icmp-rtt-avg", 0)
	if p, m := rows("icmp-reachable"); p != 2 || m != 7 {
		t.Fatalf("after the drop and a steady down: %d state rows and %d timing rows, want 2 and 7", p, m)
	}
	got, err := gw.LatestProperty(ctx, comp.ID, "icmp-reachable", "icmp")
	if err != nil || got == nil || got.Value != "down" {
		t.Fatalf("latest icmp-reachable on icmp: %+v, %v; want down", got, err)
	}

	// The port, on its own endpoint and instance: the same guard, its own words.
	for i := 0; i < 3; i++ {
		probe("telnet", "tcp-open", "open", "tcp-connect-time", 2.1)
	}
	probe("telnet", "tcp-open", "closed", "tcp-connect-time", 0)
	if p, m := rows("tcp-open"); p != 2 || m != 4 {
		t.Fatalf("three open then closed: %d state rows and %d timing rows, want 2 and 4", p, m)
	}
	got, err = gw.LatestProperty(ctx, comp.ID, "tcp-open", "telnet")
	if err != nil || got == nil || got.Value != "closed" {
		t.Fatalf("latest tcp-open on telnet: %+v, %v; want closed", got, err)
	}

	// Liveness reads the receipt, not the rows: a steady state the guard
	// skipped still advances the endpoint's last_seen_at.
	if err := gw.StampEndpointsSeen(ctx, []string{telnetID}); err != nil {
		t.Fatalf("first stamp: %v", err)
	}
	if _, err := conn.Exec(ctx, `update endpoint_live set last_seen_at = now() - interval '1 minute' where endpoint_id = $1::uuid`, telnetID); err != nil {
		t.Fatalf("age the stamp: %v", err)
	}
	probe("telnet", "tcp-open", "closed", "tcp-connect-time", 0)
	if p, _ := rows("tcp-open"); p != 2 {
		t.Fatalf("a steady closed wrote a state row: %d rows, want 2", p)
	}
	if err := gw.StampEndpointsSeen(ctx, []string{telnetID}); err != nil {
		t.Fatalf("stamp after the steady batch: %v", err)
	}
	var seen time.Time
	if err := conn.QueryRow(ctx, `select last_seen_at from endpoint_live where endpoint_id = $1::uuid`, telnetID).Scan(&seen); err != nil {
		t.Fatalf("read the stamp: %v", err)
	}
	if time.Since(seen) > 30*time.Second {
		t.Fatalf("the steady batch did not advance last_seen_at: %s ago", time.Since(seen))
	}
}

// timingOf pairs each probe state with the timing metric that rides beside it.
var timingOf = map[string]string{"icmp-reachable": "icmp-rtt-avg", "tcp-open": "tcp-connect-time"}
