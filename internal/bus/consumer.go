package bus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/key"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// telemetryStream is the JetStream stream capturing every node's telemetry
// publish; telemetryConsumer is the server's durable, at-least-once worker over
// it. The durable consumer IS the ingest worklist (no separate Postgres queue in
// this checkpoint): its handler derives, confines, writes, and acks inline.
//
// maxTelemetryDeliveries bounds how many times a single TelemetryBatch is
// redelivered before nakOrTerm gives up on it: without a bound, a
// permanently-failing message (a row the handler will never manage to write)
// would redeliver forever, the mirror problem to the stream's own retention
// (see startTelemetryConsumer).
//
// telemetryDuplicateWindow is how long the stream remembers a batch's
// Nats-Msg-Id (#453): a publisher retrying inside it (a node's next tick, the
// push route's bounded retry) is answered with the first copy's ack and
// nothing is stored twice. A retry past it is stored again, and the receipt
// (#311, LandBatch) keeps it from landing twice, so the window saves work and
// the receipt is the guarantee. receiptHorizon is how long a receipt is kept:
// far past the stream's redelivery (maxTelemetryDeliveries acks) and a
// node's retry of a batch it could not ship, ADR-0311.
const (
	telemetryStream          = "OG_TELEMETRY"
	telemetryConsumer        = "og-telemetry-worker"
	maxTelemetryDeliveries   = 5
	telemetryDuplicateWindow = 5 * time.Minute
	receiptHorizon           = 24 * time.Hour
)

// telemetryAckWait is how long the stream waits for the consumer's ack
// before it redelivers: a consumer that died with a batch unacked hands it
// to the next one after this. A var so a test can shorten it.
var telemetryAckWait = 30 * time.Second

// telemetryNakDelay is the base of the backoff a transiently failed batch is
// redelivered after (the delay grows with each delivery). A var so a test can
// shorten it.
var telemetryNakDelay = 500 * time.Millisecond

// startTelemetryConsumer creates (idempotently) the telemetry stream + durable
// consumer over the full-permission internal client and begins consuming. The
// stream persists a TelemetryBatch the instant a node publishes it, and (WorkQueuePolicy)
// deletes it the instant it is acked, so disk stays bounded to the current
// backlog rather than growing forever; the consumer redelivers a transient
// failure up to maxTelemetryDeliveries (nakOrTerm), so a DB hiccup never loses a
// sample but a permanently-failing one still leaves the queue.
//
// The stream keeps a duplicate window over each batch's Nats-Msg-Id (#453),
// and the stream's creation time is recorded as the epoch a batch with no id
// is keyed under (batchID).
func (s *Server) startTelemetryConsumer() error {
	js, err := jetstream.New(s.nc)
	if err != nil {
		return err
	}
	s.js = js
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       telemetryStream,
		Subjects:   []string{collection.TelemetryWildcard, collection.APITelemetrySubject},
		Retention:  jetstream.WorkQueuePolicy,
		Duplicates: telemetryDuplicateWindow,
	})
	if err != nil {
		return err
	}
	s.streamEpoch = stream.CachedInfo().Created.UTC().Format(time.RFC3339Nano)
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:    telemetryConsumer,
		AckPolicy:  jetstream.AckExplicitPolicy,
		AckWait:    telemetryAckWait,
		MaxDeliver: maxTelemetryDeliveries,
		// One batch in flight: nothing is delivered past a batch the
		// consumer has not acked or dropped, a redelivery included, so each
		// publisher's batches land in the order the stream stored them. The
		// property lane's transition guard (a value equal to its series'
		// latest is not written) is right only in that order (ADR-0311).
		MaxAckPending: 1,
	})
	if err != nil {
		return err
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) { s.handleTelemetry(msg) })
	if err != nil {
		return err
	}
	s.consumeCtx = cc
	return nil
}

// maxBatchIDLen bounds a Nats-Msg-Id the receipt keys verbatim; a longer one
// is keyed by its digest, so a publisher cannot make a receipt key the index
// refuses.
const maxBatchIDLen = 128

