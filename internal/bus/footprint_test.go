package bus

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/footprint"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
)

// The ingest footprint (#1272): what one node batch costs the database, counted.
//
// This is the counting instrument of ADR-0094 extended from reads to ingest.
// The standard workload (internal/footprint) is delivered through the real
// handler, handleTelemetry, exactly as JetStream would: the owner binding, the
// catalog fingerprint read, LandBatch with its receipt, locks, transition guard and rule
// evaluation, and the endpoint's seen stamp. Only the transport is a double.
// The fleet is the dev scenario's, seeded for real, and every batch is bound to
// one of its simulated endpoints' tasks, so the node writer's binding check
// runs as it does in production.
//
// What gates, all deterministic: statements per batch (exact), rows per
// observation on each lane (exact), metric rows per full heap page (exact),
// the metric table's index definitions (exact), and metric index bytes per
// row after REINDEX (bounded). None of these depends on the machine or the
// day; bytes depend on the schema and the Postgres major, which the harness
// pins. What does not gate: the live index size (its page splits follow the
// fleet's random uuids), receipt bytes, WAL bytes per observation, which move
// with checkpoint timing, and wall-clock time, which BenchmarkIngestFootprint
// measures and ADR-0094 forbids gating on.

const footprintNode = "edge-hq"

// footprintTicks is how many checks the measurement window spans, after one
// warm-up check that lands every property's first value.
const footprintTicks = 4

// The held footprint of the standard workload, measured 2026-10-27 on main at
// 50d4ae8d. A change that moves one of these on purpose updates it in the same
// PR and says why in the PR body; one that moves it by accident fails here
// with what moved.
const (
	// wantStatementsPerBatch is every statement one batch costs, from the
	// owner resolution to the endpoint's seen stamp. Exact. 31 until #1279,
	// which replaced the three catalog reads with one fingerprint read and a
	// registry kept between batches.
	wantStatementsPerBatch = 29
	// wantMetricRowsPerPage is how many of the window's metric rows fill one
	// 8 KiB heap page (about 126 bytes a row). Exact on every full page. Density
	// rather than a page count, so a fleet that grows by a few endpoints moves
	// nothing here unless a row does.
	wantMetricRowsPerPage = 65
	// maxMetricIndexBytesPerRow bounds the metric indexes per window row,
	// measured after REINDEX so the figure is the same on every run (a live
	// index's page splits follow the fleet's random uuids and vary by a fifth).
	// The bound is a little over the measured figure; the index definitions
	// below are pinned exactly, so an added index fails by name, not by size.
	maxMetricIndexBytesPerRow = 96
)

// wantMetricIndexes is every index on the metric table, by definition. An
// added, dropped or re-keyed index changes this list.
var wantMetricIndexes = []string{
	"CREATE INDEX metric_owner_idx ON public.metric USING btree (component_id, metric_type_id, instance, ts DESC) WHERE (component_id IS NOT NULL)",
	"CREATE UNIQUE INDEX metric_pkey ON public.metric USING btree (id)",
	"CREATE INDEX metric_ts_brin ON public.metric USING brin (ts)",
}

type footprintFixture struct {
	srv     *Server
	counter *querycount.Counter
	conn    *pgx.Conn
	tasks   []footprint.Task
	work    footprint.Workload
	seq     int
}

