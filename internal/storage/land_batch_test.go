package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// landFixture is one database with a component and a node to land batches on,
// and a connection to count what landed.
type landFixture struct {
	dsn  string
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
	return landFixture{dsn: dsn, gw: gw, conn: conn}
}

// landOf is LandBatch's result as the landed flag the older tests assert.
func landOf(r storage.LandResult, err error) (bool, error) { return r.Landed, err }

// rows counts what has landed on each sink, so a test can say each row
// landed exactly once.
type landedRows struct{ metric, property, event, logLine, nodeLog int }

func (f landFixture) rows(t *testing.T) landedRows {
	t.Helper()
	var r landedRows
	err := f.conn.QueryRow(context.Background(), `select
		(select count(*) from `+storagetest.MetricRows+` metric),
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
	t.Parallel()
	f := newLandFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute)

	landed, err := landOf(f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-1", "on", ts)))
	if err != nil || !landed {
		t.Fatalf("first landing: landed=%v err=%v", landed, err)
	}
	want := landedRows{metric: 1, property: 1, event: 1, logLine: 1, nodeLog: 1}
	if got := f.rows(t); got != want {
		t.Fatalf("after the first landing: got %+v, want %+v", got, want)
	}

	landed, err = landOf(f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-1", "on", ts)))
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
			ok, err := landOf(f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-2", "off", ts.Add(time.Second))))
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
	landed, err = landOf(f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-3", "off", ts.Add(2*time.Second))))
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
	t.Parallel()
	f := newLandFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute)

	bad := fullBatch("og.v1.api.telemetry b-9", "on", ts)
	// An event on an owner that does not exist is refused, wherever the
	// event sink runs in the batch, and every other sink has rows to leave.
	bad.Events[0].OwnerID = "ghost"
	if landed, err := landOf(f.gw.LandBatch(ctx, bad)); err == nil {
		t.Fatalf("a batch with a refused row: want an error, got landed=%v", landed)
	}
	if got := f.rows(t); got != (landedRows{}) {
		t.Fatalf("a failed batch left rows behind: %+v, want none", got)
	}

	landed, err := landOf(f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry b-9", "on", ts)))
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	if landed, err := landOf(f.gw.LandBatch(ctx, storage.IngestBatch{BatchID: "r-0"})); err != nil || !landed {
		t.Fatalf("a batch past the horizon: landed=%v err=%v, want it to land again", landed, err)
	}
	if landed, err := landOf(f.gw.LandBatch(ctx, storage.IngestBatch{BatchID: "r-4"})); err != nil || landed {
		t.Fatalf("a batch inside the horizon: landed=%v err=%v, want it held", landed, err)
	}
}

// TestLandBatchCleansNUL: a batch lands all or nothing, so a NUL that
// Postgres refuses in any text or jsonb value (a device's line, a pushed
// value) would lose every row that rode with it, after the push route had
// answered 202. The gateway replaces NUL with U+FFFD on every lane's text and
// jsonb before the insert, as it does for log lines, so the batch lands.
func TestLandBatchCleansNUL(t *testing.T) {
	t.Parallel()
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
	landed, err := landOf(f.gw.LandBatch(ctx, b))
	if err != nil || !landed {
		t.Fatalf("a batch carrying NUL: landed=%v err=%v, want it landed with NUL replaced", landed, err)
	}
	var firmware, message, input, instance, nodeLog string
	if err := f.conn.QueryRow(ctx, `select
		(select value #>> '{}' from property where property_type_id = (select id from property_type where name = 'firmware-version')),
		(select message from event where origin = 'caught'),
		(select attributes->>'input' from event where origin = 'caught'),
		(select instance from `+storagetest.MetricRows+` metric),
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
	t.Parallel()
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
		if err := edit.QueryRow(ctx, `select count(*) from pg_stat_activity where datname = current_database() and wait_event_type = 'Lock' and pid <> pg_backend_pid()`).Scan(&waiting); err != nil {
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

// receiptLedgerVersion is the migration that widens the receipt into the
// batch's accounting row (#1641).
const receiptLedgerVersion = "20261109232007"

// receiptTermedVersion is the migration that marks a Term receipt (#1691).
const receiptTermedVersion = "20261110140219"

// receiptPackedVersion is the migration that packs the receipt's ledger and
// outcome into integer arrays (#1699).
const receiptPackedVersion = "20261112051714"

// tallyOf is a tally from a literal, for comparison.
func tallyOf(m map[delivery.Kind]map[delivery.Outcome]int) delivery.Tally {
	var t delivery.Tally
	for k, os := range m {
		for o, n := range os {
			t.Add(k, o, n)
		}
	}
	return t
}

// sameTally reports whether two tallies count the same, as their JSON.
func sameTally(t *testing.T, got, want delivery.Tally) bool {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	return string(g) == string(w)
}

// receiptRow is one receipt's accounting columns.
type receiptRow struct {
	node, boot string
	seq        int64
	minted     time.Time
	resolved   int64
	deliveries int
	ledger     delivery.Declared
	outcome    delivery.Tally
	termed     bool
}

func (f landFixture) receipt(t *testing.T, id string) receiptRow {
	t.Helper()
	var r receiptRow
	var ledger []*int64
	var outcome []int32
	err := f.conn.QueryRow(context.Background(), `select node, boot, seq, minted, resolved, deliveries, ledger, outcome, termed
		from telemetry_receipt where batch_id = $1`, id).Scan(&r.node, &r.boot, &r.seq, &r.minted, &r.resolved, &r.deliveries, &ledger, &outcome, &r.termed)
	if err != nil {
		t.Fatalf("read receipt %s: %v", id, err)
	}
	if r.ledger, err = delivery.DeclaredOf(ledger); err != nil {
		t.Fatalf("receipt %s ledger: %v", id, err)
	}
	if r.outcome, err = delivery.TallyOf(outcome); err != nil {
		t.Fatalf("receipt %s outcome %v: %v", id, outcome, err)
	}
	return r
}

// ledgerTick is a node's tick under its ledger: task-a's rows on disp-1,
// task-b's (refuse decides how) on disp-2, a node self-log, and an item the
// consumer counted orphaned before the gateway saw the batch.
func ledgerTick(f groupFixture, id string, seq uint64, ts time.Time, refuse func(storage.IngestGroup) storage.IngestGroup) storage.IngestBatch {
	b := storage.IngestBatch{
		BatchID: id,
		Writer:  storage.NodeWriter("", ""),
		Node:    "edge-1",
		Ledger: &delivery.Stamp{Boot: "boot-1", Seq: seq, Minted: ts, Resolved: seq - 1, Kinds: delivery.Declared{
			delivery.Metric:   {Count: 3, Before: 3 * (seq - 1)},
			delivery.Property: {Count: 2, Before: 2 * (seq - 1)},
			delivery.Event:    {Count: 2, Before: 2 * (seq - 1)},
			delivery.Log:      {Count: 3, Before: 3 * (seq - 1)},
		}},
		NodeLogs: []storage.NodeLogWrite{{Node: "edge-1", Source: "node", Severity: "info", Message: "tick", TS: ts}},
		Groups:   []storage.IngestGroup{taskGroup("task-a", f.disp1, f.node, "on", ts)},
	}
	if refuse != nil {
		b.Groups = append(b.Groups, refuse(taskGroup("task-b", f.disp2, f.node, "on", ts)))
	}
	b.Tally.Add(delivery.Metric, delivery.Orphaned, 1)
	return b
}

// TestLandBatchRecordsItsOutcome is the receipt as the batch's accounting
// row (#1641): a landed tick records its node, its ledger and every item's
// outcome, the consumer's tally plus what the gateway stored, coalesced and
// refused. A refused group's items read refused-write and its sibling's
// stored, counted from the pass that commits alone: a refusal Postgres
// decides lands the batch three times, and only the last counts.
func TestLandBatchRecordsItsOutcome(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		refuse func(f groupFixture) func(storage.IngestGroup) storage.IngestGroup
	}{
		{"refused before any row is written", func(f groupFixture) func(storage.IngestGroup) storage.IngestGroup {
			return func(g storage.IngestGroup) storage.IngestGroup { g.Metrics[0].OwnerID = f.disp1; return g }
		}},
		{"refused by Postgres as the rows land", func(groupFixture) func(storage.IngestGroup) storage.IngestGroup {
			return func(g storage.IngestGroup) storage.IngestGroup {
				g.Events[0].Attributes = []byte("{not json")
				return g
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newGroupFixture(t)
			ctx := context.Background()
			ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
			b := ledgerTick(f, "tick-1", 1, ts, tc.refuse(f))
			res, err := f.gw.LandBatch(ctx, b)
			if err != nil || !res.Landed {
				t.Fatalf("land the tick: %+v, %v", res, err)
			}
			want := tallyOf(map[delivery.Kind]map[delivery.Outcome]int{
				delivery.Metric:   {delivery.Stored: 1, delivery.RefusedWrite: 1, delivery.Orphaned: 1},
				delivery.Property: {delivery.Stored: 1, delivery.RefusedWrite: 1},
				delivery.Event:    {delivery.Stored: 1, delivery.RefusedWrite: 1},
				delivery.Log:      {delivery.Stored: 2, delivery.RefusedWrite: 1},
			})
			if !sameTally(t, res.Outcome, want) {
				g, _ := json.Marshal(res.Outcome)
				w, _ := json.Marshal(want)
				t.Fatalf("outcome = %s, want %s", g, w)
			}
			r := f.receipt(t, "tick-1")
			if r.node != "edge-1" || r.boot != "boot-1" || r.seq != 1 || !r.minted.Equal(ts) || r.resolved != 0 || r.deliveries != 1 {
				t.Fatalf("receipt = %+v, want edge-1 boot-1 seq 1 minted %v resolved 0, one delivery", r, ts)
			}
			if r.ledger[delivery.Metric] != (delivery.Count{Count: 3}) || r.ledger[delivery.Log] != (delivery.Count{Count: 3}) || len(r.ledger) != 4 {
				t.Fatalf("receipt ledger = %+v, want the batch's declared counts", r.ledger)
			}
			if !sameTally(t, r.outcome, want) {
				t.Fatalf("receipt outcome = %+v, want the landed outcome", r.outcome)
			}
		})
	}
}

// TestLandBatchCountsACoalescedProperty: an unchanged property value the
// transition guard holds back is coalesced, not stored and not lost, and
// the receipt says so.
func TestLandBatchCountsACoalescedProperty(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	if _, err := f.gw.LandBatch(ctx, ledgerTick(f, "tick-1", 1, ts, nil)); err != nil {
		t.Fatal(err)
	}
	res, err := f.gw.LandBatch(ctx, ledgerTick(f, "tick-2", 2, ts.Add(time.Second), nil))
	if err != nil || !res.Landed {
		t.Fatalf("land the second tick: %+v, %v", res, err)
	}
	want := tallyOf(map[delivery.Kind]map[delivery.Outcome]int{
		delivery.Metric:   {delivery.Stored: 1, delivery.Orphaned: 1},
		delivery.Property: {delivery.Coalesced: 1},
		delivery.Event:    {delivery.Stored: 1},
		delivery.Log:      {delivery.Stored: 2},
	})
	if !sameTally(t, res.Outcome, want) {
		g, _ := json.Marshal(res.Outcome)
		t.Fatalf("outcome = %s, want the unchanged power-state coalesced", g)
	}
	if r := f.receipt(t, "tick-2"); !sameTally(t, r.outcome, want) {
		t.Fatalf("receipt outcome = %+v, want the landed outcome", r.outcome)
	}
}

// TestLandBatchRedeliveryCountsADelivery: the same batch id again writes no
// row and changes no outcome, and the receipt counts the delivery, so the
// read can say how many items the receipt absorbed. An upsert read by its
// rows affected would land the redelivery's rows a second time.
func TestLandBatchRedeliveryCountsADelivery(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	b := ledgerTick(f, "tick-1", 1, ts, nil)
	first, err := f.gw.LandBatch(ctx, b)
	if err != nil || !first.Landed {
		t.Fatalf("land the tick: %+v, %v", first, err)
	}
	rows := f.rows(t)
	again, err := f.gw.LandBatch(ctx, b)
	if err != nil || again.Landed || !again.Outcome.Empty() {
		t.Fatalf("redelivery = %+v, %v; want nothing landed and no outcome", again, err)
	}
	if got := f.rows(t); got != rows {
		t.Fatalf("after the redelivery %+v, want %+v (no row twice)", got, rows)
	}
	r := f.receipt(t, "tick-1")
	if r.deliveries != 2 {
		t.Fatalf("deliveries = %d, want 2", r.deliveries)
	}
	if !sameTally(t, r.outcome, first.Outcome) {
		t.Fatalf("receipt outcome after the redelivery = %+v, want the first landing's", r.outcome)
	}
}

// termOf is the Term receipt the consumer records for b, Termed whole with
// every declared item under cause.
func termOf(b storage.IngestBatch, cause delivery.Outcome) storage.TermedBatch {
	return storage.TermedBatch{BatchID: b.BatchID, Node: b.Node, Ledger: *b.Ledger, Outcome: b.Ledger.Kinds.Under(cause)}
}

// TestTermReceiptRecordsItsCause: a batch the server Terms whole leaves a
// receipt with its node, its ledger and every declared item under the cause,
// marked termed (#1691), so the loss table reads it under that cause.
func TestTermReceiptRecordsItsCause(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	b := ledgerTick(f, "tick-1", 4, ts, nil)
	if err := f.gw.RecordTermedBatch(ctx, termOf(b, delivery.Abandoned)); err != nil {
		t.Fatalf("record the Term: %v", err)
	}
	r := f.receipt(t, "tick-1")
	if !r.termed || r.node != "edge-1" || r.boot != "boot-1" || r.seq != 4 || !r.minted.Equal(ts) || r.resolved != 3 || r.deliveries != 1 {
		t.Fatalf("receipt = %+v, want the batch's node and ledger, one delivery, termed", r)
	}
	if !sameTally(t, r.outcome, b.Ledger.Kinds.Under(delivery.Abandoned)) || r.outcome.Count(delivery.Metric, delivery.Abandoned) != 3 {
		t.Fatalf("outcome = %+v, want every declared item abandoned", r.outcome)
	}
	if got := f.rows(t); got != (landedRows{}) {
		t.Fatalf("rows = %+v, want none for a Termed batch", got)
	}
}

// TestTermReceiptIsNotLandedForDedup: a Term receipt never stops a later
// copy of the batch from landing (#1691). The landing finds the Term
// receipt, lands the batch's rows and replaces it: one delivery, the landed
// outcome, no longer termed. A Term after a landing changes nothing.
func TestTermReceiptIsNotLandedForDedup(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	b := ledgerTick(f, "tick-1", 1, ts, nil)
	if err := f.gw.RecordTermedBatch(ctx, termOf(b, delivery.RefusedTask)); err != nil {
		t.Fatalf("record the Term: %v", err)
	}
	landed, err := f.gw.LandBatch(ctx, b)
	if err != nil || !landed.Landed {
		t.Fatalf("land the later copy: %+v, %v; want it landed over the Term receipt", landed, err)
	}
	rows := f.rows(t)
	if rows == (landedRows{}) {
		t.Fatal("the later copy landed no rows")
	}
	r := f.receipt(t, "tick-1")
	if r.termed || r.deliveries != 1 || !sameTally(t, r.outcome, landed.Outcome) || r.outcome.Count(delivery.Metric, delivery.RefusedTask) != 0 {
		t.Fatalf("receipt = %+v, want the landing's outcome, one delivery, not termed", r)
	}

	if err := f.gw.RecordTermedBatch(ctx, termOf(b, delivery.Abandoned)); err != nil {
		t.Fatalf("record a Term after the landing: %v", err)
	}
	if again := f.receipt(t, "tick-1"); again.termed || again.deliveries != 1 || !sameTally(t, again.outcome, landed.Outcome) {
		t.Fatalf("receipt after a late Term = %+v, want the landing's untouched", again)
	}
	if redelivered, err := f.gw.LandBatch(ctx, b); err != nil || redelivered.Landed {
		t.Fatalf("redelivery = %+v, %v; want the landed receipt to hold it", redelivered, err)
	}
	if got := f.rows(t); got != rows {
		t.Fatalf("rows after the redelivery = %+v, want %+v", got, rows)
	}
}

// TestReceiptTermedMigrationOverExistingReceipts: the migration marking a
// Term receipt applies over receipts already landed, each read as landed
// (not termed), and applies again as a no-op.
func TestReceiptTermedMigrationOverExistingReceipts(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, receiptTermedVersion); err != nil {
		t.Fatalf("roll back below the termed receipt: %v", err)
	}
	ctx := context.Background()
	conn := connectDSN(t, dsn)
	if _, err := conn.Exec(ctx, `insert into telemetry_receipt (batch_id) values ('old-1'), ('old-2')`); err != nil {
		t.Fatalf("plant receipts: %v", err)
	}
	raw, err := db.FS.ReadFile("migrations/" + receiptTermedVersion + "_telemetry_receipt_termed.sql")
	if err != nil {
		t.Fatalf("read the migration: %v", err)
	}
	up, _, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatal("migration missing '-- migrate:down' marker")
	}
	for i := range 2 {
		if _, err := conn.Exec(ctx, up); err != nil {
			t.Fatalf("apply %d: %v", i+1, err)
		}
	}
	var n, landed int
	if err := conn.QueryRow(ctx, `select count(*), count(*) filter (where not termed) from telemetry_receipt`).Scan(&n, &landed); err != nil {
		t.Fatalf("read receipts: %v", err)
	}
	if n != 2 || landed != 2 {
		t.Fatalf("receipts = %d, landed %d; want 2 of each", n, landed)
	}
}

// TestReceiptLedgerMigrationOverExistingReceipts: the migration widening
// the receipt applies over receipts already landed, each read as one
// delivery with no ledger, and applies again as a no-op.
func TestReceiptLedgerMigrationOverExistingReceipts(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, receiptLedgerVersion); err != nil {
		t.Fatalf("roll back below the receipt ledger: %v", err)
	}
	ctx := context.Background()
	conn := connectDSN(t, dsn)
	if _, err := conn.Exec(ctx, `insert into telemetry_receipt (batch_id) values ('old-1'), ('old-2')`); err != nil {
		t.Fatalf("plant receipts: %v", err)
	}
	raw, err := db.FS.ReadFile("migrations/" + receiptLedgerVersion + "_telemetry_receipt_ledger.sql")
	if err != nil {
		t.Fatalf("read the migration: %v", err)
	}
	up, _, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatal("migration missing '-- migrate:down' marker")
	}
	for i := range 2 {
		if _, err := conn.Exec(ctx, up); err != nil {
			t.Fatalf("apply %d: %v", i+1, err)
		}
	}
	var n, one, bare int
	if err := conn.QueryRow(ctx, `select count(*), count(*) filter (where deliveries = 1),
		count(*) filter (where node is null and boot is null and seq is null and minted is null
			and resolved is null and ledger is null and outcome is null)
		from telemetry_receipt`).Scan(&n, &one, &bare); err != nil {
		t.Fatalf("read receipts: %v", err)
	}
	if n != 2 || one != 2 || bare != 2 {
		t.Fatalf("receipts = %d, one delivery %d, no ledger %d; want 2 of each", n, one, bare)
	}
}