// batchID is a batch's idempotency key (#311): the subject it arrived on and
// its Nats-Msg-Id. The subject is the publisher's (a node's grant reaches only
// its own), so no publisher can claim another's id and suppress its batch. A
// batch with no id (a publisher from before #453) is keyed by its stream
// sequence under the stream's creation epoch, which holds across its own
// redeliveries though not across a publisher's retry; "" only when neither
// can be read, and the batch then lands with no receipt.
func (s *Server) batchID(msg jetstream.Msg) string {
	if id := msg.Headers().Get(jetstream.MsgIDHeader); id != "" {
		if len(id) > maxBatchIDLen {
			sum := sha256.Sum256([]byte(id))
			id = "sha256:" + hex.EncodeToString(sum[:])
		}
		return msg.Subject() + " " + id
	}
	meta, err := msg.Metadata()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s seq:%s/%d", msg.Subject(), s.streamEpoch, meta.Sequence.Stream)
}

// ack confirms a batch to the stream and waits for the stream to take it
// (the double ack): a batch whose ack is lost anyway is redelivered after
// telemetryAckWait, and its receipt makes the second landing a no-op.
func ack(msg jetstream.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := msg.DoubleAck(ctx); err != nil {
		slog.Warn("telemetry ack not confirmed, the batch will be redelivered and held by its receipt", "subject", msg.Subject(), "error", err)
	}
}

// handleTelemetry is the ingest handler for one telemetry Event: decode, bind +
// confine the owner, apply reject-not-project, write the surviving typed
// samples, and ack. The ack discipline is deliberate: a permanent condition
// (undecodable payload, or an orphan the confinement fence drops) is terminated /
// acked so it is not redelivered; only a transient failure (DB, registry read) is
// left for nakOrTerm, which redelivers (Nak) up to maxTelemetryDeliveries and then
// terminates the message so it does not loop forever.
func (s *Server) handleTelemetry(msg jetstream.Msg) {
	ctx := context.Background()

	var ev ogv1.TelemetryBatch
	if err := proto.Unmarshal(msg.Data(), &ev); err != nil {
		_ = msg.Term() // undecodable: it will never succeed, stop redelivery
		return
	}
	id := s.batchID(msg)

	// TRUST DECIDED BY SUBJECT, never by which fields are populated. The API lane
	// carries an owner the route already authorized; the node lane's owner is the
	// server's to resolve from the task's interface.
	if msg.Subject() == collection.APITelemetrySubject {
		bind, ok := apiBinding(&ev)
		if !ok {
			_ = msg.Term() // malformed owner: it will never resolve, stop redelivery
			return
		}
		if err := s.land(ctx, id, &ev, bind); err != nil {
			s.failed(msg, err)
			return
		}
		ack(msg)
		return
	}

	// The node published only its own telemetry subject (per-node grant), so the
	// node extracted from the subject is trusted, the same trust the heartbeat sink
	// relies on.
	node := collection.NodeFromSubject(msg.Subject())

	// A node does not get to assert its own owner. Populating owner on this lane is a
	// protocol error, not something to quietly ignore: ignoring it would leave a
	// field that looks authoritative and is not, so say so and stop redelivery.
	if ev.GetOwner() != nil {
		slog.Warn("node-lane batch asserted an owner, dropped", "node", node)
		_ = msg.Term()
		return
	}

	// The node lane's binding. Log lines are the publishing node's SELF-LOGS
	// (ADR-0066: untyped, no task, no registry gate) and land on node_log, while
	// samples are owned by the task's COMPONENT, so one batch carries two
	// destinations and the binding states both.
	bind := ingestBinding{LogNode: node}

	// Samples, and a task's collected log lines (#1145), are owner-bound to the
	// task's component, resolved only when there is something to bind: a
	// logs-only batch (a node's self-log tick) carries no task_id, and asking
	// the resolver about an empty task would read as an orphan.
	if taskBound(&ev) > 0 {
		// Owner + confinement: the owner is the task's interface component, and the
		// task must belong to THIS node. A task on another node, an unknown task, or
		// a shared interface resolves to !ok: the samples are orphans, never written
		// for a component the node was not placed on. The batch's log lines still
		// land, since they were never the task's to begin with.
		owner, ok, err := s.store.ResolveTaskOwner(ctx, ev.GetTaskId(), node)
		if err != nil {
			s.nakOrTerm(msg) // transient DB failure: redeliver (bounded)
			return
		}
		if ok {
			// The node's writer is bound to the task's endpoint: the only
			// instance a node-probe key lands under (ADR-0161).
			bind.SampleOwner = &owner
			bind.Arc = componentArc(owner)
			bind.Writer = storage.NodeWriter(owner.ComponentID, owner.EndpointName)
			bind.Writer.Node = owner.NodeID
		} else {
			// The fence drops these by design, but never silently (#1200): a
			// warning per node per window and a count on the node, so a node
			// stuck on a task it no longer holds reads as that, not as a
			// broken pipeline, and a flood of orphans is not a flood of logs.
			s.recordOrphan(node, ev.GetTaskId(), sampleCount(&ev), len(ev.GetTaskLogs()))
		}
	}

	if err := s.land(ctx, id, &ev, bind); err != nil {
		s.failed(msg, err) // a refused write is dropped; a transient failure redelivered (bounded)
		return
	}
	ack(msg)
}

