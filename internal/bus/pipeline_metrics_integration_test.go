package bus_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The server's pipeline figures (#1628), driven through a real JetStream and
// a real Postgres: what the bus records lands, at the window's report, on the
// server's own platform component.

// pipelineRig is a delivery fixture with the server's platform component and
// the recorder the bus records into.
type pipelineRig struct {
	deliveryFixture
	rec    *platformmetric.Recorder
	server string
}

func newPipelineRig(t *testing.T) pipelineRig {
	t.Helper()
	f := newDeliveryFixture(t)
	id, err := f.gw.EnsureServerComponent(f.ctx, "pipeline-test")
	if err != nil {
		t.Fatalf("ensure the server component: %v", err)
	}
	return pipelineRig{deliveryFixture: f, rec: platformmetric.NewRecorder(time.Now), server: id}
}

// start runs the bus over store, recording into the rig's recorder.
func (r pipelineRig) start(t *testing.T, store bus.Store) *bus.Server {
	t.Helper()
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1, Metrics: r.rec}, store)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	t.Cleanup(srv.Shutdown)
	return srv
}

// report closes the window as the server's reporter does, the bus's sampler
// included.
func (r pipelineRig) report(t *testing.T, srv *bus.Server) {
	t.Helper()
	if err := platformmetric.NewReporter(r.rec, r.gw, r.server, nil, nil, srv.Sampler()).Report(r.ctx); err != nil {
		t.Fatalf("report the window: %v", err)
	}
}

// latest reads one figure off the server's component, failing when it never
// landed.
func (r pipelineRig) latest(t *testing.T, key, instance string) float64 {
	t.Helper()
	m, err := r.gw.LatestMetricInstance(r.ctx, r.server, key, instance)
	if err != nil {
		t.Fatalf("read %s[%s]: %v", key, instance, err)
	}
	if m == nil {
		t.Fatalf("%s[%s] never landed on the server component", key, instance)
	}
	return m.Value
}

