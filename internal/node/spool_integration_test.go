package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

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

// lossyAck is a real JetStream publish whose first n acks are lost: the
// stream stores the batch, and the node is told it did not. Every attempt is
// recorded with the id it carried.
type lossyAck struct {
	real publishFunc
	n    int

	mu       sync.Mutex
	attempts map[string][]string // a batch's tag -> the ids its attempts carried
	calls    int
}

func (l *lossyAck) publish(subject string, data []byte, id string) (ackWait, error) {
	wait, err := l.real(subject, data, id)
	l.mu.Lock()
	l.calls++
	call := l.calls
	l.attempts[id] = append(l.attempts[id], id)
	l.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		if err := wait(ctx); err != nil {
			return err
		}
		if call <= l.n {
			return errors.New("the stream's ack never arrived")
		}
		return nil
	}, nil
}

// TestNodePublishMissingItsAckIsRetriedOnce drives the node's spool over a
// real JetStream and a real Postgres, as an enrolled node with its own
// credential: the stream stores a batch but the ack is lost twice, the spool
// retries it on each tick under the id it was minted with, and the batch
// lands exactly once, a node's self-log line (which no natural key could
// dedupe) and a task's sample alike (#453, #454).
// spoolFixture is a seeded database with an enrolled node (edge-1, token
// token-1), a component and a task on it, and the bus in front of them.
type spoolFixture struct {
	ctx  context.Context
	gw   *storage.PG
	conn *pgx.Conn
	srv  *bus.Server
}

func newSpoolFixture(t *testing.T) spoolFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-1"}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	sum := sha256.Sum256([]byte("token-1"))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-1", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('disp-1-tcp', 'tcp', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'edge-1'), '{"target":"127.0.0.1:1"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-1', 'poll', (select id from endpoint where name = 'disp-1-tcp'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	t.Cleanup(srv.Shutdown)
	return spoolFixture{ctx: ctx, gw: gw, conn: conn, srv: srv}
}

// nodeLogs counts the node_log rows carrying message.
func (f spoolFixture) nodeLogs(t *testing.T, message string) int {
	t.Helper()
	var n int
	if err := f.conn.QueryRow(f.ctx, `select count(*) from node_log where message = $1`, message).Scan(&n); err != nil {
		t.Fatalf("count node_log %q: %v", message, err)
	}
	return n
}