// apiBinding builds the binding for a batch that arrived on the API push lane. The
// route already authorized the caller against this owner, so the batch lands on
// it: unlike the node lane, a push has one owner, not two.
//
// It returns false for a batch whose owner is missing or not addressable, which the
// caller terminates rather than redelivers: a malformed owner will never resolve.
// A push names a component or a system (ADR-0422); a location or a node owner is
// refused, since no route pushes for one and nothing authorized that arc. A
// system batch carries no log lines: log_line is component-only (#589, a log line
// comes off a component), so a system batch with one is refused whole rather
// than landing half of it; the route refuses the same batch with a 422 before it
// is ever published. Refusing here with a named error beats writing a row to the
// wrong home.
//
// o.GetRef() is copied through as-is, dual-accept per ADR-0062 (the push
// route's own input is documented "by name or id"): the route that publishes
// this wire message resolves the caller's owner and sends its id
// (internal/api/telemetry.go), so in practice this is always already a uuid,
// but nothing here assumes that. Downstream (InsertMetricSamples et al.)
// resolves either form the same way TaskOwner.ComponentID's node-lane sibling
// does.
func apiBinding(ev *ogv1.TelemetryBatch) (ingestBinding, bool) {
	o := ev.GetOwner()
	if o == nil || o.GetKind() == "" || o.GetRef() == "" {
		return ingestBinding{}, false
	}
	switch o.GetKind() {
	case "component":
		owner := storage.TaskOwner{ComponentID: o.GetRef()}
		return ingestBinding{
			LogComponent: o.GetRef(),
			SampleOwner:  &owner,
			Arc:          componentArc(owner),
			Writer:       storage.PushWriter(),
		}, true
	case "system":
		if len(ev.GetLogs()) > 0 {
			slog.Warn("push batch refused: a system owns no log lines (log_line is component-owned), batch dropped",
				"ref", o.GetRef(), "logs", len(ev.GetLogs()))
			return ingestBinding{}, false
		}
		// No component and no endpoint: a system's samples are owner-level,
		// and nothing about this batch is a task's.
		return ingestBinding{
			SampleOwner: &storage.TaskOwner{},
			Arc:         sampleArc{Kind: "system", ID: o.GetRef()},
			Writer:      storage.PushWriter(),
		}, true
	}
	slog.Warn("push batch refused: a push names a component or a system, batch dropped",
		"kind", o.GetKind(), "ref", o.GetRef())
	return ingestBinding{}, false
}

// sampleArc is the owner arc a batch's samples land on: an owner_kind and
// the owner's id (or a name ADR-0062 resolves). It is the binding's, never a
// lane derivation's own guess (#422).
type sampleArc struct {
	Kind string
	ID   string
}

