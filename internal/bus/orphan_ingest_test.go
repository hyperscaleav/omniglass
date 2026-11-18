package bus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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

// syncBuffer is a log sink safe for the consumer's goroutine and the test's.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) records(msg string) []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(b.buf.String(), "\n") {
		var r map[string]any
		if json.Unmarshal([]byte(line), &r) == nil && r["msg"] == msg {
			out = append(out, r)
		}
	}
	return out
}

// TestAnOrphanedBatchIsWarnedOnceAndCounted is #1200 over the real bus: an outpost
// publishing for a task placed on another outpost has its samples dropped by the
// confinement fence (ADR-0037), and the drop is never silent and never a
// flood. Five orphaned batches inside one window are warned about once, the
// warning naming the outpost, the task and the samples dropped, and all five are
// counted on the outpost, where the API's outpost read serves them.
func TestAnOrphanedBatchIsWarnedOnceAndCounted(t *testing.T) {
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
	for _, name := range []string{"outpost-a", "outpost-b"} {
		if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: name}, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	tokenA := "secret-a"
	if _, err := gw.SetEnrollmentToken(ctx, "", "outpost-a", hashHex(tokenA), all); err != nil {
		t.Fatalf("mint a: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-2"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into endpoint (name, transport, component, outpost_id, params) values
		('tcp', 'tcp', (select id from component where name = 'disp-2'), (select user_id from outpost where name = 'outpost-b'), '{"target":"127.0.0.1:1"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-b', 'poll', (select id from endpoint where component = (select id from component where name = 'disp-2')), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	conn.Close(ctx)

	sink := &syncBuffer{}
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(sink, nil)))
	defer slog.SetDefault(prior)
	// A window longer than the test, so every batch falls in one; a flush
	// short enough that the suppressed ones reach the store while it runs.
	defer bus.SetOrphanWindow(time.Hour, 50*time.Millisecond)()

	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	ncA, err := nats.Connect(srv.ClientURL(), nats.UserInfo("outpost-a", tokenA),
		nats.CustomInboxPrefix(collection.InboxPrefix("outpost-a")))
	if err != nil {
		t.Fatalf("outpost-a connect: %v", err)
	}
	defer ncA.Close()

	for range 5 {
		publishEvent(t, ncA, "outpost-a", bus.AsTick(&ogv1.TelemetryBatch{
			TaskId:    "t-b",
			OutpostId: "outpost-a",
			Metrics:   []*ogv1.MetricSample{{Name: "icmp-rtt-avg", Value: 1}, {Name: "tcp-connect-time", Value: 3}},
		}))
	}

	deadline := time.Now().Add(10 * time.Second)
	var n *storage.Outpost
	for {
		n, err = gw.GetOutpost(ctx, "outpost-a", all)
		if err != nil {
			t.Fatalf("get outpost: %v", err)
		}
		if n.OrphanedBatches == 5 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n.OrphanedBatches != 5 || n.LastOrphanedAt == nil {
		t.Fatalf("outpost-a orphaned_batches=%d last_orphaned_at=%v, want 5 counted and a time", n.OrphanedBatches, n.LastOrphanedAt)
	}
	if b, _ := gw.GetOutpost(ctx, "outpost-b", all); b.OrphanedBatches != 0 {
		t.Fatalf("outpost-b counted %d orphans, want none: the count is the publisher's", b.OrphanedBatches)
	}

	warned := sink.records("outpost batch orphaned by the confinement fence, samples dropped")
	if len(warned) != 1 {
		t.Fatalf("%d warnings for five orphaned batches in one window, want exactly 1: %v", len(warned), warned)
	}
	w := warned[0]
	if w["level"] != "WARN" || w["outpost"] != "outpost-a" || w["task"] != "t-b" || w["samples"] != float64(2) {
		t.Fatalf("warning = %v, want WARN naming outpost-a, task t-b and 2 samples", w)
	}
	if got, err := gw.LatestMetric(ctx, "disp-2", "tcp-connect-time"); err == nil && got != nil {
		t.Fatalf("an orphaned sample landed on disp-2: %+v", got)
	}
}
