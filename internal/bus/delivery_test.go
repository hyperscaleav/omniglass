package bus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The delivery half of telemetry integrity (#311, #453), driven through a
// real JetStream and a real Postgres: a batch the stream hands the consumer
// more than once lands once, whatever the reason it came again.

// deliveryFixture is a seeded database with one component to push to.
type deliveryFixture struct {
	ctx  context.Context
	dsn  string
	gw   *storage.PG
	conn *pgx.Conn
}

func newDeliveryFixture(t *testing.T) deliveryFixture {
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1"}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return deliveryFixture{ctx: ctx, dsn: dsn, gw: gw, conn: conn}
}

// pushBatch is one batch on every lane a push reaches, its rows told apart
// by tag.
func pushBatch(tag string) *ogv1.TelemetryBatch {
	ts := timestamppb.New(time.Now().UTC().Add(-time.Minute))
	return &ogv1.TelemetryBatch{
		Owner:      &ogv1.Owner{Kind: "component", Ref: "bar-1"},
		Source:     "delivery-test",
		Ts:         ts,
		Metrics:    []*ogv1.MetricSample{{Name: "room-temperature", Value: 21.5}},
		Properties: []*ogv1.PropertySample{{Name: "firmware-version", ValueJson: `"` + tag + `"`}},
		Events:     []*ogv1.EventSample{{Name: "input-changed", Message: tag}},
		Logs:       []*ogv1.LogLine{{Message: tag, Severity: "info"}},
	}
}

// tagged counts the rows a batch tagged tag landed on each lane.
func (f deliveryFixture) tagged(t *testing.T, tag string) (metric, property, event, logLine int) {
	t.Helper()
	err := f.conn.QueryRow(f.ctx, `select
		(select count(*) from metric where source = 'delivery-test'),
		(select count(*) from property where value = to_jsonb($1::text) and provenance = 'observed'),
		(select count(*) from event where message = $1 and origin = 'caught'),
		(select count(*) from log_line where message = $1)`, tag).Scan(&metric, &property, &event, &logLine)
	if err != nil {
		t.Fatalf("count rows tagged %s: %v", tag, err)
	}
	return
}

// waitLanded waits until a batch tagged tag has a log line, the last sink
// any batch reaches, then gives the consumer a beat to land anything more.
func (f deliveryFixture) waitLanded(t *testing.T, tag string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, _, _, l := f.tagged(t, tag); l > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the batch tagged %s never landed", tag)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitDrained waits until the stream holds no batch: every one published,
// and every redelivery, has been taken and acked.
func (f deliveryFixture) waitDrained(t *testing.T, srv *bus.Server) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		n, err := srv.TelemetryBacklog(f.ctx)
		if err != nil {
			t.Fatalf("read the stream's backlog: %v", err)
		}
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stream still holds %d batches", n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// assertOnce fails unless the batch tagged tag landed exactly once on every
// lane (one metric row in all, since every batch here pushes one).
func (f deliveryFixture) assertOnce(t *testing.T, tag string, metrics int) {
	t.Helper()
	m, p, e, l := f.tagged(t, tag)
	if m != metrics || p != 1 || e != 1 || l != 1 {
		t.Fatalf("batch %s landed metric=%d property=%d event=%d log=%d, want metric=%d and each other lane exactly once", tag, m, p, e, l, metrics)
	}
}

// commitThenFailStore lands a batch for real, then fails its first n
// landings anyway: the consumer never acks, as when its ack is lost after
// the commit, so the stream hands the same batch over again.
type commitThenFailStore struct {
	*storage.PG
	n     int32
	calls atomic.Int32
}

func (s *commitThenFailStore) LandBatch(ctx context.Context, b storage.IngestBatch) (bool, error) {
	landed, err := s.PG.LandBatch(ctx, b)
	if s.calls.Add(1) <= s.n {
		return landed, errors.New("the ack never reached the stream")
	}
	return landed, err
}

// TestRedeliveredBatchLandsOnce: a batch whose landing committed but whose
// ack never reached the stream is redelivered, and the redelivery lands
// nothing, on any lane, log lines included (#311).
func TestRedeliveredBatchLandsOnce(t *testing.T) {
	f := newDeliveryFixture(t)
	store := &commitThenFailStore{PG: f.gw, n: 2}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, store)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()

	if err := srv.PublishTelemetry(f.ctx, pushBatch("redelivered")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	f.waitLanded(t, "redelivered")
	f.waitDrained(t, srv)
	deadline := time.Now().Add(10 * time.Second)
	for store.calls.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("the consumer took the batch %d times, want the 2 failed landings and the redelivery that acked", store.calls.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.assertOnce(t, "redelivered", 1)
}

// blockingStore holds the consumer inside a landing until released, so a
// test can kill the server there. With commit set the landing commits
// first (the crash comes between the commit and the ack); without it the
// landing never runs (the crash comes mid-batch, before the commit).
type blockingStore struct {
	*storage.PG
	commit  bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingStore) LandBatch(ctx context.Context, b storage.IngestBatch) (bool, error) {
	landed := false
	var err error
	if s.commit {
		landed, err = s.PG.LandBatch(ctx, b)
	}
	s.once.Do(func() { close(s.entered) })
	<-s.release
	if !s.commit {
		return false, errors.New("the process died mid-batch")
	}
	return landed, err
}

// TestCrashedConsumerReplaysCleanly kills the server with the consumer
// inside a landing, then starts a new one over the same JetStream store and
// database: the batch the dead consumer never acked is redelivered and lands
// exactly once, whether the crash came before its commit or after it.
func TestCrashedConsumerReplaysCleanly(t *testing.T) {
	restore := bus.SetTelemetryAckWait(time.Second)
	defer restore()
	for _, c := range []struct {
		name   string
		commit bool
	}{
		{"before the commit", false},
		{"after the commit, before the ack", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newDeliveryFixture(t)
			storeDir := t.TempDir()
			stuck := &blockingStore{PG: f.gw, commit: c.commit, entered: make(chan struct{}), release: make(chan struct{})}
			first, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, StoreDir: storeDir}, stuck)
			if err != nil {
				t.Fatalf("start the first bus: %v", err)
			}
			if err := first.PublishTelemetry(f.ctx, pushBatch("crashed")); err != nil {
				t.Fatalf("publish: %v", err)
			}
			select {
			case <-stuck.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("the consumer never took the batch")
			}
			// The crash: the server goes away with the consumer inside the
			// landing, and only then does the landing return, to a connection
			// that is gone, so no ack reaches the stream.
			first.Shutdown()
			close(stuck.release)

			second, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, StoreDir: storeDir}, f.gw)
			if err != nil {
				t.Fatalf("restart the bus: %v", err)
			}
			defer second.Shutdown()
			f.waitLanded(t, "crashed")
			// The batch the dead consumer held leaves the stream only when the
			// new one acks its redelivery, so a drained stream means the replay
			// has landed, or been held by its receipt.
			f.waitDrained(t, second)
			if err := second.PublishTelemetry(f.ctx, pushBatch("watermark")); err != nil {
				t.Fatalf("publish the watermark: %v", err)
			}
			f.waitLanded(t, "watermark")
			f.waitDrained(t, second)
			f.assertOnce(t, "crashed", 2)
		})
	}
}