// componentArc is the arc of a task's (or a component push's) samples: the
// component the owner names.
func componentArc(owner storage.TaskOwner) sampleArc {
	return sampleArc{Kind: "component", ID: owner.ComponentID}
}

// ingestBinding is the resolved destination for one batch: where its log lines
// land and where its samples land. They differ by lane, which is the whole
// reason this is explicit rather than a single owner. On the node lane, log
// lines are the node's self-logs (node_log) while samples are owned by the
// task's component. On the push lane, both land under the component the API
// authorized, or the samples alone under the system it authorized. At most one
// of LogNode / LogComponent is set per lane; a nil SampleOwner means the
// batch's samples must not be written (no samples, or an orphan task). Arc is
// the owner arc the samples land on, set with SampleOwner; SampleOwner itself
// carries the observing endpoint, if any. Writer is who the samples speak
// for, which the Storage Gateway holds every key to (ADR-0161): the node bound
// to its task's endpoint, or the push lane.
type ingestBinding struct {
	LogComponent string
	LogNode      string
	SampleOwner  *storage.TaskOwner
	Arc          sampleArc
	Writer       storage.KeyWriter
}

// sampleCount is how many registry-resolved observations a batch carries across
// its three typed lanes. Zero means no registry to read.
func sampleCount(ev *ogv1.TelemetryBatch) int {
	return len(ev.GetMetrics()) + len(ev.GetProperties()) + len(ev.GetEvents())
}

// taskBound is how much of a batch belongs to its task: its samples and the
// log lines the task collected (#1145). Zero means a self-log batch: no owner
// to resolve.
func taskBound(ev *ogv1.TelemetryBatch) int {
	return sampleCount(ev) + len(ev.GetTaskLogs())
}

// land writes one batch to its sinks: the raw log lines and the three typed
// lanes (#594: the lane is the array, so each validates against its own
// catalog). It is the ONE write path, and both ingest lanes call it, so a new
// lane cannot drift from the node lane's semantics (reject-not-project,
// per-lane validation, the transition-only state guard).
//
// It deliberately knows nothing about JetStream: no message, no ack, no nak. The
// caller owns delivery semantics and decides what a returned error means, which is
// what lets a synchronous API caller reuse it. A returned error is always transient
// (a registry read or a sink write); anything permanent is dropped in place, since
// re-delivering it would not help.
//
// The batch lands as ONE unit under id, its receipt (#311, ADR-0311): every sink
// in one transaction, so a failure part way writes nothing a redelivery would
// write again, and an id that already landed writes nothing at all, so a
// redelivery after a lost ack, a consumer that died after its commit, or a
// publisher's retry past the stream's duplicate window lands each row once.
func (s *Server) land(ctx context.Context, id string, ev *ogv1.TelemetryBatch, bind ingestBinding) error {
	batch := storage.IngestBatch{BatchID: id, Writer: bind.Writer}
	// The raw log sink, routed by lane: a node's self-logs to node_log, a push's
	// component-owned lines to log_line (#589). The storage layer refuses any
	// other binding, so a future lane cannot quietly widen the log owner.
	if bind.LogNode != "" {
		batch.NodeLogs = nodeLogWrites(ev, bind.LogNode)
	} else if bind.LogComponent != "" {
		batch.LogLines = logLineWrites(ev, bind.LogComponent)
	}
	// A task's collected log lines (#1145), on the node lane alone: they land
	// on the task's component, owner-bound server-side from the task as every
	// sample is, so a node can land a line only for a component its own task
	// collects for. An orphan batch (no resolved owner) lands none.
	if bind.LogNode != "" && bind.SampleOwner != nil {
		batch.LogLines = append(batch.LogLines, taskLogWrites(ev, *bind.SampleOwner)...)
	}
	if bind.SampleOwner != nil && sampleCount(ev) > 0 {
		// The registry is kept between batches and rebuilt only when the
		// catalogs change (#1279): one fingerprint read, not three catalogs.
		// The command-targeted property names ride with it (#1299).
		reg, targeted, err := s.registry.get(ctx, s.store)
		if err != nil {
			return err
		}
		batch.CommandTargeted = targeted
		// Each lane derives against its own catalog (#594): the array the sample rode
		// says which table it lands in, and the catalog row says what a valid value
		// looks like. All three survive the SAME owner binding and reject-not-project;
		// the split is only the sink, not a second trust decision. The property lane
		// is transition-only: a value equal to its series' latest is not written, the
		// guard held in the insert itself (LandBatch), so a restart that re-emits an
		// unchanged verdict adds no row, under any dispatch (#455).
		batch.Metrics = deriveMetrics(ev, bind.Arc, *bind.SampleOwner, reg, bind.Writer)
		batch.Properties = deriveProperties(ev, bind.Arc, *bind.SampleOwner, reg, bind.Writer)
		batch.Events = deriveEvents(ev, bind.Arc, *bind.SampleOwner, reg, bind.Writer)
	}
	if _, err := s.store.LandBatch(ctx, batch); err != nil {
		return err
	}
	// The endpoint delivered this batch, whatever the sinks kept of it: an
	// instanced series, an unchanged property the guard skipped, a sample a
	// node's clock dated ahead, or a copy of a batch that already landed. Its
	// freshness is the server's receipt (#933), stamped after the batch's
	// transaction so it adds no lock to it. The push lane has no endpoint, so
	// nothing is stamped there.
	if bind.SampleOwner != nil && taskBound(ev) > 0 {
		if id := bind.SampleOwner.EndpointID; id != "" {
			return s.store.StampEndpointsSeen(ctx, []string{id})
		}
	}
	return nil
}

