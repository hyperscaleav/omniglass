package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/nats-io/nats.go/jetstream"
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

func (l *lossyAck) publish(ctx context.Context, subject string, data []byte, id string) error {
	err := l.real(ctx, subject, data, id)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	l.attempts[id] = append(l.attempts[id], id)
	if err != nil {
		return err
	}
	if l.calls <= l.n {
		return errors.New("the stream's ack never arrived")
	}
	return nil
}

// TestNodePublishMissingItsAckIsRetriedOnce drives the node's spool over a
// real JetStream and a real Postgres, as an enrolled node with its own
// credential: the stream stores a batch but the ack is lost twice, the spool
// retries it on each tick under the id it was minted with, and the batch
// lands exactly once, a node's self-log line (which no natural key could
// dedupe) and a task's sample alike (#453, #454).
func TestNodePublishMissingItsAckIsRetriedOnce(t *testing.T) {
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
	defer conn.Close(ctx)
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
	defer srv.Shutdown()
	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo("edge-1", "token-1"), nats.CustomInboxPrefix(collection.InboxPrefix("edge-1")))
	if err != nil {
		t.Fatalf("connect as the node: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
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
	if n := spool.pending(); n != 2 {
		t.Fatalf("held after the lost ack: %d, want both batches", n)
	}
	// Two ticks: the first retry's ack is lost too, the second is answered.
	for tick := 0; tick < 2; tick++ {
		_ = spool.retry()
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
	if ids != 3 || calls != 5 {
		t.Fatalf("attempts: %d calls over %d ids, want 5 calls (the self-log batch three times, the sample batch and the watermark once) over the 3 ids the batches were minted with", calls, ids)
	}
}