func newFootprintFixture(tb testing.TB) *footprintFixture {
	tb.Helper()
	if testing.Short() {
		tb.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(tb)
	counter := querycount.New()
	gw, err := storage.NewPG(ctx, dsn, storage.WithQueryTracer(counter),
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		tb.Fatalf("open gateway: %v", err)
	}
	tb.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		tb.Fatalf("boot seed: %v", err)
	}
	dev, err := devseed.LookupScenario("dev")
	if err != nil {
		tb.Fatalf("scenario: %v", err)
	}
	if err := devseed.RunScenario(ctx, gw, "", dev, devseed.Options{SimFleet: true}); err != nil {
		tb.Fatalf("dev seed: %v", err)
	}
	wl, err := gw.NodeWorklist(ctx, footprintNode)
	if err != nil {
		tb.Fatalf("worklist: %v", err)
	}
	// One poll task per endpoint: a node checks each endpoint once per tick.
	byEndpoint := map[string]string{}
	for _, t := range wl.Tasks {
		if t.Mode != "poll" {
			continue
		}
		if cur, ok := byEndpoint[t.EndpointID]; !ok || t.ID < cur {
			byEndpoint[t.EndpointID] = t.ID
		}
	}
	var tasks []footprint.Task
	for _, id := range byEndpoint {
		tasks = append(tasks, footprint.Task{ID: id, Node: footprintNode})
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	if len(tasks) == 0 {
		tb.Fatalf("the dev scenario placed no poll task on %s", footprintNode)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		tb.Fatalf("connect: %v", err)
	}
	tb.Cleanup(func() { _ = conn.Close(context.Background()) })
	return &footprintFixture{
		srv:     &Server{store: gw, orphans: newOrphanTally(orphanWindow)},
		counter: counter,
		conn:    conn,
		tasks:   tasks,
		work: footprint.Workload{
			Mix:     footprint.StandardMix,
			Cadence: 30 * time.Second,
			Start:   time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		},
	}
}

// deliver hands one task's tick to the handler as JetStream would, and fails
// unless the batch was acked: a refused or redelivered batch means the
// workload no longer fits the fleet, and every count after it would lie.
func (f *footprintFixture) deliver(tb testing.TB, task footprint.Task, tick int) {
	tb.Helper()
	data, err := proto.Marshal(f.work.Batch(task, tick))
	if err != nil {
		tb.Fatalf("marshal: %v", err)
	}
	f.seq++
	m := &deliveredMsg{
		subject: collection.TelemetrySubject(task.Node),
		data:    data,
		header:  nats.Header{jetstream.MsgIDHeader: []string{fmt.Sprintf("fp-%d", f.seq)}},
	}
	f.srv.handleTelemetry(m)
	if !m.acked || m.termed || m.naked {
		tb.Fatalf("task %s tick %d: acked=%v termed=%v naked=%v (the batch did not land)", task.ID, tick, m.acked, m.termed, m.naked)
	}
}

func (f *footprintFixture) tick(tb testing.TB, tick int) {
	for _, task := range f.tasks {
		f.deliver(tb, task, tick)
	}
}

// relationBytes is a relation's heap and its indexes after VACUUM. With
// rebuilt set, the indexes are rebuilt first (REINDEX), so the figure is what
// the entries occupy packed, the same on every run, rather than what this
// run's page splits left.
func (f *footprintFixture) relationBytes(tb testing.TB, table string, rebuilt bool) (heap, index int64) {
	tb.Helper()
	ctx := context.Background()
	if _, err := f.conn.Exec(ctx, "vacuum "+pgx.Identifier{table}.Sanitize()); err != nil {
		tb.Fatalf("vacuum %s: %v", table, err)
	}
	if rebuilt {
		if _, err := f.conn.Exec(ctx, "reindex table "+pgx.Identifier{table}.Sanitize()); err != nil {
			tb.Fatalf("reindex %s: %v", table, err)
		}
	}
	if err := f.conn.QueryRow(ctx, `select pg_relation_size($1::regclass), pg_indexes_size($1::regclass)`, table).Scan(&heap, &index); err != nil {
		tb.Fatalf("size %s: %v", table, err)
	}
	return heap, index
}

// rowsPerFullPage is how many of the metric rows after afterID each heap page
// holds, for every page but the window's first and last (which share space
// with rows outside it, or are not full yet).
func (f *footprintFixture) rowsPerFullPage(tb testing.TB, afterID int64) []int64 {
	tb.Helper()
	rows, err := f.conn.Query(context.Background(), `select count(*) from metric where id > $1
		group by (ctid::text::point)[0] order by (ctid::text::point)[0]`, afterID)
	if err != nil {
		tb.Fatalf("rows per page: %v", err)
	}
	counts, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		tb.Fatalf("rows per page: %v", err)
	}
	if len(counts) < 3 {
		tb.Fatalf("the window spans %d heap pages; at least 3 are needed to see a full one", len(counts))
	}
	return counts[1 : len(counts)-1]
}

func (f *footprintFixture) indexDefinitions(tb testing.TB, table string) []string {
	tb.Helper()
	rows, err := f.conn.Query(context.Background(), `select indexdef from pg_indexes where tablename = $1 order by indexname`, table)
	if err != nil {
		tb.Fatalf("index definitions: %v", err)
	}
	defs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		tb.Fatalf("index definitions: %v", err)
	}
	return defs
}