// failed answers a batch whose landing failed. A write the gateway refuses
// outright (storage.IsRefusedWrite: its owner gone, its key's writer class
// refusing the writer, a value Postgres refuses as data or by a CHECK) is Term'd at once,
// since one batch in flight means every batch behind it waits on it; any
// other failure is redelivered, bounded (nakOrTerm).
func (s *Server) failed(msg jetstream.Msg, err error) {
	if storage.IsRefusedWrite(err) {
		_ = msg.Term()
		slog.Warn("telemetry batch refused, dropped", "subject", msg.Subject(), "error", err.Error())
		return
	}
	s.nakOrTerm(msg)
}

// nakOrTerm redelivers a telemetry message that failed for a transient reason
// (a Nak with a delay that grows with each delivery, so a database that is
// briefly away is waited out rather than raced), unless it has already been
// delivered maxTelemetryDeliveries times (or its delivery count can't be
// read), in which case it is Term'd: deleted from the OG_TELEMETRY work queue
// rather than left to redeliver forever. A message that only ever Nak's never
// leaves a work-queue-retention stream on its own, so this is what actually
// bounds the redelivery loop; ConsumerConfig.MaxDeliver is the JetStream-side
// backstop for the same bound. With one batch in flight, the redelivery comes
// before anything behind it.
func (s *Server) nakOrTerm(msg jetstream.Msg) {
	meta, err := msg.Metadata()
	if err != nil {
		_ = msg.Term()
		slog.Warn("telemetry message dropped: could not read delivery metadata", "subject", msg.Subject(), "error", err)
		return
	}
	if meta.NumDelivered >= maxTelemetryDeliveries {
		_ = msg.Term()
		slog.Warn("telemetry message dropped after repeated failures", "subject", msg.Subject(), "delivered", meta.NumDelivered)
		return
	}
	_ = msg.NakWithDelay(time.Duration(meta.NumDelivered) * telemetryNakDelay)
}

