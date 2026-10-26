package storage_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// landFixture is one database with a component and a node to land batches on,
// and a connection to count what landed.
type landFixture struct {
	gw   *storage.PG
	conn *pgx.Conn
}

func newLandFixture(t *testing.T) landFixture {
	t.Helper()
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all); err != nil {
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
	return landFixture{gw: gw, conn: conn}
}

// rows counts what has landed on each sink, so a test can say each row
// landed exactly once.
type landedRows struct{ metric, property, event, logLine, nodeLog int }

func (f landFixture) rows(t *testing.T) landedRows {
	t.Helper()
	var r landedRows
	err := f.conn.QueryRow(context.Background(), `select
		(select count(*) from metric),
		(select count(*) from property where provenance = 'observed'),
		(select count(*) from event where origin = 'caught'),
		(select count(*) from log_line),
		(select count(*) from node_log)`).Scan(&r.metric, &r.property, &r.event, &r.logLine, &r.nodeLog)
	if err != nil {
		t.Fatalf("count landed rows: %v", err)
	}
	return r
}

// fullBatch is one push batch on every sink a batch reaches, plus a node's
// self-log line, under id.
func fullBatch(id, power string, ts time.Time) storage.IngestBatch {
	return storage.IngestBatch{
		BatchID: id,
		Writer:  storage.PushWriter(),
		NodeLogs: []storage.NodeLogWrite{
			{Node: "edge-1", Source: "node", Severity: "info", Message: "worklist pulled", TS: ts},
		},
		LogLines: []storage.LogLineWrite{
			{OwnerKind: "component", OwnerID: "disp-1", Source: "syslog", Severity: "info", Message: "input changed", TS: ts},
		},
		Metrics: []storage.MetricSampleWrite{
			{OwnerKind: "component", OwnerID: "disp-1", Key: "room-temperature", Value: 21.5, Source: "push", TS: ts},
		},
		Events: []storage.EventWrite{
			{OwnerKind: "component", OwnerID: "disp-1", Key: "input-changed", Message: "hdmi-2", Source: "push", TS: ts},
		},
		Properties: []storage.PropertySampleWrite{
			{OwnerKind: "component", OwnerID: "disp-1", Key: "power-state", Value: power, Source: "push", TS: ts},
		},
	}
}

// TestLandBatchLandsEachRowOnce is the redelivery guarantee at the gateway
// (#311): a batch id lands once, so the same batch landed again (a JetStream
// redelivery after an ack that never arrived, a node's retry past the
// stream's duplicate window) writes nothing on any sink, log lines included,
// and says so. Concurrent deliveries of one batch land it once too: the
// receipt's key serializes them.
func TestLandBatchLandsEachRowOnce(t *testing.T) {
	f := newLandFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute)

	landed, err := f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-1", "on", ts))
	if err != nil || !landed {
		t.Fatalf("first landing: landed=%v err=%v", landed, err)
	}
	want := landedRows{metric: 1, property: 1, event: 1, logLine: 1, nodeLog: 1}
	if got := f.rows(t); got != want {
		t.Fatalf("after the first landing: got %+v, want %+v", got, want)
	}

	landed, err = f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-1", "on", ts))
	if err != nil {
		t.Fatalf("redelivered landing: %v", err)
	}
	if landed {
		t.Fatal("a redelivered batch reported landed: want false, it already landed")
	}
	if got := f.rows(t); got != want {
		t.Fatalf("after the redelivery: got %+v, want %+v (each row exactly once)", got, want)
	}

	// Eight deliveries of one new batch at once: one lands, seven see it.
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-2", "off", ts.Add(time.Second)))
			if err != nil {
				errs <- err
				return
			}
			results <- ok
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent landing: %v", err)
	}
	n := 0
	for ok := range results {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("concurrent deliveries of one batch: %d landed, want exactly 1", n)
	}
	want = landedRows{metric: 2, property: 2, event: 2, logLine: 2, nodeLog: 2}
	if got := f.rows(t); got != want {
		t.Fatalf("after the concurrent landing: got %+v, want %+v", got, want)
	}

	// A different batch carrying the same content is a new batch: it lands,
	// and only the property's unchanged value is held back by the transition
	// guard.
	landed, err = f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-3", "off", ts.Add(2*time.Second)))
	if err != nil || !landed {
		t.Fatalf("a new batch with repeated content: landed=%v err=%v", landed, err)
	}
	want = landedRows{metric: 3, property: 2, event: 3, logLine: 3, nodeLog: 3}
	if got := f.rows(t); got != want {
		t.Fatalf("after a new batch: got %+v, want %+v", got, want)
	}
}