// TestReceiptPackedMigrationOverExistingReceipts (#1699): the migration
// packing the ledger and outcome applies over receipts already landed as
// jsonb, each reading back as it was written (a kind or outcome this server
// has no slot or code for left out, a zero left out of the outcome), applies
// again as a no-op, and its down section restores the jsonb.
func TestReceiptPackedMigrationOverExistingReceipts(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, receiptPackedVersion); err != nil {
		t.Fatalf("roll back below the packed receipt: %v", err)
	}
	ctx := context.Background()
	conn := connectDSN(t, dsn)
	if _, err := conn.Exec(ctx, `insert into telemetry_receipt (batch_id, ledger, outcome) values
		('full', '{"metric":{"count":58,"before":3000000000},"property":{"count":0,"before":9},"log":{"count":1,"before":2},"trace":{"count":1,"before":1}}',
			'{"metric":{"stored":58,"missing":2},"log":{"stored":1,"coalesced":0},"trace":{"stored":1},"event":{"returned":4}}'),
		('null-ledger', 'null', '{}'),
		('past-int32', '{"metric":{"count":3000000000,"before":18446744073709551615}}', '{"metric":{"stored":1,"missing":2999999999}}'),
		('bare', null, null)`); err != nil {
		t.Fatalf("plant receipts: %v", err)
	}
	raw, err := db.FS.ReadFile("migrations/" + receiptPackedVersion + "_telemetry_receipt_packed.sql")
	if err != nil {
		t.Fatalf("read the migration: %v", err)
	}
	up, down, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatal("migration missing '-- migrate:down' marker")
	}
	for i := range 2 {
		if _, err := conn.Exec(ctx, up); err != nil {
			t.Fatalf("apply %d: %v", i+1, err)
		}
	}
	read := func(id string) (ledger []*int64, outcome []int32) {
		t.Helper()
		if err := conn.QueryRow(ctx, `select ledger, outcome from telemetry_receipt where batch_id = $1`, id).Scan(&ledger, &outcome); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return ledger, outcome
	}

	ledger, outcome := read("full")
	d, err := delivery.DeclaredOf(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if want := (delivery.Declared{
		delivery.Metric:   {Count: 58, Before: 3_000_000_000},
		delivery.Property: {Count: 0, Before: 9},
		delivery.Log:      {Count: 1, Before: 2},
	}); fmt.Sprint(d) != fmt.Sprint(want) {
		t.Fatalf("full ledger = %v, want %v", d, want)
	}
	tl, err := delivery.TallyOf(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if want := tallyOf(map[delivery.Kind]map[delivery.Outcome]int{
		delivery.Metric: {delivery.Stored: 58, delivery.Missing: 2},
		delivery.Log:    {delivery.Stored: 1},
	}); !sameTally(t, tl, want) {
		t.Fatalf("full outcome = %+v, want %+v", tl, want)
	}

	ledger, outcome = read("null-ledger")
	if d, err := delivery.DeclaredOf(ledger); ledger == nil || err != nil || len(d) != 0 {
		t.Fatalf("null ledger = %v (%v, %v), want an array declaring nothing", ledger, d, err)
	}
	if outcome == nil || len(outcome) != 0 {
		t.Fatalf("empty outcome = %v, want an empty array", outcome)
	}

	ledger, outcome = read("past-int32")
	if d, err := delivery.DeclaredOf(ledger); err != nil || d[delivery.Metric] != (delivery.Count{Count: 3_000_000_000, Before: math.MaxInt64}) {
		t.Fatalf("past-int32 ledger = %v (%v), want before saturated at %d", d, err, int64(math.MaxInt64))
	}
	if tl, err := delivery.TallyOf(outcome); err != nil || tl.Count(delivery.Metric, delivery.Missing) != math.MaxInt32 || tl.Count(delivery.Metric, delivery.Stored) != 1 {
		t.Fatalf("past-int32 outcome = %v (%+v, %v), want missing saturated at %d", outcome, tl, err, math.MaxInt32)
	}

	if ledger, outcome = read("bare"); ledger != nil || outcome != nil {
		t.Fatalf("bare receipt = %v, %v; want no ledger and no outcome", ledger, outcome)
	}

	for i := range 2 {
		if _, err := conn.Exec(ctx, down); err != nil {
			t.Fatalf("roll back %d: %v", i+1, err)
		}
	}
	var restored bool
	if err := conn.QueryRow(ctx, `select ledger = '{"metric":{"count":58,"before":3000000000},"property":{"count":0,"before":9},"log":{"count":1,"before":2}}'
		and outcome = '{"metric":{"stored":58,"missing":2},"log":{"stored":1}}'
		from telemetry_receipt where batch_id = 'full'`).Scan(&restored); err != nil {
		t.Fatalf("read the restored receipt: %v", err)
	}
	if !restored {
		t.Fatal("the down section did not restore the full receipt's jsonb")
	}
}

// TestLandBatchRecordsTheNodesLoss (#1692): a batch whose ledger carries the
// node's own loss account writes it on the receipt as it came, and a batch
// whose node lost nothing leaves the column null.
func TestLandBatchRecordsTheNodesLoss(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	if _, err := f.gw.LandBatch(ctx, ledgerTick(f, "tick-1", 1, ts, nil)); err != nil {
		t.Fatal(err)
	}
	b := ledgerTick(f, "tick-3", 3, ts.Add(time.Second), nil)
	b.Ledger.Loss = delivery.NodeLoss{
		Dropped: []delivery.Drop{{Cause: delivery.SpoolOverflow, From: 2, To: 2, Items: map[delivery.Kind]uint64{delivery.Metric: 3}}},
		Lost:    map[delivery.Outcome]map[delivery.Kind]uint64{delivery.SelfLogOverflow: {delivery.Log: 7}},
	}
	if _, err := f.gw.LandBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	read := func(id string) *delivery.NodeLoss {
		var raw []byte
		if err := f.conn.QueryRow(ctx, `select node_loss from telemetry_receipt where batch_id = $1`, id).Scan(&raw); err != nil {
			t.Fatalf("receipt %s: %v", id, err)
		}
		if raw == nil {
			return nil
		}
		var n delivery.NodeLoss
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Fatalf("receipt %s node loss %s: %v", id, raw, err)
		}
		return &n
	}
	if n := read("tick-1"); n != nil {
		t.Fatalf("tick-1 node loss = %+v, want null for a node that lost nothing", n)
	}
	n := read("tick-3")
	if n == nil {
		t.Fatal("tick-3 carries no node loss")
	}
	got, _ := json.Marshal(n)
	want, _ := json.Marshal(b.Ledger.Loss)
	if string(got) != string(want) {
		t.Fatalf("tick-3 node loss = %s, want %s", got, want)
	}
}

