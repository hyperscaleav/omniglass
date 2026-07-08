package bus_test

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
)

// TestTelemetryRoundTrip is the checkpoint-3 closing gate: a node runs a REAL tcp
// probe against a live listener, ships the result as a protobuf Event over
// JetStream, and the datapoint lands in metric_datapoint owned (server-side) by
// the target component. It then proves the two invariants are real, not faked:
// (a) reject-not-project drops an unregistered datapoint name, and (b) the
// confinement fence drops an Event whose task belongs to ANOTHER node. Both are
// asserted structurally by a watermark: a later valid datapoint proves the
// consumer drained past the negatives, so their absence is a real drop.
func TestTelemetryRoundTrip(t *testing.T) {
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

	// A live listener is the probe's open target; capture its address.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	target := ln.Addr().String()

	// Enroll node-a and node-b.
	for _, name := range []string{"node-a", "node-b"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: name}, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	tokenA := "secret-a"
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-a", hashHex(tokenA), all); err != nil {
		t.Fatalf("mint a: %v", err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-b", hashHex("secret-b"), all); err != nil {
		t.Fatalf("mint b: %v", err)
	}

	// Components + interfaces + tasks: disp-1 bound to node-a (the happy path);
	// disp-2 bound to node-b (the confinement target node-a must not reach).
	for _, name := range []string{"disp-1", "disp-2"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, ComponentType: "display"}, all); err != nil {
			t.Fatalf("create component %s: %v", name, err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, node_name, params) values
		('disp-1-tcp', 'tcp', 'disp-1', 'node-a', $1::jsonb),
		('disp-2-tcp', 'tcp', 'disp-2', 'node-b', '{"target":"127.0.0.1:1"}'::jsonb)`,
		`{"target":"`+target+`"}`); err != nil {
		t.Fatalf("insert interfaces: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_name, node_name, enabled) values
		('t-a', 'poll', 'disp-1-tcp', 'node-a', true),
		('t-b', 'poll', 'disp-2-tcp', 'node-b', true)`); err != nil {
		t.Fatalf("insert tasks: %v", err)
	}
	conn.Close(ctx)

	// Register the interface_type so the interface FK is satisfiable was already
	// handled at seed; start the bus + an API server that advertises it.
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()

	// HAPPY PATH: run node-a once with the REAL dialer. It pulls t-a, probes the
	// live listener, and publishes the Event; the consumer binds owner disp-1 and
	// writes tcp.open=1.
	if _, err := node.Run(ctx, node.Config{ServerURL: apiSrv.URL, Name: "node-a", Token: tokenA, Once: true}); err != nil {
		t.Fatalf("node run: %v", err)
	}
	dp := waitMetric(t, ctx, gw, "disp-1", "tcp.open", func(d *storage.MetricDatapoint) bool { return d != nil && d.Value == 1 })
	if dp.OwnerKind != "component" || dp.Provenance != "observed" || dp.Source != "tcp" {
		t.Fatalf("tcp.open row = %+v, want component/observed/tcp", dp)
	}
	// connect_time landed too (the port was open).
	waitMetric(t, ctx, gw, "disp-1", "tcp.connect_time", func(d *storage.MetricDatapoint) bool { return d != nil })

	// A node client to publish crafted Events (only its OWN telemetry subject).
	permErrs := make(chan error, 16)
	ncA, err := nats.Connect(srv.ClientURL(),
		nats.UserInfo("node-a", tokenA),
		nats.CustomInboxPrefix(collection.InboxPrefix("node-a")),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, e error) { permErrs <- e }),
	)
	if err != nil {
		t.Fatalf("node-a connect: %v", err)
	}
	defer ncA.Close()

	// NEGATIVE (b) CONFINEMENT: node-a publishes an Event for t-b, which belongs to
	// node-b (owner disp-2). The consumer must drop it (orphan): disp-2 gets no row.
	publishEvent(t, ncA, "node-a", &ogv1.Event{
		TaskId:     "t-b",
		NodeId:     "node-a",
		Datapoints: []*ogv1.Datapoint{{Name: "tcp.open", Value: &ogv1.Datapoint_DoubleValue{DoubleValue: 1}}},
	})

	// NEGATIVE (a) REJECT-NOT-PROJECT: node-a publishes for its own t-a but with an
	// unregistered datapoint name; that name must not be written.
	publishEvent(t, ncA, "node-a", &ogv1.Event{
		TaskId:     "t-a",
		NodeId:     "node-a",
		Datapoints: []*ogv1.Datapoint{{Name: "bogus.metric", Value: &ogv1.Datapoint_DoubleValue{DoubleValue: 9}}},
	})

	// WATERMARK: a valid datapoint published AFTER the negatives. JetStream is
	// ordered per subject and the consumer processes sequentially, so once the
	// watermark is visible the two negatives have already been handled (and
	// dropped). connect_time=42 is distinctive from any real dial.
	publishEvent(t, ncA, "node-a", &ogv1.Event{
		TaskId:     "t-a",
		NodeId:     "node-a",
		Datapoints: []*ogv1.Datapoint{{Name: "tcp.connect_time", Value: &ogv1.Datapoint_DoubleValue{DoubleValue: 42}}},
	})
	waitMetric(t, ctx, gw, "disp-1", "tcp.connect_time", func(d *storage.MetricDatapoint) bool { return d != nil && d.Value == 42 })

	// Confinement held: disp-2 (node-b's component) has NO datapoint from node-a.
	if got, err := gw.LatestMetric(ctx, "disp-2", "tcp.open"); err != nil {
		t.Fatalf("latest disp-2: %v", err)
	} else if got != nil {
		t.Fatalf("confinement breached: node-a landed a datapoint on disp-2: %+v", got)
	}
	// reject-not-project held: the unregistered name was never written.
	if got, err := gw.LatestMetric(ctx, "disp-1", "bogus.metric"); err != nil {
		t.Fatalf("latest bogus: %v", err)
	} else if got != nil {
		t.Fatalf("reject-not-project breached: unregistered name was written: %+v", got)
	}

	// --- STATE PATH (cp5a): interface.reachable is a STATE datapoint, routed by
	// the datapoint_type kind to state_datapoint, under the SAME confinement and
	// reject-not-project as a metric, plus the ingest-side transition-only guard.

	// node-a publishes interface.reachable=up for its own t-a. The registry kind is
	// state, so it lands in state_datapoint (not metric_datapoint), owned disp-1 and
	// instanced by the interface (disp-1-tcp).
	publishEvent(t, ncA, "node-a", &ogv1.Event{
		TaskId:     "t-a",
		NodeId:     "node-a",
		Datapoints: []*ogv1.Datapoint{{Name: "interface.reachable", Value: &ogv1.Datapoint_StringValue{StringValue: "up"}}},
	})
	waitState(t, ctx, gw, "disp-1", "interface.reachable", "disp-1-tcp", func(d *storage.StateDatapoint) bool { return d != nil && d.Value == "up" })

	// CONFINEMENT (state path): node-a publishes interface.reachable=up for t-b,
	// which belongs to node-b (owner disp-2). The same fence that drops a foreign
	// metric drops a foreign state: disp-2 gets no verdict.
	publishEvent(t, ncA, "node-a", &ogv1.Event{
		TaskId:     "t-b",
		NodeId:     "node-a",
		Datapoints: []*ogv1.Datapoint{{Name: "interface.reachable", Value: &ogv1.Datapoint_StringValue{StringValue: "up"}}},
	})

	// TRANSITION-ONLY (ingest guard): a repeated identical up must NOT add a second
	// row (the latest-value guard skips it); only a flip to down writes. The first
	// up is already committed (waitState above), so the guard sees it deterministically.
	publishEvent(t, ncA, "node-a", &ogv1.Event{
		TaskId:     "t-a",
		NodeId:     "node-a",
		Datapoints: []*ogv1.Datapoint{{Name: "interface.reachable", Value: &ogv1.Datapoint_StringValue{StringValue: "up"}}},
	})
	publishEvent(t, ncA, "node-a", &ogv1.Event{
		TaskId:     "t-a",
		NodeId:     "node-a",
		Datapoints: []*ogv1.Datapoint{{Name: "interface.reachable", Value: &ogv1.Datapoint_StringValue{StringValue: "down"}}},
	})
	waitState(t, ctx, gw, "disp-1", "interface.reachable", "disp-1-tcp", func(d *storage.StateDatapoint) bool { return d != nil && d.Value == "down" })

	// The series is exactly [up, down]: the duplicate up was guarded out, so the
	// availability strip has one row per transition, not one per publish.
	trans, err := gw.StateTransitions(ctx, "disp-1", "interface.reachable", "disp-1-tcp", time.Time{})
	if err != nil {
		t.Fatalf("state transitions: %v", err)
	}
	if len(trans) != 2 || trans[0].Value != "up" || trans[1].Value != "down" {
		t.Fatalf("transition-only breached: want [up down], got %+v", trans)
	}

	// Confinement held for the state path: disp-2 (node-b's component) got no verdict.
	if got, err := gw.LatestState(ctx, "disp-2", "interface.reachable", "disp-2-tcp"); err != nil {
		t.Fatalf("latest state disp-2: %v", err)
	} else if got != nil {
		t.Fatalf("state confinement breached: node-a landed a verdict on disp-2: %+v", got)
	}

	// Telemetry publish isolation: node-a cannot publish to node-b's telemetry
	// subject (a permissions violation), the same fence as worklist/heartbeat.
	if err := ncA.Publish(collection.TelemetrySubject("node-b"), []byte("x")); err != nil {
		t.Fatalf("client-side publish node-b telemetry: %v", err)
	}
	_ = ncA.Flush()
	if !awaitPermissionViolation(permErrs, 3*time.Second) {
		t.Fatalf("expected a permissions violation publishing to node-b's telemetry subject")
	}
}

