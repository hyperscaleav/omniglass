package bus

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
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
const (
	telemetryStream        = "OG_TELEMETRY"
	telemetryConsumer      = "og-telemetry-worker"
	maxTelemetryDeliveries = 5
)

// startTelemetryConsumer creates (idempotently) the telemetry stream + durable
// consumer over the full-permission internal client and begins consuming. The
// stream persists a TelemetryBatch the instant a node publishes it, and (WorkQueuePolicy)
// deletes it the instant it is acked, so disk stays bounded to the current
// backlog rather than growing forever; the consumer redelivers a transient
// failure up to maxTelemetryDeliveries (nakOrTerm), so a DB hiccup never loses a
// sample but a permanently-failing one still leaves the queue.
func (s *Server) startTelemetryConsumer() error {
	js, err := jetstream.New(s.nc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      telemetryStream,
		Subjects:  []string{collection.TelemetryWildcard, collection.APITelemetrySubject},
		Retention: jetstream.WorkQueuePolicy,
	})
	if err != nil {
		return err
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:    telemetryConsumer,
		AckPolicy:  jetstream.AckExplicitPolicy,
		MaxDeliver: maxTelemetryDeliveries,
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

	// TRUST DECIDED BY SUBJECT, never by which fields are populated. The API lane
	// carries an owner the route already authorized; the node lane's owner is the
	// server's to resolve from the task's interface.
	if msg.Subject() == collection.APITelemetrySubject {
		bind, ok := apiBinding(&ev)
		if !ok {
			_ = msg.Term() // malformed owner: it will never resolve, stop redelivery
			return
		}
		if err := s.land(ctx, &ev, bind); err != nil {
			s.nakOrTerm(msg)
			return
		}
		_ = msg.Ack()
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

	// Samples are owner-bound to the task's component, resolved only when there are
	// samples to bind: a logs-only batch (a node's self-log tick) carries no task_id,
	// and asking the resolver about an empty task would read as an orphan.
	if len(ev.GetSamples()) > 0 {
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
			bind.SampleOwner = &owner
		}
	}

	if err := s.land(ctx, &ev, bind); err != nil {
		s.nakOrTerm(msg) // transient registry read or DB write failure: redeliver (bounded)
		return
	}
	_ = msg.Ack()
}

// apiBinding builds the binding for a batch that arrived on the API push lane. The
// route already authorized the caller against this owner, so both the samples and
// the log lines land on it: unlike the node lane, a push has one owner, not two.
//
// It returns false for a batch whose owner is missing or not addressable, which the
// caller terminates rather than redelivers: a malformed owner will never resolve.
// Only component owners are accepted: log_line is component-only (#589, a log
// line comes off a component), and deriveSamples hardcodes owner_kind=component
// downstream (widening the sample arms is #422). Refusing here with a named
// error beats writing either row to the wrong home.
func apiBinding(ev *ogv1.TelemetryBatch) (ingestBinding, bool) {
	o := ev.GetOwner()
	if o == nil || o.GetKind() == "" || o.GetRef() == "" {
		return ingestBinding{}, false
	}
	if o.GetKind() != "component" {
		slog.Warn("push batch refused: samples and log lines are component-owned, batch dropped",
			"kind", o.GetKind(), "ref", o.GetRef())
		return ingestBinding{}, false
	}
	owner := storage.TaskOwner{Component: o.GetRef()}
	return ingestBinding{
		LogComponent: o.GetRef(),
		SampleOwner:  &owner,
	}, true
}

// ingestBinding is the resolved destination for one batch: where its log lines
// land and where its samples land. They differ by lane, which is the whole
// reason this is explicit rather than a single owner. On the node lane, log
// lines are the node's self-logs (node_log) while samples are owned by the
// task's component. On the push lane, both land under the component the API
// authorized. Exactly one of LogNode / LogComponent is set per lane; a nil
// SampleOwner means the batch's samples must not be written (no samples, or an
// orphan task).
type ingestBinding struct {
	LogComponent string
	LogNode      string
	SampleOwner  *storage.TaskOwner
}

