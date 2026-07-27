package bus

import (
	"context"
	"encoding/json"
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
// maxTelemetryDeliveries bounds how many times a single Event is redelivered
// before nakOrTerm gives up on it: without a bound, a permanently-failing
// message (a row the handler will never manage to write) would redeliver
// forever, the mirror problem to the stream's own retention (see
// startTelemetryConsumer).
const (
	telemetryStream        = "OG_TELEMETRY"
	telemetryConsumer      = "og-telemetry-worker"
	maxTelemetryDeliveries = 5
)

// startTelemetryConsumer creates (idempotently) the telemetry stream + durable
// consumer over the full-permission internal client and begins consuming. The
// stream persists an Event the instant a node publishes it, and (WorkQueuePolicy)
// deletes it the instant it is acked, so disk stays bounded to the current
// backlog rather than growing forever; the consumer redelivers a transient
// failure up to maxTelemetryDeliveries (nakOrTerm), so a DB hiccup never loses a
// datapoint but a permanently-failing one still leaves the queue.
func (s *Server) startTelemetryConsumer() error {
	js, err := jetstream.New(s.nc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      telemetryStream,
		Subjects:  []string{collection.TelemetryWildcard},
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
// datapoints, and ack. The ack discipline is deliberate: a permanent condition
// (undecodable payload, or an orphan the confinement fence drops) is terminated /
// acked so it is not redelivered; only a transient failure (DB, registry read) is
// left for nakOrTerm, which redelivers (Nak) up to maxTelemetryDeliveries and then
// terminates the message so it does not loop forever.
func (s *Server) handleTelemetry(msg jetstream.Msg) {
	ctx := context.Background()
	// The node published only its own telemetry subject (per-node grant), so the
	// node extracted from the subject is trusted, the same trust the heartbeat sink
	// relies on.
	node := collection.NodeFromSubject(msg.Subject())

	var ev ogv1.Event
	if err := proto.Unmarshal(msg.Data(), &ev); err != nil {
		_ = msg.Term() // undecodable: it will never succeed, stop redelivery
		return
	}

	// Owner + confinement: the owner is the task's interface component, and the
	// task must belong to THIS node. A task on another node, an unknown task, or a
	// shared interface resolves to !ok: the datapoint is an orphan, dropped (acked
	// so it is not redelivered), never written for a component the node was not
	// placed on.
	owner, ok, err := s.store.ResolveTaskOwner(ctx, ev.GetTaskId(), node)
	if err != nil {
		s.nakOrTerm(msg) // transient DB failure: redeliver (bounded)
		return
	}
	if !ok {
		_ = msg.Ack() // orphan: drop
		return
	}

	properties, err := s.store.ListPropertyTypes(ctx)
	if err != nil {
		s.nakOrTerm(msg) // transient registry read failure: redeliver (bounded)
		return
	}
	eventTypes, err := s.store.ListEventTypes(ctx)
	if err != nil {
		s.nakOrTerm(msg) // transient registry read failure: redeliver (bounded)
		return
	}
	reg := collection.NewRegistry(properties, eventTypes)

	// Route by the registry kind (the cp3-deferred "route by kind" note): a metric
	// name lands in metric, a state name in state, a log name in
	// event. All survive the SAME owner-confinement and reject-not-project above; the
	// split is only the sink, not a second trust decision.
	//
	// NOTE(#311): the three sinks each write in their own transaction, then the
	// message is acked once. Under at-least-once redelivery, a failure in a later
	// write after an earlier one committed re-runs the committed write on redelivery;
	// metric and event have no uniqueness key, so that double-inserts. This
	// is a pre-existing multi-tx-then-ack characteristic (adding the event write only
	// widens the window); atomic-or-idempotent ingest is tracked separately.
	metrics, states, events := deriveDatapoints(&ev, owner, reg)
	if len(metrics) > 0 {
		if err := s.store.InsertMetricSamples(ctx, metrics); err != nil {
			s.nakOrTerm(msg) // DB write failed: redeliver (bounded)
			return
		}
	}
	if len(events) > 0 {
		if err := s.store.InsertEvents(ctx, events); err != nil {
			s.nakOrTerm(msg) // DB write failed: redeliver (bounded)
			return
		}
	}
	// The ingest-side transition guard: a state series is transition-only, so skip a
	// write whose value equals the latest stored value for that series. The node's
	// own change detection is the primary defense; this is the robustness net for a
	// node restart that re-emits an unchanged verdict.
	fresh, err := s.dedupeStates(ctx, states)
	if err != nil {
		s.nakOrTerm(msg) // latest-state read failed: redeliver (bounded)
		return
	}
	if len(fresh) > 0 {
		if err := s.store.InsertStateSamples(ctx, fresh); err != nil {
			s.nakOrTerm(msg) // DB write failed: redeliver (bounded)
			return
		}
	}
	// Derive the observed latest-value cache from the samples that just landed
	// (ADR-0063 #394). This is non-gating: the append tables are the source of
	// truth and the cache is rebuildable from them, so a failed upsert is logged,
	// not retried, and never fails the ack. Its series-key upsert is idempotent, so
	// a redelivery that re-runs it does not double-write (unlike the append sinks).
	if ups := latestUpserts(metrics, fresh); len(ups) > 0 {
		if err := s.store.UpsertProperties(ctx, ups); err != nil {
			slog.Warn("latest-value derive failed (rebuildable from samples)", "subject", msg.Subject(), "error", err)
		}
	}
	_ = msg.Ack()
}

// latestUpserts turns the observed samples that just landed (metrics and the
// post-dedupe states) into producer-cache upserts: one newest value per series,
// provenance observed, the metric's number and the state's string encoded as the
// jsonb value the cache stores. Pure: no I/O. Events are occurrences, not values,
// so they never enter the value cache.
func latestUpserts(metrics []storage.MetricSampleEvent, states []storage.StateSampleEvent) []storage.PropertyUpsert {
	ups := make([]storage.PropertyUpsert, 0, len(metrics)+len(states))
	for _, m := range metrics {
		v, err := json.Marshal(m.Value)
		if err != nil {
			continue
		}
		ups = append(ups, storage.PropertyUpsert{
			OwnerKind: m.OwnerKind, OwnerID: m.OwnerID, Key: m.Key, Instance: m.Instance,
			Provenance: "observed", Value: v, TS: m.TS,
		})
	}
	for _, st := range states {
		v, err := json.Marshal(st.Value)
		if err != nil {
			continue
		}
		ups = append(ups, storage.PropertyUpsert{
			OwnerKind: st.OwnerKind, OwnerID: st.OwnerID, Key: st.Key, Instance: st.Instance,
			Provenance: "observed", Value: v, TS: st.TS,
		})
	}
	return ups
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

// dedupeStates drops any state event whose value equals the latest stored value
// for its series (owner component + key + instance), so a repeated identical
// verdict does not add a consecutive-duplicate row. A LatestState read error is
// returned so the caller can leave the message unacked for redelivery.
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
func (s *Server) dedupeStates(ctx context.Context, states []storage.StateSampleEvent) ([]storage.StateSampleEvent, error) {
	if len(states) == 0 {
		return nil, nil
	}
	fresh := make([]storage.StateSampleEvent, 0, len(states))
	for _, ev := range states {
		latest, err := s.store.LatestState(ctx, ev.OwnerID, ev.Key, ev.Instance)
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

// deriveDatapoints turns a decoded Event + its resolved owner into the typed rows
// to persist, split by datapoint kind. Pure: no I/O. reject-not-project drops any
// datapoint whose name is not a registered datapoint_type; the registry kind then
// routes a metric to the metric slice, a state to the state slice, and a log to the
// event slice. The owner is stamped identically for all three from the task's
// interface: owner_kind=component, source=interface type, instance=interface name;
// provenance is observed (the insert path fixes that).
func deriveDatapoints(ev *ogv1.Event, owner storage.TaskOwner, reg collection.Registry) ([]storage.MetricSampleEvent, []storage.StateSampleEvent, []storage.EventOccurrence) {
	var metrics []storage.MetricSampleEvent
	var states []storage.StateSampleEvent
	var events []storage.EventOccurrence
	for _, dp := range ev.GetSamples() {
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
			metrics = append(metrics, storage.MetricSampleEvent{
				OwnerKind: "component",
				OwnerID:   owner.Component,
				Key:       dp.GetName(),
				Instance:  owner.InterfaceName,
				Value:     val,
				Source:    owner.InterfaceType,
				TS:        datapointTime(ev, dp),
			})
		case "state":
			val, ok := stringValue(dp)
			if !ok {
				continue
			}
			states = append(states, storage.StateSampleEvent{
				OwnerKind: "component",
				OwnerID:   owner.Component,
				Key:       dp.GetName(),
				Instance:  owner.InterfaceName,
				Value:     val,
				Source:    owner.InterfaceType,
				TS:        datapointTime(ev, dp),
			})
		case "event":
			msg, attrs, ok := logValue(dp)
			if !ok {
				continue
			}
			// A component published this occurrence natively (an xAPI event, an SNMP
			// trap): it is caught, the platform did not derive it. Raw log lines are a
			// separate ingest lane, not this path (ADR-0066).
			events = append(events, storage.EventOccurrence{
				OwnerKind:  "component",
				OwnerID:    owner.Component,
				Key:        dp.GetName(),
				Instance:   owner.InterfaceName,
				Origin:     "caught",
				Message:    msg,
				Attributes: attrs,
				Source:     owner.InterfaceType,
				TS:         datapointTime(ev, dp),
			})
		}
	}
	return metrics, states, events
}

// numericValue extracts a metric's float value from the datapoint's typed oneof.
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

// stringValue extracts a state's categorical value from the datapoint's typed
// oneof. A state rides string_value; a numeric/json/empty value is not a state
// verdict and yields ok=false (the caller skips it).
func stringValue(dp *ogv1.Sample) (string, bool) {
	if v, ok := dp.GetValue().(*ogv1.Sample_StringValue); ok {
		return v.StringValue, true
	}
	return "", false
}

// logValue extracts a log occurrence's payload from the datapoint's typed oneof.
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

// datapointTime resolves the timestamp for one datapoint: its own ts if set, else
// the event batch ts, else zero (the insert path then defaults to now).
func datapointTime(ev *ogv1.Event, dp *ogv1.Sample) time.Time {
	if dp.GetTs() != nil {
		return dp.GetTs().AsTime()
	}
	if ev.GetTs() != nil {
		return ev.GetTs().AsTime()
	}
	return time.Time{}
}