// TestTelemetryStreamKeepsItsDuplicateWindow pins the window ADR-0453 states:
// OG_TELEMETRY remembers a batch's Nats-Msg-Id for five minutes, not
// JetStream's two-minute default, so a node's retry a few ticks later is
// still collapsed by the stream before the receipt has to hold it.
func TestTelemetryStreamKeepsItsDuplicateWindow(t *testing.T) {
	f := newDeliveryFixture(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	got, err := srv.TelemetryDuplicateWindow(f.ctx)
	if err != nil {
		t.Fatalf("read the stream's config: %v", err)
	}
	if got != 5*time.Minute {
		t.Fatalf("OG_TELEMETRY duplicate window = %v, want 5m (ADR-0453)", got)
	}
}

// TestReplayedBatchInsideTheWindowCollapses: a batch published twice under
// one Nats-Msg-Id (a publisher retrying after an ack it never saw) is
// stored once by the stream's duplicate window, and lands once (#453).
func TestReplayedBatchInsideTheWindowCollapses(t *testing.T) {
	f := newDeliveryFixture(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()

	first, err := srv.PublishTelemetryAs(f.ctx, pushBatch("replayed"), "replay-1")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	second, err := srv.PublishTelemetryAs(f.ctx, pushBatch("replayed"), "replay-1")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if first || !second {
		t.Fatalf("stream duplicates: first=%v second=%v, want the replay alone reported as a duplicate", first, second)
	}
	f.waitLanded(t, "replayed")
	if err := srv.PublishTelemetry(f.ctx, pushBatch("watermark")); err != nil {
		t.Fatalf("publish the watermark: %v", err)
	}
	f.waitLanded(t, "watermark")
	f.waitDrained(t, srv)
	f.assertOnce(t, "replayed", 2)
}

// TestReplayedBatchPastTheWindowLandsOnce: the same batch arriving after the
// stream has forgotten its id (a node retrying past the duplicate window, or
// across a server that lost its JetStream state) is stored again, and the
// receipt keeps it from landing twice (#311).
func TestReplayedBatchPastTheWindowLandsOnce(t *testing.T) {
	f := newDeliveryFixture(t)
	first, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	if _, err := first.PublishTelemetryAs(f.ctx, pushBatch("forgotten"), "forgotten-1"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	f.waitLanded(t, "forgotten")
	first.Shutdown()

	// A fresh store directory: a stream with no memory of the id.
	second, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("restart bus: %v", err)
	}
	defer second.Shutdown()
	dup, err := second.PublishTelemetryAs(f.ctx, pushBatch("forgotten"), "forgotten-1")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if dup {
		t.Fatal("a fresh stream reported the replay a duplicate: it has no memory of the id, so the receipt is what this proves")
	}
	if err := second.PublishTelemetry(f.ctx, pushBatch("watermark")); err != nil {
		t.Fatalf("publish the watermark: %v", err)
	}
	f.waitLanded(t, "watermark")
	f.waitDrained(t, second)
	f.assertOnce(t, "forgotten", 2)
}

// TestPushAnswers503WhenTheStreamCannotStore drives the push route end to
// end with no stream to store the batch: the route answers 503, never a 202
// it cannot stand behind (#453). The same route answers 202 once the stream
// is back.
func TestPushAnswers503WhenTheStreamCannotStore(t *testing.T) {
	f := newDeliveryFixture(t)
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := f.gw.BootstrapOwner(f.ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(f.gw, api.WithTelemetryPublisher(srv)))
	defer apiSrv.Close()

	push := func(tag string) int {
		body, _ := json.Marshal(map[string]any{
			"owner":      map[string]any{"kind": "component", "ref": "bar-1"},
			"properties": []map[string]any{{"name": "firmware-version", "value": tag}},
		})
		req, _ := http.NewRequestWithContext(f.ctx, http.MethodPost, apiSrv.URL+"/api/v1/telemetry:push", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("push: %v", err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if err := srv.DeleteTelemetryStream(f.ctx); err != nil {
		t.Fatalf("delete the stream: %v", err)
	}
	if got := push("unstored"); got != http.StatusServiceUnavailable {
		t.Fatalf("push with no stream to store it: status %d, want 503", got)
	}
	if err := srv.RestartTelemetryConsumer(); err != nil {
		t.Fatalf("restore the stream: %v", err)
	}
	if got := push("stored"); got != http.StatusAccepted {
		t.Fatalf("push once the stream is back: status %d, want 202", got)
	}
}

// TestServerPrunesReceiptsPastTheHorizon: the server's retention pass
// deletes a receipt older than the horizon and keeps a fresh one.
func TestServerPrunesReceiptsPastTheHorizon(t *testing.T) {
	f := newDeliveryFixture(t)
	if _, err := f.conn.Exec(f.ctx, `insert into telemetry_receipt (batch_id, landed_at) values
		('og.v1.api.telemetry old', now() - interval '3 days'), ('og.v1.api.telemetry fresh', now())`); err != nil {
		t.Fatalf("seed receipts: %v", err)
	}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var old, fresh int
		if err := f.conn.QueryRow(f.ctx, `select
			(select count(*) from telemetry_receipt where batch_id = 'og.v1.api.telemetry old'),
			(select count(*) from telemetry_receipt where batch_id = 'og.v1.api.telemetry fresh')`).Scan(&old, &fresh); err != nil {
			t.Fatalf("count receipts: %v", err)
		}
		if fresh != 1 {
			t.Fatalf("the fresh receipt was pruned")
		}
		if old == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the receipt past the horizon was never pruned")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestAPushedNULLandsTheBatch: a push carrying NUL in a property's value
// lands, every lane of it, rather than answering 202 and then failing the
// whole batch at the database until the consumer gives up on it.
func TestAPushedNULLandsTheBatch(t *testing.T) {
	f := newDeliveryFixture(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	b := pushBatch("nul")
	b.Properties = append(b.Properties, &ogv1.PropertySample{Name: "serial-number", ValueJson: `"SN\u00001"`})
	if err := srv.PublishTelemetry(f.ctx, b); err != nil {
		t.Fatalf("publish: %v", err)
	}
	f.waitLanded(t, "nul")
	f.waitDrained(t, srv)
	f.assertOnce(t, "nul", 1)
}