// logLineWrites turns a push batch's raw log lines into component-owned
// log_line writes (ADR-0066, narrowed by #589): the component is the owner the
// API authorized. Pure: no I/O and no registry (a log line is untyped by
// design). Each line's own ts and source win when set, else the batch's; empty
// severity/facility/correlation stay "" and the storage layer maps them to NULL.
func logLineWrites(ev *ogv1.TelemetryBatch, component string) []storage.LogLineWrite {
	logs := ev.GetLogs()
	if len(logs) == 0 {
		return nil
	}
	out := make([]storage.LogLineWrite, 0, len(logs))
	for _, l := range logs {
		out = append(out, storage.LogLineWrite{
			OwnerKind:     "component",
			OwnerID:       component,
			Instance:      l.GetInstance(),
			Source:        lineSource(ev, l),
			Severity:      l.GetSeverity(),
			Facility:      l.GetFacility(),
			Message:       l.GetMessage(),
			Attributes:    l.GetAttributes(),
			Labels:        l.GetLabels(),
			CorrelationID: l.GetCorrelationId(),
			TS:            lineTime(ev, l),
		})
	}
	return out
}

// taskLogWrites turns a node-lane batch's task_logs (#1145) into log_line
// writes on the task's component: its collecting endpoint is the line's
// instance, and a line naming no source takes the endpoint's transport. Pure,
// and the same ts resolution as a push's lines; the severity table and the
// caps are the Storage Gateway's, held on every log_line write.
func taskLogWrites(ev *ogv1.TelemetryBatch, owner storage.TaskOwner) []storage.LogLineWrite {
	logs := ev.GetTaskLogs()
	if len(logs) == 0 {
		return nil
	}
	out := make([]storage.LogLineWrite, 0, len(logs))
	for _, l := range logs {
		source := l.GetSource()
		if source == "" {
			source = owner.Transport
		}
		out = append(out, storage.LogLineWrite{
			OwnerKind:     "component",
			OwnerID:       owner.ComponentID,
			Instance:      owner.EndpointName,
			Source:        source,
			Severity:      l.GetSeverity(),
			Facility:      l.GetFacility(),
			Message:       l.GetMessage(),
			Attributes:    l.GetAttributes(),
			Labels:        l.GetLabels(),
			CorrelationID: l.GetCorrelationId(),
			TS:            lineTime(ev, l),
		})
	}
	return out
}

// nodeLogWrites turns a node-lane batch's raw log lines into node_log writes:
// the node's self-logs (ADR-0066), owned by the publishing node the subject
// authenticated. Pure, and the same per-line ts and source resolution as
// logLineWrites, so the two lanes cannot drift on the fallbacks.
func nodeLogWrites(ev *ogv1.TelemetryBatch, node string) []storage.NodeLogWrite {
	logs := ev.GetLogs()
	if len(logs) == 0 {
		return nil
	}
	out := make([]storage.NodeLogWrite, 0, len(logs))
	for _, l := range logs {
		out = append(out, storage.NodeLogWrite{
			Node:          node,
			Source:        lineSource(ev, l),
			Severity:      l.GetSeverity(),
			Facility:      l.GetFacility(),
			Message:       l.GetMessage(),
			Attributes:    l.GetAttributes(),
			Labels:        l.GetLabels(),
			CorrelationID: l.GetCorrelationId(),
			TS:            lineTime(ev, l),
		})
	}
	return out
}

// lineTime resolves one log line's timestamp: its own ts wins, else the batch
// ts; when neither is set it stays zero so the storage layer stamps now()
// (GetTs().AsTime() on a nil timestamp is the 1970 epoch, not the zero time,
// and would slip past that guard).
func lineTime(ev *ogv1.TelemetryBatch, l *ogv1.LogLine) time.Time {
	switch {
	case l.GetTs() != nil:
		return l.GetTs().AsTime()
	case ev.GetTs() != nil:
		return ev.GetTs().AsTime()
	}
	return time.Time{}
}

// lineSource resolves one log line's source: its own wins, else the batch's (a
// push declares it once for the batch; a node's self-logs set it per line).
func lineSource(ev *ogv1.TelemetryBatch, l *ogv1.LogLine) string {
	if s := l.GetSource(); s != "" {
		return s
	}
	return ev.GetSource()
}