// TestATermReceiptKeepsTheNodesLoss: a batch the server Terms whole (#1691)
// still carries the node's own loss account (#1692), so its Term receipt
// records it for the node hop's read, and a later copy that lands over the
// Term receipt writes its own.
func TestATermReceiptKeepsTheNodesLoss(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	b := ledgerTick(f, "tick-2", 2, ts, nil)
	b.Ledger.Loss = delivery.NodeLoss{
		Dropped: []delivery.Drop{{Cause: delivery.SpoolOverflow, From: 1, To: 1, Items: map[delivery.Kind]uint64{delivery.Metric: 3}}},
		Lost:    map[delivery.Outcome]map[delivery.Kind]uint64{delivery.SelfLogOverflow: {delivery.Log: 4}},
	}
	read := func() string {
		var raw []byte
		if err := f.conn.QueryRow(ctx, `select node_loss from telemetry_receipt where batch_id = $1`, b.BatchID).Scan(&raw); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		if raw == nil {
			return ""
		}
		var n delivery.NodeLoss
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Fatalf("node loss %s: %v", raw, err)
		}
		got, _ := json.Marshal(n)
		return string(got)
	}
	want, _ := json.Marshal(b.Ledger.Loss)
	if err := f.gw.RecordTermedBatch(ctx, termOf(b, delivery.RefusedTask)); err != nil {
		t.Fatalf("record the Term: %v", err)
	}
	if got := read(); got != string(want) {
		t.Fatalf("Term receipt node loss = %q, want %s", got, want)
	}
	b.Ledger.Loss.Lost[delivery.SelfLogOverflow][delivery.Log] = 6
	want, _ = json.Marshal(b.Ledger.Loss)
	if landed, err := f.gw.LandBatch(ctx, b); err != nil || !landed.Landed {
		t.Fatalf("land the later copy: %+v, %v", landed, err)
	}
	if got := read(); got != string(want) {
		t.Fatalf("landed receipt node loss = %q, want the landing's %s", got, want)
	}
}

