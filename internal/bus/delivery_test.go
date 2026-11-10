package bus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/labfootprint"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
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
		(select count(*) from `+storagetest.MetricRows+` metric where source = 'delivery-test'),
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

func (s *commitThenFailStore) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
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

func (s *blockingStore) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	var landed storage.LandResult
	var err error
	if s.commit {
		landed, err = s.PG.LandBatch(ctx, b)
	}
	s.once.Do(func() { close(s.entered) })
	<-s.release
	if !s.commit {
		return storage.LandResult{}, errors.New("the process died mid-batch")
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

// failOnceOnValue lands batches for real, except that the first batch
// carrying a property of value fails, held until released, as a database
// blink would fail it.
type failOnceOnValue struct {
	*storage.PG
	value   string
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (s *failOnceOnValue) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	for _, p := range b.Properties {
		if p.Value != s.value {
			continue
		}
		first := false
		s.once.Do(func() { first = true })
		if first {
			close(s.entered)
			<-s.release
			return storage.LandResult{}, errors.New("the database blinked")
		}
	}
	return s.PG.LandBatch(ctx, b)
}

// TestATransientFailureKeepsASeriesInOrder: a batch whose landing fails for a
// transient reason is redelivered before anything behind it lands. The
// property lane is transition-only (a value equal to its series' latest is
// not written), which is right only if a series lands in order: a successor
// landing first would be judged against the wrong latest and dropped, and the
// redelivered batch would then leave the series reading a value the device
// had already left.
func TestATransientFailureKeepsASeriesInOrder(t *testing.T) {
	restore := bus.SetTelemetryNakDelay(100 * time.Millisecond)
	defer restore()
	f := newDeliveryFixture(t)
	store := &failOnceOnValue{PG: f.gw, value: "2.0", entered: make(chan struct{}), release: make(chan struct{})}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, store)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	t0 := time.Now().UTC().Add(-10 * time.Minute)
	firmware := func(v string, at time.Time) *ogv1.TelemetryBatch {
		return &ogv1.TelemetryBatch{Owner: &ogv1.Owner{Kind: "component", Ref: "bar-1"}, Source: "order-test",
			Properties: []*ogv1.PropertySample{{Name: "firmware-version", ValueJson: `"` + v + `"`, Ts: timestamppb.New(at)}}}
	}
	if err := srv.PublishTelemetry(f.ctx, firmware("1.0", t0)); err != nil {
		t.Fatalf("publish 1.0: %v", err)
	}
	if err := srv.PublishTelemetry(f.ctx, firmware("2.0", t0.Add(time.Minute))); err != nil {
		t.Fatalf("publish 2.0: %v", err)
	}
	<-store.entered
	// Published while 2.0 is inside its failing landing: back to 1.0.
	if err := srv.PublishTelemetry(f.ctx, firmware("1.0", t0.Add(2*time.Minute))); err != nil {
		t.Fatalf("publish 1.0 again: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	close(store.release)
	f.waitDrained(t, srv)
	var series []string
	rows, err := f.conn.Query(f.ctx, `select value #>> '{}' from property where provenance = 'observed' and source = 'order-test' order by ts, id`)
	if err != nil {
		t.Fatalf("read the series: %v", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		series = append(series, v)
	}
	rows.Close()
	if fmt.Sprint(series) != "[1.0 2.0 1.0]" {
		t.Fatalf("firmware-version series %v, want [1.0 2.0 1.0]: every transition, in the order the device reported them", series)
	}
}

// refuseOnValue refuses, as the gateway refuses a write it will never take,
// every batch carrying a property of value, and counts the attempts.
type refuseOnValue struct {
	*storage.PG
	value    string
	attempts atomic.Int32
}

func (s *refuseOnValue) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	for _, p := range b.Properties {
		if p.Value == s.value {
			s.attempts.Add(1)
			return storage.LandResult{}, fmt.Errorf("storage: property sample bar-1/firmware-version: %w", storage.ErrKeyWriterRefused)
		}
	}
	return s.PG.LandBatch(ctx, b)
}

// TestARefusedBatchIsDroppedAtOnce: a batch the gateway refuses outright (a
// write no retry can change) is dropped on its first delivery, so the batches
// behind it, which a series' order holds until it is settled, are not kept
// waiting through redeliveries that cannot succeed.
func TestARefusedBatchIsDroppedAtOnce(t *testing.T) {
	restore := bus.SetTelemetryNakDelay(2 * time.Second)
	defer restore()
	f := newDeliveryFixture(t)
	store := &refuseOnValue{PG: f.gw, value: "refused"}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, store)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	refused := pushBatch("refused-tag")
	refused.Properties = []*ogv1.PropertySample{{Name: "firmware-version", ValueJson: `"refused"`}}
	if err := srv.PublishTelemetry(f.ctx, refused); err != nil {
		t.Fatalf("publish the refused batch: %v", err)
	}
	if err := srv.PublishTelemetry(f.ctx, pushBatch("behind")); err != nil {
		t.Fatalf("publish the batch behind it: %v", err)
	}
	start := time.Now()
	f.waitLanded(t, "behind")
	if waited := time.Since(start); waited > 1500*time.Millisecond {
		t.Fatalf("the batch behind a refused one waited %v: the refused batch was redelivered rather than dropped", waited)
	}
	f.waitDrained(t, srv)
	if n := store.attempts.Load(); n != 1 {
		t.Fatalf("the refused batch was landed %d times, want once", n)
	}
}

// The ingest hop's loss (#1641), driven through a real JetStream and a real
// Postgres: a node publishes ledgered batches over its own subject, and the
// lab's read reconciles their receipts into each kind's loss.

// ledgerRig is a node, its credentials, and one poll task it holds.
type ledgerRig struct {
	deliveryFixture
	token string
	base  time.Time
}

const ledgerNode, ledgerBoot = "node-ledger", "boot-ledger-1"

func newLedgerRig(t *testing.T) ledgerRig {
	t.Helper()
	f := newDeliveryFixture(t)
	all := scope.Set{All: true}
	if _, err := f.gw.CreateNode(f.ctx, "", storage.NodeSpec{Name: ledgerNode}, all, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "secret-ledger"
	if _, err := f.gw.SetEnrollmentToken(f.ctx, "", ledgerNode, hashHex(token), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := f.conn.Exec(f.ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('bar-1-tcp', 'tcp', (select id from component where name = 'bar-1'), (select principal_id from node where name = $1), '{"target":"127.0.0.1:1"}'::jsonb)`, ledgerNode); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := f.conn.Exec(f.ctx, `insert into task (id, mode, endpoint_id, enabled) values
		('t-ledger', 'poll', (select id from endpoint where name = 'bar-1-tcp'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	return ledgerRig{deliveryFixture: f, token: token, base: time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)}
}

// connect is the node's own connection to srv.
func (r ledgerRig) connect(t *testing.T, srv *bus.Server) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo(ledgerNode, r.token),
		nats.CustomInboxPrefix(collection.InboxPrefix(ledgerNode)))
	if err != nil {
		t.Fatalf("node connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// window is the read's window: batches 1 to 5 are minted in it, 6 and on
// after it.
func (r ledgerRig) window() delivery.Window {
	return delivery.Window{From: r.base, To: r.base.Add(5 * time.Second)}
}

// ledgered is batch seq of the boot, minted seq seconds after the base: one
// task metric and one self-log, with the ledger a node's spool stamps.
// resolved is the highest seq every batch at or below had left the spool.
func (r ledgerRig) ledgered(seq, resolved uint64) *ogv1.TelemetryBatch {
	minted := r.base.Add(time.Duration(seq) * time.Second)
	tag := fmt.Sprintf("ledger-%d", seq)
	return &ogv1.TelemetryBatch{
		NodeId: ledgerNode,
		Groups: []*ogv1.TaskGroup{{TaskId: "t-ledger", Ts: timestamppb.New(minted),
			Metrics: []*ogv1.MetricSample{{Name: "tcp-connect-time", Value: float64(seq)}}}},
		Logs: []*ogv1.LogLine{{Message: tag, Severity: "info", Ts: timestamppb.New(minted)}},
		Ledger: &ogv1.DeliveryLedger{Boot: ledgerBoot, Seq: seq, Minted: timestamppb.New(minted), Resolved: resolved,
			Kinds: []*ogv1.KindCount{
				{Kind: string(delivery.Metric), Count: 1, Before: seq - 1},
				{Kind: string(delivery.Log), Count: 1, Before: seq - 1},
			}},
	}
}

// publish sends each batch on the node's subject.
func (r ledgerRig) publish(t *testing.T, nc *nats.Conn, batches ...*ogv1.TelemetryBatch) {
	t.Helper()
	for _, b := range batches {
		publishEvent(t, nc, ledgerNode, b)
	}
}

// waitReceipts waits until the boot has n receipts and the stream is empty.
func (r ledgerRig) waitReceipts(t *testing.T, srv *bus.Server, n int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var got int
		if err := r.conn.QueryRow(r.ctx, `select count(*) from telemetry_receipt where boot = $1`, ledgerBoot).Scan(&got); err != nil {
			t.Fatalf("count receipts: %v", err)
		}
		if got >= n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the boot has %d receipts, want %d", got, n)
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.waitDrained(t, srv)
}

// read is the lab's delivery read over the window, as lab measure makes it.
func (r ledgerRig) read(t *testing.T) ([]labfootprint.DeliveryRow, []labfootprint.UnsettledBoot) {
	t.Helper()
	w := r.window()
	rows, err := r.conn.Query(r.ctx, labfootprint.ReceiptsQuery(w.From, w.To))
	if err != nil {
		t.Fatalf("read receipts: %v", err)
	}
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan a receipt: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read receipts: %v", err)
	}
	receipts, err := labfootprint.ParseReceipts(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatalf("parse receipts: %v", err)
	}
	return labfootprint.DeliveryOf(labfootprint.DeliveryRead{Read: true, Window: w, Receipts: receipts, StreamClear: true})
}

// wantRow fails unless the read has a row for k with these figures.
func wantRow(t *testing.T, rows []labfootprint.DeliveryRow, k delivery.Kind, in, out, dedup uint64, outcomes map[delivery.Outcome]uint64) {
	t.Helper()
	for _, row := range rows {
		if row.Kind != k {
			continue
		}
		if row.In != in || row.Out != out || row.Deduplicated != dedup {
			t.Fatalf("%s: in %d out %d dedup %d, want in %d out %d dedup %d", k, row.In, row.Out, row.Deduplicated, in, out, dedup)
		}
		for _, o := range delivery.Outcomes {
			if row.Outcomes[o] != outcomes[o] {
				t.Fatalf("%s: %s = %d, want %d (all: %v)", k, o, row.Outcomes[o], outcomes[o], row.Outcomes)
			}
		}
		wantLoss, _ := delivery.Loss(in, out)
		if row.Loss == nil || *row.Loss != wantLoss {
			t.Fatalf("%s: loss %v, want %v", k, row.Loss, wantLoss)
		}
		return
	}
	t.Fatalf("no %s row in %+v", k, rows)
}

// TestWithheldBatchReadsAsMissing: a boot's batch 3 never reaches the
// stream (the node dropped it, its spool has moved past it), so its items
// read as missing, from the offsets of its neighbors, and the rest stored.
func TestWithheldBatchReadsAsMissing(t *testing.T) {
	r := newLedgerRig(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, r.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	nc := r.connect(t, srv)
	r.publish(t, nc, r.ledgered(1, 0), r.ledgered(2, 1), r.ledgered(4, 3), r.ledgered(5, 4), r.ledgered(6, 5))
	r.waitReceipts(t, srv, 5)

	rows, unsettled := r.read(t)
	if len(unsettled) != 0 {
		t.Fatalf("unsettled %v, want the boot settled: batch 6 resolved past the window", unsettled)
	}
	for _, k := range []delivery.Kind{delivery.Metric, delivery.Log} {
		wantRow(t, rows, k, 5, 4, 0, map[delivery.Outcome]uint64{delivery.Stored: 4, delivery.Missing: 1})
	}
	if len(rows) != 2 {
		t.Fatalf("rows %+v, want metric and log only", rows)
	}
}

// TestLowerNeighborLongBeforeTheWindowOwnsItsGap: the boot's batch 1 was
// minted two hours before the window (the node was down in between), so
// batch 2 is the boot's first in it. Batch 1 is still the lower neighbor
// that owns the seqs below 2: the read finds it by seq, however long ago it
// was minted, and the window reads loss 0 rather than batch 1's items as
// missing.
func TestLowerNeighborLongBeforeTheWindowOwnsItsGap(t *testing.T) {
	r := newLedgerRig(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, r.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	nc := r.connect(t, srv)
	first := r.ledgered(1, 0)
	early := timestamppb.New(r.base.Add(-2 * time.Hour))
	first.Ledger.Minted, first.Groups[0].Ts, first.Logs[0].Ts = early, early, early
	r.publish(t, nc, first, r.ledgered(2, 1), r.ledgered(3, 2), r.ledgered(4, 3), r.ledgered(5, 4), r.ledgered(6, 5))
	r.waitReceipts(t, srv, 6)

	rows, unsettled := r.read(t)
	if len(unsettled) != 0 {
		t.Fatalf("unsettled %v, want the boot settled: batch 6 resolved past the window", unsettled)
	}
	for _, k := range []delivery.Kind{delivery.Metric, delivery.Log} {
		wantRow(t, rows, k, 4, 4, 0, map[delivery.Outcome]uint64{delivery.Stored: 4})
	}
}

// TestHeldBatchLandsLateAndReadsWhole: batch 1 is held in the node's spool
// while 2 to 5 land. Until it lands the boot is unsettled and reads no
// ratio; once it lands late, the read is loss 0.
func TestHeldBatchLandsLateAndReadsWhole(t *testing.T) {
	r := newLedgerRig(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, r.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	nc := r.connect(t, srv)
	r.publish(t, nc, r.ledgered(2, 0), r.ledgered(3, 0), r.ledgered(4, 0), r.ledgered(5, 0), r.ledgered(6, 0))
	r.waitReceipts(t, srv, 5)

	rows, unsettled := r.read(t)
	if len(rows) != 0 || len(unsettled) != 1 || unsettled[0] != (labfootprint.UnsettledBoot{Node: ledgerNode, Boot: ledgerBoot}) {
		t.Fatalf("before batch 1 lands: rows %+v unsettled %v, want no ratio and the boot named", rows, unsettled)
	}

	r.publish(t, nc, r.ledgered(1, 0), r.ledgered(7, 6))
	r.waitReceipts(t, srv, 7)
	rows, unsettled = r.read(t)
	if len(unsettled) != 0 {
		t.Fatalf("unsettled %v after batch 1 landed", unsettled)
	}
	for _, k := range []delivery.Kind{delivery.Metric, delivery.Log} {
		wantRow(t, rows, k, 5, 5, 0, map[delivery.Outcome]uint64{delivery.Stored: 5})
	}
}

// failSeq fails every landing of one seq, as a database that never takes the
// batch; with redeliver set it instead fails only that seq's first landing
// after committing it, as an ack lost after the commit.
type failSeq struct {
	*storage.PG
	seq       uint64
	redeliver bool
	attempts  atomic.Int32
}

func (s *failSeq) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	if b.Ledger == nil || b.Ledger.Seq != s.seq {
		return s.PG.LandBatch(ctx, b)
	}
	n := s.attempts.Add(1)
	if !s.redeliver {
		return storage.LandResult{}, errors.New("the database blinked")
	}
	landed, err := s.PG.LandBatch(ctx, b)
	if n == 1 {
		return landed, errors.New("the ack never reached the stream")
	}
	return landed, err
}

// TestMaxDeliveredBatchReadsAsMissing: a batch that fails every delivery is
// dropped at the bound with no receipt, so its items read as missing.
func TestMaxDeliveredBatchReadsAsMissing(t *testing.T) {
	restore := bus.SetTelemetryNakDelay(20 * time.Millisecond)
	defer restore()
	r := newLedgerRig(t)
	store := &failSeq{PG: r.gw, seq: 3}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, store)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	nc := r.connect(t, srv)
	r.publish(t, nc, r.ledgered(1, 0), r.ledgered(2, 1), r.ledgered(3, 2), r.ledgered(4, 3), r.ledgered(5, 4), r.ledgered(6, 5))
	r.waitReceipts(t, srv, 5)
	if n := store.attempts.Load(); n < 2 {
		t.Fatalf("batch 3 was tried %d times, want it redelivered to the bound", n)
	}

	rows, unsettled := r.read(t)
	if len(unsettled) != 0 {
		t.Fatalf("unsettled %v", unsettled)
	}
	for _, k := range []delivery.Kind{delivery.Metric, delivery.Log} {
		wantRow(t, rows, k, 5, 4, 0, map[delivery.Outcome]uint64{delivery.Stored: 4, delivery.Missing: 1})
	}
}

// TestRedeliveredBatchReadsAsDeduplicated: a batch landed, its ack lost, and
// the stream handed it over again: the redelivery reads as deduplicated,
// beside the ratio, and the loss stays 0.
func TestRedeliveredBatchReadsAsDeduplicated(t *testing.T) {
	r := newLedgerRig(t)
	store := &failSeq{PG: r.gw, seq: 2, redeliver: true}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, store)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	nc := r.connect(t, srv)
	r.publish(t, nc, r.ledgered(1, 0), r.ledgered(2, 1), r.ledgered(3, 2), r.ledgered(4, 3), r.ledgered(5, 4), r.ledgered(6, 5))
	r.waitReceipts(t, srv, 6)
	if n := store.attempts.Load(); n != 2 {
		t.Fatalf("batch 2 was landed %d times, want the first and its redelivery", n)
	}

	rows, _ := r.read(t)
	for _, k := range []delivery.Kind{delivery.Metric, delivery.Log} {
		wantRow(t, rows, k, 5, 5, 1, map[delivery.Outcome]uint64{delivery.Stored: 5, delivery.Deduplicated: 1})
	}
}