// The per-lane derivations (#594). Each turns one typed lane of a decoded
// TelemetryBatch + its resolved owner into the rows to persist. Pure: no I/O.
// reject-not-project is per lane: a sample whose name is not registered in the
// lane's OWN catalog is dropped, even when another catalog knows the name. The
// owner is stamped identically on every lane from the binding: the arc it
// resolved (the task's component on the node lane, the authorized component or
// system on the push lane), source=the endpoint's transport, instance=the
// endpoint's name where the sample names none;
// provenance is observed (the insert path fixes that). A sample whose type's
// writer class does not admit the batch's writer is dropped too (ADR-0161; the
// gateway refuses it again, so this keeps one stray row from failing a whole
// batch). A validation failure is permanent, so it is dropped in place (with a
// warning: the node lane has no reply channel), never returned as an error the
// caller would redeliver.

// deriveMetrics maps the metric lane to metric rows: the name must be a
// registered metric_type. The value is a double by wire type, so there is no
// shape to validate beyond the name.
func deriveMetrics(ev *ogv1.TelemetryBatch, arc sampleArc, owner storage.TaskOwner, reg collection.Registry, w storage.KeyWriter) []storage.MetricSampleWrite {
	var out []storage.MetricSampleWrite
	for _, dp := range ev.GetMetrics() {
		mt, ok := reg.Metric(dp.GetName())
		if !ok {
			continue // reject-not-project: not a metric_type name
		}
		instance, ok := laneInstance(mt.WriterClass, dp.GetName(), dp.GetInstance(), owner, w)
		if !ok {
			continue
		}
		out = append(out, storage.MetricSampleWrite{
			OwnerKind: arc.Kind,
			OwnerID:   arc.ID,
			Key:       dp.GetName(),
			Instance:  instance,
			Value:     dp.GetValue(),
			Source:    laneSource(ev, owner),
			TS:        sampleTime(ev, dp.GetTs()),
		})
	}
	return out
}

// deriveProperties maps the property lane to property rows: the name must be a
// registered property_type, the value must be canonical JSON of the type's
// data_type, and when the type declares a validation schema the value must
// satisfy it (key.ValidateValue, the same validator every registered key obeys).
func deriveProperties(ev *ogv1.TelemetryBatch, arc sampleArc, owner storage.TaskOwner, reg collection.Registry, w storage.KeyWriter) []storage.PropertySampleWrite {
	var out []storage.PropertySampleWrite
	for _, dp := range ev.GetProperties() {
		pt, ok := reg.Property(dp.GetName())
		if !ok {
			continue // reject-not-project: not a property_type name
		}
		instance, ok := laneInstance(pt.WriterClass, dp.GetName(), dp.GetInstance(), owner, w)
		if !ok {
			continue
		}
		raw := json.RawMessage(dp.GetValueJson())
		if err := key.ValidateValue(pt.DataType, raw, pt.Validation); err != nil {
			slog.Warn("property sample refused: value fails the type's validation",
				"name", dp.GetName(), "error", err)
			continue
		}
		out = append(out, storage.PropertySampleWrite{
			OwnerKind: arc.Kind,
			OwnerID:   arc.ID,
			Key:       dp.GetName(),
			Instance:  instance,
			Value:     propertyText(raw),
			Source:    laneSource(ev, owner),
			TS:        sampleTime(ev, dp.GetTs()),
		})
	}
	return out
}