// TestLandBatchIsAllOrNothing is the partial-failure half (#311): one row
// the gateway refuses fails the whole batch, so the log lines and samples
// that preceded it are not left behind to be written again on redelivery,
// and the receipt rolls back with them, so the corrected batch still lands.
func TestLandBatchIsAllOrNothing(t *testing.T) {
	f := newLandFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute)

	bad := fullBatch("og.v1.api.telemetry b-9", "on", ts)
	// An event on an owner that does not exist is refused, wherever the
	// event sink runs in the batch, and every other sink has rows to leave.
	bad.Events[0].OwnerID = "ghost"
	if landed, err := f.gw.LandBatch(ctx, bad); err == nil {
		t.Fatalf("a batch with a refused row: want an error, got landed=%v", landed)
	}
	if got := f.rows(t); got != (landedRows{}) {
		t.Fatalf("a failed batch left rows behind: %+v, want none", got)
	}

	landed, err := f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-9", "on", ts))
	if err != nil || !landed {
		t.Fatalf("the corrected batch under the same id: landed=%v err=%v (the receipt must roll back with the batch)", landed, err)
	}
	want := landedRows{metric: 1, property: 1, event: 1, logLine: 1, nodeLog: 1}
	if got := f.rows(t); got != want {
		t.Fatalf("after the corrected batch: got %+v, want %+v", got, want)
	}
}

// TestLandBatchTransitionGuardInTheInsert pins the transition-only property
// series as a conditional write (the #455 ruling, folded into #311): a value
// equal to the series' latest is not written, a change is, and two equal
// values for one series in one batch write once.
func TestLandBatchTransitionGuardInTheInsert(t *testing.T) {
	f := newLandFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-10 * time.Minute)
	power := func(id string, at time.Time, values ...string) storage.IngestBatch {
		b := storage.IngestBatch{BatchID: id, Writer: storage.PushWriter()}
		for i, v := range values {
			b.Properties = append(b.Properties, storage.PropertySampleWrite{
				OwnerKind: "component", OwnerID: "disp-1", Key: "power-state", Value: v, Source: "push", TS: at.Add(time.Duration(i) * time.Second),
			})
		}
		return b
	}
	steps := []struct {
		batch storage.IngestBatch
		want  int
	}{
		{power("p-1", ts, "on"), 1},
		{power("p-2", ts.Add(time.Minute), "on"), 1},
		{power("p-3", ts.Add(2*time.Minute), "off"), 2},
		{power("p-4", ts.Add(3*time.Minute), "on", "on"), 3},
		{power("p-5", ts.Add(4*time.Minute), "on"), 3},
	}
	for i, s := range steps {
		if _, err := f.gw.LandBatch(ctx, s.batch); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if got := f.rows(t).property; got != s.want {
			t.Fatalf("step %d (%s): %d property rows, want %d", i, s.batch.BatchID, got, s.want)
		}
	}
}