// waitNodeLog waits until a node_log row carrying message has landed.
func (f spoolFixture) waitNodeLog(t *testing.T, message string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for f.nodeLogs(t, message) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the node_log line %q never landed", message)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// selfLog is a node self-log batch carrying one line.
func selfLog(message string) *ogv1.TelemetryBatch {
	return &ogv1.TelemetryBatch{NodeId: "edge-1", Logs: []*ogv1.LogLine{{Message: message, Severity: "info", Source: "node"}}}
}

func TestNodePublishMissingItsAckIsRetriedOnce(t *testing.T) {
	f := newSpoolFixture(t)
	ctx, conn, srv := f.ctx, f.conn, f.srv
	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo("edge-1", "token-1"), nats.CustomInboxPrefix(collection.InboxPrefix("edge-1")))
	if err != nil {
		t.Fatalf("connect as the node: %v", err)
	}
	defer nc.Close()
	js, err := newNodeJetStream(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}

	lossy := &lossyAck{real: jetStreamPublish(js), n: 2, attempts: map[string][]string{}}
	spool := newTelemetrySpool(collection.TelemetrySubject("edge-1"), lossy.publish)

	if err := spool.send(&ogv1.TelemetryBatch{NodeId: "edge-1", Logs: []*ogv1.LogLine{{Message: "ack lost twice", Severity: "info", Source: "node"}}}); err != nil {
		t.Fatalf("send the self-log batch: %v", err)
	}
	if err := spool.send(buildBatch("t-1", "edge-1", []collection.Sample{{Name: "tcp-open", Value: 1, TS: time.Now().UTC()}})); err != nil {
		t.Fatalf("send the sample batch: %v", err)
	}
	waitIdle(t, spool)
	if n := spool.pending(); n != 2 {
		t.Fatalf("held after the lost ack: %d, want both batches", n)
	}
	// Ticks until the stream's answers arrive: each lost ack is a retry.
	for i := 0; i < 4 && spool.pending() > 0; i++ {
		tick(t, spool)
	}
	if n := spool.pending(); n != 0 {
		t.Fatalf("held after the stream answered: %d", n)
	}

	// Each row lands once: wait for both, then for the consumer to settle.
	count := func() (logs, metrics int) {
		if err := conn.QueryRow(ctx, `select
			(select count(*) from node_log where message = 'ack lost twice'),
			(select count(*) from metric where metric_type_id = (select id from metric_type where name = 'tcp-open'))`).Scan(&logs, &metrics); err != nil {
			t.Fatalf("count rows: %v", err)
		}
		return
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		logs, metrics := count()
		if logs >= 1 && metrics >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rows never landed: logs=%d metrics=%d", logs, metrics)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// A later batch is the watermark: the consumer takes the stream in
	// order, so once it has landed, anything a retry stored is behind it.
	if err := spool.send(&ogv1.TelemetryBatch{NodeId: "edge-1", Logs: []*ogv1.LogLine{{Message: "watermark", Severity: "info", Source: "node"}}}); err != nil {
		t.Fatalf("send the watermark: %v", err)
	}
	for {
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from node_log where message = 'watermark'`).Scan(&n); err != nil {
			t.Fatalf("count the watermark: %v", err)
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the watermark never landed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if logs, metrics := count(); logs != 1 || metrics != 1 {
		t.Fatalf("landed logs=%d metrics=%d, want each exactly once", logs, metrics)
	}
	lossy.mu.Lock()
	ids := len(lossy.attempts)
	calls := lossy.calls
	lossy.mu.Unlock()
	if ids != 3 || calls < 5 {
		t.Fatalf("attempts: %d calls over %d ids, want at least 5 (each of the two lost acks sent again) over the 3 ids the batches were minted with", calls, ids)
	}
}

// busRelay is a TCP relay in front of the bus, on a port it holds for the
// whole test, that the test can take down (every connection cut, every new
// one refused) and bring back: a WAN outage between a node and the server.
type busRelay struct {
	ln       net.Listener
	upstream string

	mu    sync.Mutex
	down  bool
	conns []net.Conn
}

func newBusRelay(t *testing.T, upstreamURL string) *busRelay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("relay listen: %v", err)
	}
	r := &busRelay{ln: ln, upstream: strings.TrimPrefix(upstreamURL, "nats://")}
	t.Cleanup(func() {
		_ = ln.Close()
		r.setDown(true)
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			r.mu.Lock()
			if r.down {
				r.mu.Unlock()
				_ = c.Close()
				continue
			}
			up, err := net.Dial("tcp", r.upstream)
			if err != nil {
				r.mu.Unlock()
				_ = c.Close()
				continue
			}
			r.conns = append(r.conns, c, up)
			r.mu.Unlock()
			go func() { _, _ = io.Copy(up, c); _ = up.Close(); _ = c.Close() }()
			go func() { _, _ = io.Copy(c, up); _ = up.Close(); _ = c.Close() }()
		}
	}()
	return r
}

func (r *busRelay) url() string { return "nats://" + r.ln.Addr().String() }

func (r *busRelay) setDown(down bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.down = down
	if down {
		for _, c := range r.conns {
			_ = c.Close()
		}
		r.conns = nil
	}
}

// TestNodeConnectionOutlivesALongOutage: a bus outage longer than any
// reconnect budget (here many hundred attempts long, at a shortened wait)
// does not end the node's connection, so a batch the spool held through it
// ships when the bus is back (#454). A connection that gave up would leave
// the node running and never shipping again until a restart lost the spool.
func TestNodeConnectionOutlivesALongOutage(t *testing.T) {
	f := newSpoolFixture(t)
	prior := [3]time.Duration{reconnectWait, reconnectJitter, reconnectJitterTLS}
	reconnectWait, reconnectJitter, reconnectJitterTLS = 5*time.Millisecond, time.Millisecond, time.Millisecond
	defer func() { reconnectWait, reconnectJitter, reconnectJitterTLS = prior[0], prior[1], prior[2] }()

	relay := newBusRelay(t, f.srv.ClientURL())
	nc, err := connectBus(relay.url(), "edge-1", "token-1", "edge-1")
	if err != nil {
		t.Fatalf("connect through the relay: %v", err)
	}
	defer nc.Close()
	js, err := newNodeJetStream(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	spool := newTelemetrySpool(collection.TelemetrySubject("edge-1"), jetStreamPublish(js))

	relay.setDown(true)
	time.Sleep(3 * time.Second)
	if err := spool.send(selfLog("held through the outage")); err != nil {
		t.Fatalf("send during the outage: %v", err)
	}
	relay.setDown(false)

	deadline := time.Now().Add(20 * time.Second)
	for spool.pending() > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the spool still holds %d batches after the bus came back; the connection is %v", spool.pending(), nc.Status())
		}
		spool.retry()
		time.Sleep(100 * time.Millisecond)
	}
	f.waitNodeLog(t, "held through the outage", 15*time.Second)
}

// TestSpoolDropsABatchTheBusCanNeverTake: a batch over the bus's payload
// limit can never be sent, so the spool drops it and says so rather than
// holding it at the head, where it would block every batch behind it until
// an overflow pushed it out.
func TestSpoolDropsABatchTheBusCanNeverTake(t *testing.T) {
	f := newSpoolFixture(t)
	nc, err := connectBus(f.srv.ClientURL(), "edge-1", "token-1", "edge-1")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	js, err := newNodeJetStream(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	spool := newTelemetrySpool(collection.TelemetrySubject("edge-1"), jetStreamPublish(js))
	big := strings.Repeat("x", int(nc.MaxPayload())+1024)
	recs := captureLogs(t, func() {
		if err := spool.send(selfLog(big)); err != nil {
			t.Fatalf("send the oversized batch: %v", err)
		}
		if err := spool.send(selfLog("behind the oversized batch")); err != nil {
			t.Fatalf("send the batch behind it: %v", err)
		}
		ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		defer cancel()
		if err := spool.drain(ctx); err != nil {
			t.Fatalf("drain: %v (the oversized batch blocked the queue)", err)
		}
	})
	f.waitNodeLog(t, "behind the oversized batch", 15*time.Second)
	if !spool.takeOverflow() {
		t.Fatal("the spool dropped a batch and did not report it to the run loop")
	}
	told := false
	for _, r := range recs {
		if v, ok := attr(r, "dropped"); ok && v.Int64() == 1 {
			told = true
		}
	}
	if !told {
		t.Fatal("the dropped batch was never told with its count")
	}
}