// land writes one batch to its sinks: the raw log lines, then the samples routed by
// registry kind. It is the ONE write path, and both ingest lanes call it, so a new
// lane cannot drift from the node lane's semantics (reject-not-project, the
// transition-only state guard).
//
// It deliberately knows nothing about JetStream: no message, no ack, no nak. The
// caller owns delivery semantics and decides what a returned error means, which is
// what lets a synchronous API caller reuse it. A returned error is always transient
// (a registry read or a sink write); anything permanent is dropped in place, since
// re-delivering it would not help.
//
// NOTE(#311): the sinks each write in their own transaction and the caller acks
// once afterward. Under at-least-once redelivery, a failure in a later write after
// an earlier one committed re-runs the committed write on redelivery; metric and
// event have no uniqueness key, so that double-inserts. This is a pre-existing
// multi-tx-then-ack characteristic; atomic-or-idempotent ingest is tracked
// separately.
func (s *Server) land(ctx context.Context, ev *ogv1.TelemetryBatch, bind ingestBinding) error {
	// The raw log sink, routed by lane: a node's self-logs to node_log, a push's
	// component-owned lines to log_line (#589). The storage layer refuses any
	// other binding, so a future lane cannot quietly widen the log owner.
	if bind.LogNode != "" {
		if logs := nodeLogWrites(ev, bind.LogNode); len(logs) > 0 {
			if err := s.store.InsertNodeLogs(ctx, logs); err != nil {
				return err
			}
		}
	} else if logs := logLineWrites(ev, bind.LogComponent); len(logs) > 0 {
		if err := s.store.InsertLogLines(ctx, logs); err != nil {
			return err
		}
	}
	if bind.SampleOwner == nil || len(ev.GetSamples()) == 0 {
		return nil
	}

	metricTypes, err := s.store.ListMetricTypes(ctx)
	if err != nil {
		return err
	}
	properties, err := s.store.ListPropertyTypes(ctx)
	if err != nil {
		return err
	}
	eventTypes, err := s.store.ListEventTypes(ctx)
	if err != nil {
		return err
	}
	reg := collection.NewRegistry(metricTypes, properties, eventTypes)
	// A name in both registries resolves to nothing, so every sample carrying it is
	// refused. The fence at the create routes stops new ones, but an install that
	// already has a collision would otherwise just lose data quietly. Say so.
	if clashes := reg.Collisions(); len(clashes) > 0 {
		slog.Warn("registry name collision: samples using these names are refused until one side is renamed",
			"names", clashes)
	}

	// Route by the registry kind: a metric name lands in metric, a state name in
	// property, a registered event_type name in event as a caught occurrence (the
	// ADR-0066 lanes; raw logs ride their own lane to log_line or node_log). All
	// survive the SAME owner binding and reject-not-project; the split is only the
	// sink, not a second trust decision. The multi-transaction-then-ack
	// redelivery characteristic is NOTE(#311) on land above.
	metrics, states, events := deriveSamples(ev, *bind.SampleOwner, reg)
	if len(metrics) > 0 {
		if err := s.store.InsertMetricSamples(ctx, metrics); err != nil {
			return err
		}
	}
	if len(events) > 0 {
		if err := s.store.InsertEvents(ctx, events); err != nil {
			return err
		}
	}
	// The transition guard: a property series is transition-only, so skip a write whose
	// value equals the latest stored value for that series. A producer's own change
	// detection is the primary defense; this is the robustness net for a restart that
	// re-emits an unchanged verdict. Nothing derives after the write: a sample's
	// current value IS its latest series row, both lanes (#591 retired the cache).
	fresh, err := s.dedupeProperties(ctx, states)
	if err != nil {
		return err
	}
	if len(fresh) > 0 {
		if err := s.store.InsertPropertySamples(ctx, fresh); err != nil {
			return err
		}
	}
	return nil
}

// nakOrTerm redelivers a telemetry message that failed for a transient reason
// (Nak), unless it has already been delivered maxTelemetryDeliveries times (or
// its delivery count can't be read), in which case it is Term'd: deleted from
// the OG_TELEMETRY work queue rather than left to redeliver forever. A message
// that only ever Nak's never leaves a work-queue-retention stream on its own, so
// this is what actually bounds the redelivery loop; ConsumerConfig.MaxDeliver is
// the JetStream-side backstop for the same bound.
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
	_ = msg.Nak()
}