// TestLandBatchTransitionGuardHoldsUnderConcurrentDispatch is what removes
// the serial-dispatch dependency the guard used to rest on (#455): in each
// round, sixteen distinct batches carrying one new verdict for one series,
// released at once as concurrent consumers would land them, write it once,
// never a consecutive duplicate. The rounds alternate the verdict, so every
// round is a transition the guard must let through exactly once.
func TestLandBatchTransitionGuardHoldsUnderConcurrentDispatch(t *testing.T) {
	f := newLandFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Hour)
	const rounds, racers = 6, 16
	for r := 0; r < rounds; r++ {
		value := []string{"standby", "on"}[r%2]
		var wg sync.WaitGroup
		release := make(chan struct{})
		errs := make(chan error, racers)
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-release
				_, err := f.gw.LandBatch(ctx, storage.IngestBatch{
					BatchID: fmt.Sprintf("c-%d-%d", r, i),
					Writer:  storage.PushWriter(),
					Properties: []storage.PropertySampleWrite{{
						OwnerKind: "component", OwnerID: "disp-1", Key: "power-state", Value: value,
						Source: "push", TS: ts.Add(time.Duration(r)*time.Minute + time.Duration(i)*time.Millisecond),
					}},
				})
				if err != nil {
					errs <- err
				}
			}(i)
		}
		close(release)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("round %d: concurrent landing: %v", r, err)
		}
		if got := f.rows(t).property; got != r+1 {
			t.Fatalf("round %d: %d concurrent batches of the verdict %q left %d property rows, want %d (one per transition)", r, racers, value, got, r+1)
		}
	}
}

// TestPruneTelemetryReceipts pins the receipt horizon: a receipt older than
// the cut is deleted (in chunks, so a long backlog is not one statement) and
// a fresh one is kept, so a batch past the horizon could land again and one
// inside it cannot.
func TestPruneTelemetryReceipts(t *testing.T) {
	f := newLandFixture(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := f.gw.LandBatch(ctx, storage.IngestBatch{BatchID: fmt.Sprintf("r-%d", i)}); err != nil {
			t.Fatalf("land r-%d: %v", i, err)
		}
	}
	if _, err := f.conn.Exec(ctx, `update telemetry_receipt set landed_at = now() - interval '2 days' where batch_id in ('r-0', 'r-1', 'r-2')`); err != nil {
		t.Fatalf("age receipts: %v", err)
	}
	n, err := f.gw.PruneTelemetryReceipts(ctx, time.Now().Add(-24*time.Hour), 2)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 3 {
		t.Fatalf("pruned %d receipts, want the 3 past the cut", n)
	}
	if landed, err := f.gw.LandBatch(ctx, storage.IngestBatch{BatchID: "r-0"}); err != nil || !landed {
		t.Fatalf("a batch past the horizon: landed=%v err=%v, want it to land again", landed, err)
	}
	if landed, err := f.gw.LandBatch(ctx, storage.IngestBatch{BatchID: "r-4"}); err != nil || landed {
		t.Fatalf("a batch inside the horizon: landed=%v err=%v, want it held", landed, err)
	}
}

// TestLandBatchCleansNUL: a batch lands all or nothing, so a NUL that
// Postgres refuses in any text or jsonb value (a device's line, a pushed
// value) would lose every row that rode with it, after the push route had
// answered 202. The gateway replaces NUL with U+FFFD on every lane's text and
// jsonb before the insert, as it does for log lines, so the batch lands.
func TestLandBatchCleansNUL(t *testing.T) {
	f := newLandFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute)
	b := fullBatch("og.v1.api.telemetry nul-1", "on", ts)
	b.Properties = append(b.Properties, storage.PropertySampleWrite{
		OwnerKind: "component", OwnerID: "disp-1", Key: "firmware-version", Value: "1.2\x00\x00", Source: "push\x00", TS: ts,
	})
	b.Events[0].Message = "hdmi\x002"
	b.Events[0].Attributes = []byte(`{"input":"hdmi\u00002"}`)
	b.Metrics[0].Instance = "rx\x001"
	b.NodeLogs[0].Message = "arm reply\x00"
	b.LogLines[0].Message = "line\x00"
	landed, err := f.gw.LandBatch(ctx, b)
	if err != nil || !landed {
		t.Fatalf("a batch carrying NUL: landed=%v err=%v, want it landed with NUL replaced", landed, err)
	}
	var firmware, message, input, instance, nodeLog string
	if err := f.conn.QueryRow(ctx, `select
		(select value #>> '{}' from property where property_type_id = (select id from property_type where name = 'firmware-version')),
		(select message from event where origin = 'caught'),
		(select attributes->>'input' from event where origin = 'caught'),
		(select instance from metric),
		(select message from node_log)`).Scan(&firmware, &message, &input, &instance, &nodeLog); err != nil {
		t.Fatalf("read the landed rows: %v", err)
	}
	for _, c := range []struct{ got, want string }{
		{firmware, "1.2��"}, {message, "hdmi�2"}, {input, "hdmi�2"}, {instance, "rx�1"}, {nodeLog, "arm reply�"},
	} {
		if c.got != c.want {
			t.Fatalf("landed %q, want %q", c.got, c.want)
		}
	}
}

