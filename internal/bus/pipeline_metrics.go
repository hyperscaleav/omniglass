package bus

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// The server's pipeline, measured from inside (#1628): how far behind ingest
// lands, how deep the telemetry consumer and the calc and outbox queues run,
// whether the bus request-reply paths fail, all recorded through the
// process's platformmetric.Recorder (#1627) and landed on the server's
// platform component once a report window. A failure count always has its
// attempts beside it (ingest-batch-count, bus-request-count, sweep-count), so
// every loss figure reads as a ratio where 0 is healthy; the push lane's
// refusals stand beside push-lane-publish-count the same way.

// pipelineMetrics is the bus's handle on the recorder. Its zero value records
// nothing, so a Server built without one (a fixture) instruments as a no-op.
type pipelineMetrics struct{ rec *platformmetric.Recorder }

// count adds n to a window count; a count of nothing is no activity.
func (m pipelineMetrics) count(key, instance string, n float64) {
	if m.rec != nil && n > 0 {
		m.rec.Count(key, instance, n)
	}
}

func (m pipelineMetrics) mean(key, instance string, v float64) {
	if m.rec != nil {
		m.rec.Mean(key, instance, v)
	}
}

func (m pipelineMetrics) max(key, instance string, v float64) {
	if m.rec != nil {
		m.rec.Max(key, instance, v)
	}
}

func (m pipelineMetrics) quantile(key, instance string, q float64, d time.Duration) {
	if m.rec != nil {
		m.rec.Quantile(key, instance, q, d)
	}
}

// pushTurn records one caller's wait for the push lane's publish turn
// (#1746): a publish, a refusal when the wait ran out, and the wait toward
// the window's p99 and max in milliseconds, a refusal's whole bound included.
func (m pipelineMetrics) pushTurn(t delivery.Turn) {
	m.count(writerclass.PushLanePublishCount, "", 1)
	refused := 0.0
	if t.Refused {
		refused = 1
	}
	// Counted even at zero, unlike a quiet count: a window of publishes lands
	// its refusals beside them, so the pair reads "0 / n", healthy, rather
	// than a refusal figure never reported.
	if m.rec != nil {
		m.rec.Count(writerclass.PushLaneRefusalCount, "", refused)
	}
	m.quantile(writerclass.PushLaneTurnWaitP99, "", 0.99, t.Wait)
	m.max(writerclass.PushLaneTurnWaitMax, "", float64(t.Wait)/float64(time.Millisecond))
}

func (m pipelineMetrics) gauge(key, instance string, v float64) {
	if m.rec != nil {
		m.rec.Gauge(key, instance, v)
	}
}

// landing is what one committed batch carried, for its lag: how many samples
// across its typed lanes and its groups', and the newest and oldest of their
// timestamps. A sample with no ts on it, its group or its batch counts as
// throughput but not toward lag (the insert stamps it at ingest, so it was
// never late).
type landing struct {
	samples        int
	newest, oldest time.Time
}

func (l *landing) observe(ts time.Time) {
	l.samples++
	if ts.IsZero() {
		return
	}
	if l.oldest.IsZero() || ts.Before(l.oldest) {
		l.oldest = ts
	}
	if ts.After(l.newest) {
		l.newest = ts
	}
}

// lanes observes the typed samples b carries under the same ts rule its
// rows land by (sampleTime): b is a batch, or one group's view of its tick
// (groupView).
func (l *landing) lanes(b *ogv1.TelemetryBatch) {
	for _, s := range b.GetMetrics() {
		l.observe(sampleTime(b, s.GetTs()))
	}
	for _, s := range b.GetProperties() {
		l.observe(sampleTime(b, s.GetTs()))
	}
	for _, s := range b.GetEvents() {
		l.observe(sampleTime(b, s.GetTs()))
	}
}

// batchLanding reads a batch's landing: its own lanes and every group's.
// Pure.
func batchLanding(ev *ogv1.TelemetryBatch) landing {
	var l landing
	l.lanes(ev)
	for _, g := range ev.GetGroups() {
		l.lanes(groupView(ev, g))
	}
	return l
}