// dedupeProperties drops any property sample whose value equals the latest
// stored value for its series (owner component + key + instance), so a repeated
// identical verdict does not add a consecutive-duplicate row. A LatestProperty
// read error is returned so the caller can leave the message unacked for
// redelivery.
//
// Correctness of this read-then-insert guard depends on the telemetry consumer
// dispatching messages serially (one fully processed before the next is read),
// which the current ConsumerConfig gives us (AckExplicit, no MaxAckPending, no
// per-message goroutine); MaxDeliver bounds how many times a failed message is
// redelivered but does not affect this serial-dispatch property. Adding
// concurrent handlers or batched in-flight acks would make this racy: two
// identical in-flight duplicates could both read an older latest and both
// insert. Keep dispatch serial, or move the transition check into the insert (a
// conditional write) before parallelizing.
func (s *Server) dedupeProperties(ctx context.Context, states []storage.PropertySampleWrite) ([]storage.PropertySampleWrite, error) {
	if len(states) == 0 {
		return nil, nil
	}
	fresh := make([]storage.PropertySampleWrite, 0, len(states))
	for _, ev := range states {
		latest, err := s.store.LatestProperty(ctx, ev.OwnerID, ev.Key, ev.Instance)
		if err != nil {
			return nil, err
		}
		if latest != nil && latest.Value == ev.Value {
			continue // unchanged verdict: transition-only, skip
		}
		fresh = append(fresh, ev)
	}
	return fresh, nil
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

// deriveSamples turns a decoded TelemetryBatch + its resolved owner into the
// typed rows to persist, split by sample kind. Pure: no I/O. reject-not-project
// drops any sample whose name is not a registered property_type or event_type;
// the registry kind then routes a metric to the metric slice, a state to the
// property slice, and an event-type occurrence to the event slice. The owner is stamped identically for all three from the task's
// interface: owner_kind=component, source=interface type, instance=interface name;
// provenance is observed (the insert path fixes that).
func deriveSamples(ev *ogv1.TelemetryBatch, owner storage.TaskOwner, reg collection.Registry) ([]storage.MetricSampleWrite, []storage.PropertySampleWrite, []storage.EventWrite) {
	var metrics []storage.MetricSampleWrite
	var states []storage.PropertySampleWrite
	var events []storage.EventWrite
	// The wire value wins where the batch supplies one, else fall back to what the
	// task's interface implies. The node lane sets neither (its interface name IS the
	// instance discriminator and its interface type IS the source), so it is
	// unchanged; a push has no interface, so it must be able to say both.
	batchSource := ev.GetSource()
	for _, dp := range ev.GetSamples() {
		instance := dp.GetInstance()
		if instance == "" {
			instance = owner.InterfaceName
		}
		source := batchSource
		if source == "" {
			source = owner.InterfaceType
		}
		kind, ok := reg.Allows(dp.GetName())
		if !ok {
			continue // reject-not-project: unregistered name
		}
		switch kind {
		case "metric":
			val, ok := numericValue(dp)
			if !ok {
				continue
			}
			metrics = append(metrics, storage.MetricSampleWrite{
				OwnerKind: "component",
				OwnerID:   owner.Component,
				Key:       dp.GetName(),
				Instance:  instance,
				Value:     val,
				Source:    source,
				TS:        sampleTime(ev, dp),
			})
		case "state":
			val, ok := stringValue(dp)
			if !ok {
				continue
			}
			states = append(states, storage.PropertySampleWrite{
				OwnerKind: "component",
				OwnerID:   owner.Component,
				Key:       dp.GetName(),
				Instance:  instance,
				Value:     val,
				Source:    source,
				TS:        sampleTime(ev, dp),
			})
		case "event":
			msg, attrs, ok := logValue(dp)
			if !ok {
				continue
			}
			// A component published this occurrence natively (an xAPI event, an SNMP
			// trap): it is caught, the platform did not derive it. Raw log lines are a
			// separate ingest lane, not this path (ADR-0066).
			events = append(events, storage.EventWrite{
				OwnerKind:  "component",
				OwnerID:    owner.Component,
				Key:        dp.GetName(),
				Instance:   instance,
				Origin:     "caught",
				Message:    msg,
				Attributes: attrs,
				Source:     source,
				TS:         sampleTime(ev, dp),
			})
		}
	}
	return metrics, states, events
}

// numericValue extracts a metric's float value from the sample's typed oneof.
// A metric rides double_value (or int_value); a string/json/empty value is not a
// metric and yields ok=false (the caller skips it).
func numericValue(dp *ogv1.Sample) (float64, bool) {
	switch v := dp.GetValue().(type) {
	case *ogv1.Sample_DoubleValue:
		return v.DoubleValue, true
	case *ogv1.Sample_IntValue:
		return float64(v.IntValue), true
	default:
		return 0, false
	}
}

// stringValue extracts a state's categorical value from the sample's typed
// oneof. A state rides string_value; a numeric/json/empty value is not a state
// verdict and yields ok=false (the caller skips it).
func stringValue(dp *ogv1.Sample) (string, bool) {
	if v, ok := dp.GetValue().(*ogv1.Sample_StringValue); ok {
		return v.StringValue, true
	}
	return "", false
}

// logValue extracts a log occurrence's payload from the sample's typed oneof.
// A log rides string_value (its message) or json_value (structured attributes); a
// numeric/empty value is not a log and yields ok=false (the caller skips it).
func logValue(dp *ogv1.Sample) (string, []byte, bool) {
	switch v := dp.GetValue().(type) {
	case *ogv1.Sample_StringValue:
		return v.StringValue, nil, true
	case *ogv1.Sample_JsonValue:
		return "", v.JsonValue, true
	default:
		return "", nil, false
	}
}

// sampleTime resolves the timestamp for one sample: its own ts if set, else
// the event batch ts, else zero (the insert path then defaults to now).
func sampleTime(ev *ogv1.TelemetryBatch, dp *ogv1.Sample) time.Time {
	if dp.GetTs() != nil {
		return dp.GetTs().AsTime()
	}
	if ev.GetTs() != nil {
		return ev.GetTs().AsTime()
	}
	return time.Time{}
}