func (f *footprintFixture) count(tb testing.TB, sql string) int64 {
	tb.Helper()
	var n int64
	if err := f.conn.QueryRow(context.Background(), sql).Scan(&n); err != nil {
		tb.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (f *footprintFixture) walLSN(tb testing.TB) string {
	tb.Helper()
	var lsn string
	if err := f.conn.QueryRow(context.Background(), `select pg_current_wal_lsn()::text`).Scan(&lsn); err != nil {
		tb.Fatalf("wal lsn: %v", err)
	}
	return lsn
}

// TestIngestFootprint measures the standard workload over the dev scenario's
// fleet and holds the counts. A change to the ingest path that moves a count
// fails here with the number, and the PR that moves it on purpose (batched
// writes, #1274; narrow rows, #1275) updates the pin and says why.
func TestIngestFootprint(t *testing.T) {
	f := newFootprintFixture(t)
	ctx := context.Background()

	// The warm-up check lands every property's first value and the endpoints'
	// first seen stamps; the window after it is the steady state.
	f.tick(t, 0)

	metricRows0 := f.count(t, `select count(*) from metric`)
	lastMetricID := f.count(t, `select coalesce(max(id), 0) from metric`)
	propertyRows0 := f.count(t, `select count(*) from property where provenance = 'observed'`)
	_, metricIndex0 := f.relationBytes(t, "metric", true)
	receiptHeap0, receiptIndex0 := f.relationBytes(t, "telemetry_receipt", false)
	lsn0 := f.walLSN(t)
	f.counter.Reset()

	for tick := 1; tick <= footprintTicks; tick++ {
		f.tick(t, tick)
	}

	statements := f.counter.N()
	lsn1 := f.walLSN(t)
	batches := footprintTicks * len(f.tasks)
	var metricsPerBatch, propertiesPerBatch int
	for _, s := range footprint.StandardMix {
		if s.Lane == footprint.MetricLane {
			metricsPerBatch++
		} else {
			propertiesPerBatch++
		}
	}
	observations := batches * (metricsPerBatch + propertiesPerBatch)
	metricRows := f.count(t, `select count(*) from metric`) - metricRows0
	propertyRows := f.count(t, `select count(*) from property where provenance = 'observed'`) - propertyRows0
	_, liveIndex := f.relationBytes(t, "metric", false)
	liveIndex -= metricIndex0
	perPage := f.rowsPerFullPage(t, lastMetricID)
	_, metricIndex := f.relationBytes(t, "metric", true)
	metricIndex -= metricIndex0
	receiptHeap, receiptIndex := f.relationBytes(t, "telemetry_receipt", false)
	receiptBytes := receiptHeap - receiptHeap0 + receiptIndex - receiptIndex0
	var walBytes int64
	if err := f.conn.QueryRow(ctx, `select pg_wal_lsn_diff($1::pg_lsn, $2::pg_lsn)::bigint`, lsn1, lsn0).Scan(&walBytes); err != nil {
		t.Fatalf("wal diff: %v", err)
	}

	t.Logf("ingest footprint, standard mix, dev scenario, %d tasks x %d ticks = %d batches, %d observations",
		len(f.tasks), footprintTicks, batches, observations)
	t.Logf("  statements per batch     %8.2f", float64(statements)/float64(batches))
	t.Logf("  metric rows per obs      %8.3f  (%d rows for %d metric observations)", float64(metricRows)/float64(batches*metricsPerBatch), metricRows, batches*metricsPerBatch)
	t.Logf("  property rows per obs    %8.3f  (%d rows for %d property observations)", float64(propertyRows)/float64(batches*propertiesPerBatch), propertyRows, batches*propertiesPerBatch)
	t.Logf("  metric rows per heap page %7d  (%d full pages; %.1f bytes a row)", perPage[0], len(perPage), 8192/float64(perPage[0]))
	t.Logf("  metric index per row     %8.1f  (after REINDEX; held)", float64(metricIndex)/float64(metricRows))
	t.Logf("  metric index per row     %8.1f  (live, as this run's splits left it; varies with the fleet's random uuids)", float64(liveIndex)/float64(metricRows))
	t.Logf("  receipt bytes per batch  %8.1f", float64(receiptBytes)/float64(batches))
	t.Logf("  WAL bytes per obs        %8.1f  (diagnostic, not held)", float64(walBytes)/float64(observations))

	if statements != wantStatementsPerBatch*batches {
		t.Errorf("statements: %d over %d batches (%.2f a batch), want exactly %d a batch; one batch issued:\n  %s",
			statements, batches, float64(statements)/float64(batches), wantStatementsPerBatch,
			strings.Join(firstBatchStatements(f.counter, statements/batches), "\n  "))
	}
	if want := int64(batches * metricsPerBatch); metricRows != want {
		t.Errorf("metric rows: %d for %d metric observations; a quantity stores every observation", metricRows, want)
	}
	if propertyRows != 0 {
		t.Errorf("property rows: %d in a window where no property changed; the property lane stores transitions only", propertyRows)
	}
	for i, n := range perPage {
		if n != wantMetricRowsPerPage {
			t.Errorf("metric heap: full page %d of the window holds %d rows, want %d (%.1f bytes a row, want %.1f)",
				i+1, n, wantMetricRowsPerPage, 8192/float64(n), 8192/float64(wantMetricRowsPerPage))
		}
	}
	if perRow := float64(metricIndex) / float64(metricRows); perRow > maxMetricIndexBytesPerRow {
		t.Errorf("metric indexes: %.1f bytes a row after REINDEX, over the %d bound", perRow, maxMetricIndexBytesPerRow)
	}
	if got := f.indexDefinitions(t, "metric"); strings.Join(got, "\n") != strings.Join(wantMetricIndexes, "\n") {
		t.Errorf("metric indexes changed:\n  got  %s\n  want %s", strings.Join(got, "\n       "), strings.Join(wantMetricIndexes, "\n       "))
	}
	if testing.Verbose() {
		t.Logf("the statements of one batch:\n  %s", strings.Join(firstBatchStatements(f.counter, wantStatementsPerBatch), "\n  "))
	}
}

// BenchmarkIngestFootprint is the timed half (#1272): the wall-clock cost of one
// node batch through the real handler, at a small fleet and the whole dev
// fleet, about a factor of ten apart (ADR-0094: two sizes, so a constant reads
// apart from a linear cost). Diagnostic only: run with make bench before and
// after a change that claims an effect, compare with benchstat, never assert.
func BenchmarkIngestFootprint(b *testing.B) {
	f := newFootprintFixture(b)
	f.tick(b, 0) // first values and seen stamps, outside the timed section
	sizes := []struct {
		name  string
		tasks []footprint.Task
	}{
		{"fleet=13", f.tasks[:min(13, len(f.tasks))]},
		{fmt.Sprintf("fleet=%d", len(f.tasks)), f.tasks},
	}
	tick := 1
	for _, size := range sizes {
		b.Run(size.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				task := size.tasks[i%len(size.tasks)]
				if i%len(size.tasks) == 0 {
					tick++
				}
				f.deliver(b, task, tick)
			}
		})
	}
}

