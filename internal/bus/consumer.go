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

	keys, err := s.store.ListKeys(ctx)
	if err != nil {
		s.nakOrTerm(msg) // transient registry read failure: redeliver (bounded)
		return
	}
	reg := collection.NewRegistry(keys)

	// Route by the registry kind (the cp3-deferred "route by kind" note): a metric
	// name lands in metric_datapoint, a state name in state_datapoint. Both survive
	// the SAME owner-confinement and reject-not-project above; the split is only the
	// sink, not a second trust decision.
	metrics, states := deriveDatapoints(&ev, owner, reg)
	if len(metrics) > 0 {
		if err := s.store.InsertMetricDatapoints(ctx, metrics); err != nil {
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
		if err := s.store.InsertStateDatapoints(ctx, fresh); err != nil {
			s.nakOrTerm(msg) // DB write failed: redeliver (bounded)
			return
		}
	}
	_ = msg.Ack()
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
func (s *Server) dedupeStates(ctx context.Context, states []storage.StateDatapointEvent) ([]storage.StateDatapointEvent, error) {
	if len(states) == 0 {
		return nil, nil
	}
	fresh := make([]storage.StateDatapointEvent, 0, len(states))
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
// routes a metric to the metric slice and a state to the state slice (a log kind
// has no sink this checkpoint, dropped). The owner is stamped identically for both
// from the task's interface: owner_kind=component, source=interface type,
// instance=interface name; provenance is observed (the insert path fixes that).
func deriveDatapoints(ev *ogv1.Event, owner storage.TaskOwner, reg collection.Registry) ([]storage.MetricDatapointEvent, []storage.StateDatapointEvent) {
	var metrics []storage.MetricDatapointEvent
	var states []storage.StateDatapointEvent
	for _, dp := range ev.GetDatapoints() {
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
			metrics = append(metrics, storage.MetricDatapointEvent{
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
			states = append(states, storage.StateDatapointEvent{
				OwnerKind: "component",
				OwnerID:   owner.Component,
				Key:       dp.GetName(),
				Instance:  owner.InterfaceName,
				Value:     val,
				Source:    owner.InterfaceType,
				TS:        datapointTime(ev, dp),
			})
		}
	}
	return metrics, states
}

// numericValue extracts a metric's float value from the datapoint's typed oneof.
// A metric rides double_value (or int_value); a string/json/empty value is not a
// metric and yields ok=false (the caller skips it).
func numericValue(dp *ogv1.Datapoint) (float64, bool) {
	switch v := dp.GetValue().(type) {
	case *ogv1.Datapoint_DoubleValue:
		return v.DoubleValue, true
	case *ogv1.Datapoint_IntValue:
		return float64(v.IntValue), true
	default:
		return 0, false
	}
}

// stringValue extracts a state's categorical value from the datapoint's typed
// oneof. A state rides string_value; a numeric/json/empty value is not a state
// verdict and yields ok=false (the caller skips it).
func stringValue(dp *ogv1.Datapoint) (string, bool) {
	if v, ok := dp.GetValue().(*ogv1.Datapoint_StringValue); ok {
		return v.StringValue, true
	}
	return "", false
}

// datapointTime resolves the timestamp for one datapoint: its own ts if set, else
// the event batch ts, else zero (the insert path then defaults to now).
func datapointTime(ev *ogv1.Event, dp *ogv1.Datapoint) time.Time {
	if dp.GetTs() != nil {
		return dp.GetTs().AsTime()
	}
	if ev.GetTs() != nil {
		return ev.GetTs().AsTime()
	}
	return time.Time{}
}
