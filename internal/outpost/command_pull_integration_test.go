package node

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
)

// TestCommandPullAgainstAStoppedBusLandsTheFailureCount (#1626): edge-1 pulls
// its command queue from a bus that has stopped. The pull fails, the node's
// report counts it, and once a bus is back the window's count rides the
// node-self block and lands on edge-1's platform component as
// command-pull-failure-count.
func TestCommandPullAgainstAStoppedBusLandsTheFailureCount(t *testing.T) {
	t.Parallel()
	f := newSpoolFixture(t)
	closed := make(chan struct{})
	down, err := nats.Connect(f.srv.ClientURL(), nats.UserInfo("edge-1", "token-1"),
		nats.CustomInboxPrefix(collection.InboxPrefix("edge-1")), nats.MaxReconnects(0),
		nats.ClosedHandler(func(*nats.Conn) { close(closed) }))
	if err != nil {
		t.Fatalf("connect as the node: %v", err)
	}
	defer down.Close()
	f.srv.Shutdown()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the node's connection never saw the bus stop")
	}

	start := time.Now().Add(-time.Minute)
	self := newNodeReport(time.Minute, 0, start)
	runCommands(f.ctx, down, "edge-1", nil, nil, nil, newCommandState(), self)
	report := self.take(time.Now())
	if got := readings(report)[writerclass.CommandPullFailureCount]; got != 1 {
		t.Fatalf("the report's command-pull-failure-count = %v, want the one failed pull: %v", got, readings(report))
	}

	back, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("restart the bus: %v", err)
	}
	t.Cleanup(back.Shutdown)
	nc, err := nats.Connect(back.ClientURL(), nats.UserInfo("edge-1", "token-1"), nats.CustomInboxPrefix(collection.InboxPrefix("edge-1")))
	if err != nil {
		t.Fatalf("reconnect as the node: %v", err)
	}
	defer nc.Close()
	js, err := newNodeJetStream(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	spool := newTelemetrySpool(collection.TelemetrySubject("edge-1"), "edge-1", jetStreamPublish(js))
	if err := spool.send(&ogv1.TelemetryBatch{NodeId: "edge-1", NodeSelf: &ogv1.NodeSelf{Metrics: report}}); err != nil {
		t.Fatalf("send the report: %v", err)
	}
	waitIdle(t, spool)

	n, err := f.gw.GetNode(f.ctx, "edge-1", scope.Set{All: true})
	if err != nil || n.PlatformComponentID == nil {
		t.Fatalf("edge-1's platform component: %+v (%v)", n, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := f.gw.LatestMetric(context.Background(), *n.PlatformComponentID, writerclass.CommandPullFailureCount)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			if got.Value != 1 {
				t.Fatalf("landed command-pull-failure-count = %v, want 1", got.Value)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("command-pull-failure-count never landed on edge-1's platform component")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