func publishEvent(t *testing.T, nc *nats.Conn, node string, ev *ogv1.Event) {
	t.Helper()
	b, err := proto.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := nc.Publish(collection.TelemetrySubject(node), b); err != nil {
		t.Fatalf("publish telemetry %s: %v", node, err)
	}
	_ = nc.Flush()
}

// waitState polls LatestState until pred is satisfied or a deadline passes.
func waitState(t *testing.T, ctx context.Context, gw storage.Gateway, comp, key, instance string, pred func(*storage.StateDatapoint) bool) *storage.StateDatapoint {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		dp, err := gw.LatestState(ctx, comp, key, instance)
		if err != nil {
			t.Fatalf("latest state %s/%s[%s]: %v", comp, key, instance, err)
		}
		if pred(dp) {
			return dp
		}
		if time.Now().After(deadline) {
			t.Fatalf("state %s/%s[%s] never satisfied the predicate (last=%+v)", comp, key, instance, dp)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitMetric polls LatestMetric until pred is satisfied or a deadline passes.
func waitMetric(t *testing.T, ctx context.Context, gw storage.Gateway, comp, key string, pred func(*storage.MetricDatapoint) bool) *storage.MetricDatapoint {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		dp, err := gw.LatestMetric(ctx, comp, key)
		if err != nil {
			t.Fatalf("latest %s/%s: %v", comp, key, err)
		}
		if pred(dp) {
			return dp
		}
		if time.Now().After(deadline) {
			t.Fatalf("metric %s/%s never satisfied the predicate (last=%+v)", comp, key, dp)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