// enrollNode makes node-a with a token and connects it to srv.
func (r pipelineRig) enrollNode(t *testing.T, srv *bus.Server) *nats.Conn {
	t.Helper()
	all := scope.Set{All: true}
	if _, err := r.gw.CreateNode(r.ctx, "", storage.NodeSpec{Name: "node-a"}, all, all); err != nil {
		t.Fatalf("create node-a: %v", err)
	}
	if _, err := r.gw.SetEnrollmentToken(r.ctx, "", "node-a", hashHex("secret-a"), all); err != nil {
		t.Fatalf("mint node-a: %v", err)
	}
	nc, err := nats.Connect(srv.ClientURL(), nats.UserInfo("node-a", "secret-a"), nats.CustomInboxPrefix(collection.InboxPrefix("node-a")))
	if err != nil {
		t.Fatalf("node-a connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// TestTelemetryBatchLandsPipelineMetricsOverNATS: a batch a node publishes
// lands, and the window it landed in reports its lag, its samples and the
// consumer's backlog on the server's platform component. A group the fence
// drops landed nothing, so it counts toward neither, and reads as the
// server's delivery loss.
func TestTelemetryBatchLandsPipelineMetricsOverNATS(t *testing.T) {
	r := newPipelineRig(t)
	srv := r.start(t, r.gw)
	nc := r.enrollNode(t, srv)
	if _, err := r.conn.Exec(r.ctx, `insert into endpoint (name, transport, component, node_name, params) values
		('bar-1-tcp', 'tcp', (select id from component where name = 'bar-1'), (select principal_id from node where name = 'node-a'), '{"target":"127.0.0.1:1"}'::jsonb)`); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	if _, err := r.conn.Exec(r.ctx, `insert into task (id, mode, endpoint_id, enabled) values ('t-a', 'poll', (select id from endpoint where name = 'bar-1-tcp'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	sampled := time.Now().UTC().Add(-3 * time.Second)
	// t-ghost is no task of node-a's: the fence drops its group, so its
	// sample, an hour late, is no landing and moves no figure.
	publishEvent(t, nc, "node-a", &ogv1.TelemetryBatch{NodeId: "node-a", Groups: []*ogv1.TaskGroup{
		{TaskId: "t-a", Ts: timestamppb.New(sampled), Metrics: []*ogv1.MetricSample{{Name: "tcp-connect-time", Value: 11}, {Name: "tcp-open", Value: 1}}},
		{TaskId: "t-ghost", Ts: timestamppb.New(sampled.Add(-time.Hour)), Metrics: []*ogv1.MetricSample{{Name: "tcp-open", Value: 0}}},
	}})
	waitMetric(t, r.ctx, r.gw, "bar-1", "tcp-connect-time", func(d *storage.MetricSample) bool { return d != nil && d.Value == 11 })
	r.waitDrained(t, srv)
	r.report(t, srv)

	if lag := r.latest(t, writerclass.IngestLag, ""); lag < 3 || lag > 30 {
		t.Fatalf("ingest-lag = %v s, want the 3 s the batch waited, give or take its landing", lag)
	}
	if lag := r.latest(t, writerclass.IngestLagMax, ""); lag < 3 || lag > 30 {
		t.Fatalf("ingest-lag-max = %v s, want about 3", lag)
	}
	if n := r.latest(t, writerclass.IngestSampleCount, ""); n != 2 {
		t.Fatalf("ingest-sample-count = %v, want the 2 the batch landed", n)
	}
	if n := r.latest(t, writerclass.IngestBatchCount, ""); n != 1 {
		t.Fatalf("ingest-batch-count = %v, want 1", n)
	}
	if n := r.latest(t, writerclass.BusConsumerPending, ""); n != 0 {
		t.Fatalf("bus-consumer-pending = %v, want a drained stream's 0", n)
	}
	r.latest(t, writerclass.BusConsumerAckPending, "")
	r.latest(t, writerclass.IngestLaneBusy, "")

	// The server's hop (#1659): the tick's 3 metrics came in and 1 went out.
	// The fence kept t-ghost's, and tcp-open is a property the metric lane
	// refuses, so the landed group stored only tcp-connect-time. The kinds
	// the tick did not carry read a quiet window's zeros, not nothing.
	got := [3]float64{
		r.latest(t, writerclass.DeliveryInCount, string(delivery.Metric)),
		r.latest(t, writerclass.DeliveryOutCount, string(delivery.Metric)),
		r.latest(t, writerclass.DeliveryLossRatio, string(delivery.Metric)),
	}
	if want := [3]float64{3, 1, 2.0 / 3}; got != want {
		t.Fatalf("metric delivery in/out/loss = %v, want %v", got, want)
	}
	for _, k := range []delivery.Kind{delivery.Property, delivery.Event, delivery.Log} {
		if n := r.latest(t, writerclass.DeliveryInCount, string(k)); n != 0 {
			t.Fatalf("%s delivery-in-count = %v, want a quiet window's 0", k, n)
		}
		if n := r.latest(t, writerclass.DeliveryLossRatio, string(k)); n != 0 {
			t.Fatalf("%s delivery-loss-ratio = %v, want a quiet window's 0", k, n)
		}
	}
}

// failLanding fails, as a database that blinks fails, every landing of a
// batch carrying a property of value, and counts the attempts.
type failLanding struct {
	*storage.PG
	value    string
	attempts atomic.Int32
}

func (s *failLanding) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	for _, p := range b.Properties {
		if p.Value == s.value {
			s.attempts.Add(1)
			return storage.LandResult{}, errors.New("the database blinked")
		}
	}
	return s.PG.LandBatch(ctx, b)
}

// TestMaxDeliverCountsOneDrop: a batch that fails every delivery is dropped
// at the bound, and its items count once as in and never out, however many
// deliveries it took to get there (#1659); a batch that landed counts as
// landed.
func TestMaxDeliverCountsOneDrop(t *testing.T) {
	restore := bus.SetTelemetryNakDelay(20 * time.Millisecond)
	defer restore()
	r := newPipelineRig(t)
	store := &failLanding{PG: r.gw, value: "doomed"}
	srv := r.start(t, store)
	doomed := pushBatch("doomed-tag")
	doomed.Properties = []*ogv1.PropertySample{{Name: "firmware-version", ValueJson: `"doomed"`}}
	if err := srv.PublishTelemetry(r.ctx, doomed); err != nil {
		t.Fatalf("publish the doomed batch: %v", err)
	}
	if err := srv.PublishTelemetry(r.ctx, pushBatch("landed")); err != nil {
		t.Fatalf("publish the batch behind it: %v", err)
	}
	r.waitLanded(t, "landed")
	r.waitDrained(t, srv)
	if n := store.attempts.Load(); n < 2 {
		t.Fatalf("the doomed batch was tried %d times, want it redelivered to the bound", n)
	}
	r.report(t, srv)

	// Each batch carries one item of every kind: the doomed one came in and
	// never went out, the landed one went both ways.
	r.wantDelivery(t, 2, 1, 0.5)
	if n := r.latest(t, writerclass.IngestBatchCount, ""); n != 1 {
		t.Fatalf("ingest-batch-count = %v, want the 1 batch that landed", n)
	}
}

// alreadyLanded answers a batch carrying a property of value as LandBatch
// answers one whose receipt exists (a redelivery after a lost ack, or a
// replay past the stream's duplicate window): landed nothing, no error.
type alreadyLanded struct {
	*storage.PG
	value string
	held  atomic.Int32
}

func (s *alreadyLanded) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	for _, p := range b.Properties {
		if p.Value == s.value {
			s.held.Add(1)
			return storage.LandResult{}, nil
		}
	}
	return s.PG.LandBatch(ctx, b)
}

// TestAHeldRedeliveryCountsNoLanding: a batch its receipt holds to a no-op
// landed nothing, so it adds no batch, no samples and no lag; only the batch
// that landed counts. Its delivery reads no loss.
func TestAHeldRedeliveryCountsNoLanding(t *testing.T) {
	r := newPipelineRig(t)
	store := &alreadyLanded{PG: r.gw, value: "held"}
	srv := r.start(t, store)
	held := pushBatch("held")
	held.Ts = timestamppb.New(time.Now().UTC().Add(-time.Hour))
	if err := srv.PublishTelemetry(r.ctx, held); err != nil {
		t.Fatalf("publish the held batch: %v", err)
	}
	if err := srv.PublishTelemetry(r.ctx, pushBatch("landed")); err != nil {
		t.Fatalf("publish the batch behind it: %v", err)
	}
	r.waitLanded(t, "landed")
	r.waitDrained(t, srv)
	if n := store.held.Load(); n != 1 {
		t.Fatalf("the receipt held %d deliveries, want the 1", n)
	}
	r.report(t, srv)

	if n := r.latest(t, writerclass.IngestBatchCount, ""); n != 1 {
		t.Fatalf("ingest-batch-count = %v, want the 1 batch that landed", n)
	}
	if n := r.latest(t, writerclass.IngestSampleCount, ""); n != 3 {
		t.Fatalf("ingest-sample-count = %v, want the landed batch's 3", n)
	}
	if lag := r.latest(t, writerclass.IngestLagMax, ""); lag > 30*60 {
		t.Fatalf("ingest-lag-max = %v s, want the landed batch's minute, not the held batch's hour", lag)
	}
	// The held copy is one an earlier delivery landed: in and out alike, so
	// a redelivery reads no loss (#1659).
	r.wantDelivery(t, 2, 2, 0)
}

// wantDelivery fails unless the server's delivery figures (#1659) read in,
// out and loss for every kind.
func (r pipelineRig) wantDelivery(t *testing.T, in, out, loss float64) {
	t.Helper()
	for _, k := range delivery.Kinds {
		got := [3]float64{
			r.latest(t, writerclass.DeliveryInCount, string(k)),
			r.latest(t, writerclass.DeliveryOutCount, string(k)),
			r.latest(t, writerclass.DeliveryLossRatio, string(k)),
		}
		if got != [3]float64{in, out, loss} {
			t.Errorf("%s delivery in/out/loss = %v, want [%v %v %v]", k, got, in, out, loss)
		}
	}
}

// TestCalcPendingDepthMatchesTheQueue: calc marks left pending (waiting out
// a backoff, so no pass serves them) read as the window's calc-pending-depth.
func TestCalcPendingDepthMatchesTheQueue(t *testing.T) {
	r := newPipelineRig(t)
	all := scope.Set{All: true}
	if _, err := r.gw.CreateMetricType(r.ctx, "", storage.MetricTypeSpec{Name: "avg-temp", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bar-2", "bar-3"} {
		if _, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	if _, err := r.gw.CreateCalcRule(r.ctx, "", storage.CalcRuleSpec{Name: "avg-room", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "avg(t)"}, OutputKey: "avg-temp"}, all); err != nil {
		t.Fatalf("create calc rule: %v", err)
	}
	if _, err := r.conn.Exec(r.ctx, `delete from calc_pending`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.conn.Exec(r.ctx, `insert into calc_pending (rule_id, owner_kind, component_id, marked_at)
		select (select id from calc_rule where name = 'avg-room'), 'component', id, now() + interval '1 hour'
		from component where name in ('bar-1', 'bar-2', 'bar-3')`); err != nil {
		t.Fatalf("leave marks pending: %v", err)
	}
	srv := r.start(t, r.gw)
	srv.CalcPassNow(r.ctx)
	r.report(t, srv)

	if d := r.latest(t, writerclass.CalcPendingDepth, ""); d != 3 {
		t.Fatalf("calc-pending-depth = %v, want the 3 marks left pending", d)
	}
	r.latest(t, writerclass.CalcPassDuration, "")
}

// failRequests fails the command pull read and the heartbeat write, counting
// each. Every heartbeat past the second is held until hold closes: the
// heartbeats land one at a time, so the bus taking the third is the proof it
// is done with the second, and the held third adds nothing to the window.
type failRequests struct {
	*storage.PG
	pulls, beats atomic.Int32
	hold         chan struct{}
}

func (s *failRequests) PendingNodeCommands(context.Context, string) ([]storage.CommandDelivery, error) {
	s.pulls.Add(1)
	return nil, errors.New("the database blinked")
}

func (s *failRequests) RecordHeartbeat(context.Context, string) error {
	if s.beats.Add(1) > 2 {
		<-s.hold
	}
	return errors.New("the database blinked")
}

// TestRequestFailuresAreCounted: a command pull whose read fails counts
// under command-pull; heartbeats whose write fails count under heartbeat,
// each one, and log one WARN for the run, not one a beat.
func TestRequestFailuresAreCounted(t *testing.T) {
	r := newPipelineRig(t)
	store := &failRequests{PG: r.gw, hold: make(chan struct{})}
	srv := r.start(t, store)
	t.Cleanup(func() { close(store.hold) }) // before the bus shuts down
	nc := r.enrollNode(t, srv)

	logs := &syncBuffer{}
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prior)

	if _, err := nc.Request(collection.CommandSubject("node-a"), nil, 500*time.Millisecond); err == nil {
		t.Fatal("a command pull whose read failed was answered")
	}
	for range 3 {
		publishHeartbeat(t, nc, "node-a")
	}
	deadline := time.Now().Add(5 * time.Second)
	for store.beats.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("the bus took %d of 3 heartbeats", store.beats.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.report(t, srv)

	warned := logs.records(bus.HeartbeatFailLog)
	if len(warned) != 1 {
		t.Fatalf("two failed heartbeats logged %d warnings, want one for the run: %v", len(warned), warned)
	}
	if warned[0]["level"] != "WARN" || warned[0]["node"] != "node-a" {
		t.Fatalf("the heartbeat failure logged %v, want a WARN naming node-a", warned[0])
	}
	if n := r.latest(t, writerclass.BusRequestFailureCount, "command-pull"); n != 1 {
		t.Fatalf("bus-request-failure-count[command-pull] = %v, want 1", n)
	}
	if n := r.latest(t, writerclass.BusRequestFailureCount, "heartbeat"); n != 2 {
		t.Fatalf("bus-request-failure-count[heartbeat] = %v, want 2", n)
	}
	if n := r.latest(t, writerclass.BusRequestCount, "heartbeat"); n != 2 {
		t.Fatalf("bus-request-count[heartbeat] = %v, want 2", n)
	}
}