// observeLanding records a committed batch's lag at now, its landing time:
// the window mean of ingest-lag over each batch's newest and oldest sample,
// the window max of ingest-lag-max over its oldest, and its samples into
// ingest-sample-count. The lag is server time minus the sample's ts, so a
// node clock ahead of the server reads short (even negative) and one behind
// reads long: kept as read, the skew is the clock's to fix (#419). A batch
// with no samples observes nothing.
func observeLanding(m pipelineMetrics, now time.Time, l landing) {
	if l.samples == 0 {
		return
	}
	m.count(writerclass.IngestSampleCount, "", float64(l.samples))
	if l.oldest.IsZero() {
		return
	}
	m.mean(writerclass.IngestLag, "", now.Sub(l.newest).Seconds())
	m.mean(writerclass.IngestLag, "", now.Sub(l.oldest).Seconds())
	m.max(writerclass.IngestLagMax, "", now.Sub(l.oldest).Seconds())
}

// committed records a batch whose transaction has just landed it: one more
// batch, the samples it landed and their lag, at the moment it committed. A
// batch its receipt held to a no-op (LandResult.Landed false: a redelivery or
// a replay) landed nothing and is never recorded, and a node tick's landing
// is only its groups that landed, never one the fence or the gateway dropped.
func (s *Server) committed(l landing) {
	s.metrics.count(writerclass.IngestBatchCount, "", 1)
	observeLanding(s.metrics, time.Now(), l)
}

// drop terminates a telemetry batch and counts its items as lost at the
// server's hop (#1659). An undecodable batch has no items it can name, so it
// counts none; the ledger reads its items missing.
func (s *Server) drop(msg interface{ Term() error }, items map[delivery.Kind]int) {
	_ = msg.Term()
	s.flow.Lost(items)
}

// settled counts a landing's items at the server's hop (#1659): the store's
// outcome for a batch that landed, and for one its receipt held (a copy an
// earlier delivery landed), every item in and out, so a redelivery reads no
// loss.
func (s *Server) settled(ev *ogv1.TelemetryBatch, res storage.LandResult) {
	if res.Landed {
		s.flow.Landed(res.Outcome)
		return
	}
	s.flow.Delivered(delivery.BatchItems(ev))
}

// The request-reply paths, the instances of bus-request-count and
// bus-request-failure-count.
const (
	requestHeartbeat     = "heartbeat"
	requestCommandPull   = "command-pull"
	requestCommandReport = "command-report"
)

// request records one request on path, and whether it failed.
func (s *Server) request(path string, err error) {
	s.metrics.count(writerclass.BusRequestCount, path, 1)
	if err != nil {
		s.metrics.count(writerclass.BusRequestFailureCount, path, 1)
	}
}

// failOnce says when a failing path's warning is news: the first failure for
// a key, and not again until that key has succeeded, so a node heartbeating
// into a database that is down logs once, not every beat.
type failOnce struct {
	mu      sync.Mutex
	failing map[string]bool
}

// failed marks key failing, true when it was not already.
func (f *failOnce) failed(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing[key] {
		return false
	}
	if f.failing == nil {
		f.failing = map[string]bool{}
	}
	f.failing[key] = true
	return true
}

// ok clears key, so its next failure is news again.
func (f *failOnce) ok(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.failing, key)
}

// heartbeatFailLog is the warning a node's heartbeat write failure logs, once
// per run of failures.
const heartbeatFailLog = "heartbeat not recorded, the node may read as silent"

// recordHeartbeat records node's heartbeat, counting it and its failure.
func (s *Server) recordHeartbeat(ctx context.Context, node string) {
	err := s.store.RecordHeartbeat(ctx, node)
	s.request(requestHeartbeat, err)
	if err != nil {
		if s.heartbeatFail.failed(node) {
			slog.Warn(heartbeatFailLog, "facility", "bus", "node", node, "error", err.Error())
		}
		return
	}
	s.heartbeatFail.ok(node)
}

// The sweeps the settlement tick times, sweep-*'s instances.
const (
	sweepCommandSettlement = "command-settlement"
	sweepEventRule         = "event-rule"
)