// TestReceiptLandsAShortfallPastInt32 (#1699): a node whose ledger declares
// more items than int32 holds, as a counter bug would, still lands the
// items it carried, its receipt holding the shortfall saturated rather
// than the batch refused for it.
func TestReceiptLandsAShortfallPastInt32(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	b := storage.IngestBatch{
		BatchID: "og.v1.telemetry.edge-1 past-int32",
		Writer:  storage.NodeWriter("", ""),
		Node:    "edge-1",
		Ledger: &delivery.Stamp{Boot: "b1", Seq: 1, Minted: at, Kinds: delivery.Declared{
			delivery.Log: {Count: 3_000_000_001},
		}},
		NodeLogs: []storage.NodeLogWrite{{Node: "edge-1", Source: "node", Severity: "info", Message: "tick", TS: at}},
	}
	b.Tally.Add(delivery.Log, delivery.Missing, 3_000_000_000)
	if res, err := f.gw.LandBatch(ctx, b); err != nil || !res.Landed {
		t.Fatalf("land: %+v, %v", res, err)
	}
	var outcome []int32
	if err := f.conn.QueryRow(ctx, `select outcome from telemetry_receipt where batch_id = $1`, b.BatchID).Scan(&outcome); err != nil {
		t.Fatalf("read the receipt: %v", err)
	}
	tl, err := delivery.TallyOf(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Count(delivery.Log, delivery.Stored) != 1 || tl.Count(delivery.Log, delivery.Missing) != math.MaxInt32 {
		t.Fatalf("outcome = %+v, want 1 stored and missing saturated at %d", tl, math.MaxInt32)
	}
}