// deriveEvents maps the event lane to caught occurrences: the name must be a
// registered event_type, a payload must be valid JSON satisfying the type's
// payload_schema when one is declared, and an entry carrying neither message
// nor payload says nothing (dropped, as the old oneof wire dropped a valueless
// sample). A component published the occurrence natively (an xAPI event, an
// SNMP trap): origin caught, the platform did not derive it. Raw log lines are
// a separate ingest lane, not this path (ADR-0066).
func deriveEvents(ev *ogv1.TelemetryBatch, arc sampleArc, owner storage.TaskOwner, reg collection.Registry, w storage.KeyWriter) []storage.EventWrite {
	var out []storage.EventWrite
	for _, dp := range ev.GetEvents() {
		et, ok := reg.Event(dp.GetName())
		if !ok {
			continue // reject-not-project: not an event_type name
		}
		instance, ok := laneInstance(et.WriterClass, dp.GetName(), dp.GetInstance(), owner, w)
		if !ok {
			continue
		}
		payload := dp.GetPayload()
		if dp.GetMessage() == "" && len(payload) == 0 {
			continue // an occurrence with no content says nothing
		}
		if len(payload) > 0 {
			// data_type json pins no base type, so the payload_schema (or nothing)
			// governs; the JSON well-formedness check always runs.
			if err := key.ValidateValue("json", payload, et.PayloadSchema); err != nil {
				slog.Warn("event sample refused: payload fails the event_type's schema",
					"name", dp.GetName(), "error", err)
				continue
			}
		}
		out = append(out, storage.EventWrite{
			OwnerKind:  arc.Kind,
			OwnerID:    arc.ID,
			Key:        dp.GetName(),
			Instance:   instance,
			Origin:     "caught",
			Message:    dp.GetMessage(),
			Attributes: payload,
			Source:     laneSource(ev, owner),
			TS:         sampleTime(ev, dp.GetTs()),
		})
	}
	return out
}

// laneInstance resolves a sample's instance discriminator, or refuses the
// sample (ok false). The wire value wins where supplied, else the task
// endpoint's name: the node lane sets none (its endpoint name IS the
// discriminator), a driver may name its own (one series per port), and a push
// has no endpoint, so it must be able to say it.
//
// Two refusals come first (ADR-0161). A sample whose type's writer class does
// not admit the batch's writer is dropped: a push of a calc rule's output, a
// node lane carrying a key a stored driver may no longer emit. And a node-probe
// key is the node's account of the endpoint whose task it ran, so on the node
// lane its instance is that endpoint's name: empty is filled with it, and any
// other (another node's endpoint on the same component, a name no endpoint
// holds) is refused, never written into a series the console reads as that
// endpoint's.
func laneInstance(class writerclass.Class, name, instance string, owner storage.TaskOwner, w storage.KeyWriter) (string, bool) {
	if !writerclass.Admits(class, w.Kind, name) {
		slog.Warn("sample refused: its writer class does not admit the lane",
			"name", name, "class", class, "writer", w.Kind)
		return "", false
	}
	if class == writerclass.NodeProbe && w.Kind == writerclass.Node {
		if owner.EndpointName == "" || (instance != "" && instance != owner.EndpointName) {
			slog.Warn("node-probe sample refused: a node writes one only under its task's endpoint",
				"name", name, "instance", instance, "endpoint", owner.EndpointName)
			return "", false
		}
		return owner.EndpointName, true
	}
	if instance != "" {
		return instance, true
	}
	return owner.EndpointName, true
}

// laneSource resolves a batch's provenance source: the batch's own wins, else
// the task interface's type, the same precedence as laneInstance and for the
// same reason.
func laneSource(ev *ogv1.TelemetryBatch, owner storage.TaskOwner) string {
	if s := ev.GetSource(); s != "" {
		return s
	}
	return owner.Transport
}

// propertyText maps a validated canonical-JSON property value to the text form
// the property sink stores (to_jsonb over text): a JSON string lands unquoted,
// any other shape lands as its compact JSON text, so the stored encoding is
// unchanged from the pre-lane wire and the transition guard keeps comparing
// like with like.
func propertyText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err == nil {
		return buf.String()
	}
	return string(raw)
}

// sampleTime resolves the timestamp for one sample on any lane, the #594 ts
// rule: a supplied per-sample ts survives verbatim, the batch ts fills an
// absent one, and zero means the insert path stamps ingest time.
func sampleTime(ev *ogv1.TelemetryBatch, ts *timestamppb.Timestamp) time.Time {
	if ts != nil {
		return ts.AsTime()
	}
	if ev.GetTs() != nil {
		return ev.GetTs().AsTime()
	}
	return time.Time{}
}