// sweep runs one sweep, recording its duration, that it ran, and whether it
// failed. It returns the sweep's error.
func (s *Server) sweep(name string, run func() error) error {
	start := time.Now()
	err := run()
	s.metrics.mean(writerclass.SweepDuration, name, float64(time.Since(start))/float64(time.Millisecond))
	s.metrics.count(writerclass.SweepCount, name, 1)
	if err != nil {
		s.metrics.count(writerclass.SweepFailureCount, name, 1)
	}
	return err
}

// worklistOutcomes names each worklist pull outcome, bus-worklist-pull-count's
// instances, in WorklistCounts order.
var worklistOutcomes = []string{"served", "read-failed", "oversize", "respond-failed", "expired", "abandoned", "busy"}

func (c WorklistCounts) values() []int64 {
	return []int64{c.Served, c.ReadFailed, c.Oversize, c.RespondFailed, c.Expired, c.Abandoned, c.Busy}
}

// worklistDeltas is each outcome's count between two reads of the running
// totals: the window's own, never negative (a lower read is a fresh tally,
// not activity), and only the outcomes that moved. Pure.
func worklistDeltas(prev, cur WorklistCounts) map[string]int64 {
	out := map[string]int64{}
	p, c := prev.values(), cur.values()
	for i, name := range worklistOutcomes {
		if d := c[i] - p[i]; d > 0 {
			out[name] = d
		}
	}
	return out
}

// worklistSampler counts each window's worklist pulls by outcome from read,
// the server's running totals, keeping the last read so each window counts
// only its own.
func worklistSampler(read func() WorklistCounts) func(pipelineMetrics) {
	prev := read()
	return func(m pipelineMetrics) {
		cur := read()
		for name, d := range worklistDeltas(prev, cur) {
			m.count(writerclass.BusWorklistPullCount, name, float64(d))
		}
		prev = cur
	}
}

// laneBusy is the fraction of wall the ingest lane spent busy between two
// tallies. Pure.
func laneBusy(prev, cur LaneTally, wall time.Duration) float64 {
	if wall <= 0 {
		return 0
	}
	return max(cur.Busy-prev.Busy, 0).Seconds() / wall.Seconds()
}

// Sampler is the bus's window-close reading for a platformmetric.Reporter:
// the telemetry consumer's backlog, the ingest lane's busy fraction, the
// window's worklist pulls by outcome and its largest worklist reply.
func (s *Server) Sampler() platformmetric.Sampler {
	worklist := worklistSampler(s.WorklistCounts)
	prevLane, prevAt := s.LaneTally(), time.Now()
	return func(ctx context.Context, r *platformmetric.Recorder) {
		m := pipelineMetrics{r}
		worklist(m)
		now, lane := time.Now(), s.LaneTally()
		m.gauge(writerclass.IngestLaneBusy, "", laneBusy(prevLane, lane, now.Sub(prevAt)))
		prevLane, prevAt = lane, now
		for _, b := range s.WorklistReplyBytes() {
			m.max(writerclass.BusWorklistReplyBytesMax, "", float64(b))
		}
		s.sampleConsumer(ctx, m)
		for _, rd := range s.flow.Take().Readings() {
			m.gauge(rd.Key, rd.Instance, rd.Value)
		}
	}
}

// sampleConsumer gauges the telemetry consumer's backlog: the batches stored
// and not yet delivered, delivered and not yet acked, and redelivered. A read
// that fails says nothing this window rather than a zero that did not happen.
func (s *Server) sampleConsumer(ctx context.Context, m pipelineMetrics) {
	if s.cons == nil {
		return
	}
	ictx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	info, err := s.cons.Info(ictx)
	if err != nil {
		slog.Debug("telemetry consumer info unread", "facility", "ingest", "error", err.Error())
		return
	}
	m.gauge(writerclass.BusConsumerPending, "", float64(info.NumPending))
	m.gauge(writerclass.BusConsumerAckPending, "", float64(info.NumAckPending))
	m.gauge(writerclass.BusConsumerRedelivered, "", float64(info.NumRedelivered))
}