// TestLandBatchTakesItsRuleRowsBeforeAnyHealthLock pins the platform's lock
// order for a whole batch (rule_eval.go, calc_eval.go): the event rule rows
// reading any of its keys, then its calc marks, then the series and health
// locks judging takes. A batch that judged its property lane (raising an
// alarm, taking the owner's health lock) before taking the rule rows its
// metric lane reads would hold that health lock while waiting on a rule an
// edit holds FOR UPDATE, and the edit, wanting the health lock next, would
// deadlock with it.
func TestLandBatchTakesItsRuleRowsBeforeAnyHealthLock(t *testing.T) {
	f := newLandFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "power-off",
		SampleKey: "power-state", Fire: criteria("eq", "off"), Severity: "high",
		Selector: storage.EventRuleSelector{Components: []string{"disp-1"}}}, all); err != nil {
		t.Fatalf("create the property rule: %v", err)
	}
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "room-hot",
		SampleKey: "room-temperature", Fire: criteria("gt", 90), Severity: "warning"}, all); err != nil {
		t.Fatalf("create the metric rule: %v", err)
	}
	var compID string
	if err := f.conn.QueryRow(ctx, `select id::text from component where name = 'disp-1'`).Scan(&compID); err != nil {
		t.Fatalf("resolve the component: %v", err)
	}

	// The edit's side: the metric rule's row FOR UPDATE, as a rule write takes it.
	edit, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin the edit: %v", err)
	}
	defer func() { _ = edit.Rollback(ctx) }()
	if _, err := edit.Exec(ctx, `select id from event_rule where name = 'room-hot' for update`); err != nil {
		t.Fatalf("lock the metric rule: %v", err)
	}

	ts := time.Now().UTC().Add(-time.Minute)
	done := make(chan error, 1)
	go func() {
		_, err := f.gw.LandBatch(ctx, storage.IngestBatch{BatchID: "lock-order-1", Writer: storage.PushWriter(),
			Properties: []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "disp-1", Key: "power-state", Value: "off", Source: "push", TS: ts}},
			Metrics:    []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "disp-1", Key: "room-temperature", Value: 21.5, Source: "push", TS: ts}},
		})
		done <- err
	}()

	// Wait for the batch to block on the rule row the edit holds.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := edit.QueryRow(ctx, `select count(*) from pg_stat_activity where wait_event_type = 'Lock' and pid <> pg_backend_pid()`).Scan(&waiting); err != nil {
			t.Fatalf("read the waits: %v", err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the batch never waited on the rule row the edit holds")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Blocked there, the batch must hold no health lock the edit would want next.
	var free bool
	if err := edit.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtextextended($1, 0))`, storage.ExportHealthLockKey("component", compID)).Scan(&free); err != nil {
		t.Fatalf("probe the health lock: %v", err)
	}
	if !free {
		t.Fatal("the batch held disp-1's health lock while it waited on a rule row: an edit wanting that lock next would deadlock with it")
	}
	if err := edit.Rollback(ctx); err != nil {
		t.Fatalf("release the edit: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("land the batch: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the batch never landed once the edit let go")
	}
}