// firstBatchStatements is the statements the first measured batch issued, so a
// count that moves names what moved.
func firstBatchStatements(c *querycount.Counter, n int) []string {
	all := c.Summary()
	if n > len(all) {
		n = len(all)
	}
	return all[:n]
}

// deliveredMsg is the transport double: a JetStream delivery carrying a node's
// batch on its own subject with a message id, recording how the handler
// answered it. Everything behind it is real.
type deliveredMsg struct {
	subject string
	data    []byte
	header  nats.Header
	acked   bool
	naked   bool
	termed  bool
}

func (m *deliveredMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: 1}, nil
}
func (m *deliveredMsg) Data() []byte                    { return m.data }
func (m *deliveredMsg) Headers() nats.Header            { return m.header }
func (m *deliveredMsg) Subject() string                 { return m.subject }
func (m *deliveredMsg) Reply() string                   { return "" }
func (m *deliveredMsg) Ack() error                      { m.acked = true; return nil }
func (m *deliveredMsg) DoubleAck(context.Context) error { m.acked = true; return nil }
func (m *deliveredMsg) Nak() error                      { m.naked = true; return nil }
func (m *deliveredMsg) NakWithDelay(time.Duration) error {
	m.naked = true
	return nil
}
func (m *deliveredMsg) InProgress() error { return nil }
func (m *deliveredMsg) Term() error       { m.termed = true; return nil }
func (m *deliveredMsg) TermWithReason(string) error {
	m.termed = true
	return nil
}
