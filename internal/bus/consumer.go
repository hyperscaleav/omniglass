package bus

import (
	"context"
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
const (
	telemetryStream   = "OG_TELEMETRY"
	telemetryConsumer = "og-telemetry-worker"
)

// startTelemetryConsumer creates (idempotently) the telemetry stream + durable
// consumer over the full-permission internal client and begins consuming. The
// stream persists an Event the instant a node publishes it; the consumer redelivers
// until the handler acks, so a transient DB error never loses a datapoint.
func (s *Server) startTelemetryConsumer() error {
	js, err := jetstream.New(s.nc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     telemetryStream,
		Subjects: []string{collection.TelemetryWildcard},
	})
	if err != nil {
		return err
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:   telemetryConsumer,
		AckPolicy: jetstream.AckExplicitPolicy,
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
// left unacked (Nak) so JetStream redelivers.
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
		_ = msg.Nak() // transient DB failure: redeliver
		return
	}
	if !ok {
		_ = msg.Ack() // orphan: drop
		return
	}

	types, err := s.store.ListDatapointTypes(ctx)
	if err != nil {
		_ = msg.Nak()
		return
	}
	reg := collection.NewRegistry(types)

	evs := deriveDatapoints(&ev, owner, reg)
	if len(evs) > 0 {
		if err := s.store.InsertMetricDatapoints(ctx, evs); err != nil {
			_ = msg.Nak() // DB write failed: redeliver
			return
		}
	}
	_ = msg.Ack()
}

// deriveDatapoints turns a decoded Event + its resolved owner into the metric
// rows to persist. Pure: no I/O. reject-not-project drops any datapoint whose
// name is not a registered metric datapoint_type (an unregistered name, or a
// state/log kind this checkpoint has no sink for). The owner is stamped from the
// task's interface: owner_kind=component, source=interface type, instance=
// interface name; provenance is observed (the insert path fixes that).
func deriveDatapoints(ev *ogv1.Event, owner storage.TaskOwner, reg collection.Registry) []storage.MetricDatapointEvent {
	var out []storage.MetricDatapointEvent
	for _, dp := range ev.GetDatapoints() {
		kind, ok := reg.Allows(dp.GetName())
		if !ok || kind != "metric" {
			continue
		}
		val, ok := numericValue(dp)
		if !ok {
			continue
		}
		out = append(out, storage.MetricDatapointEvent{
			OwnerKind: "component",
			OwnerID:   owner.Component,
			Key:       dp.GetName(),
			Instance:  owner.InterfaceName,
			Value:     val,
			Source:    owner.InterfaceType,
			TS:        datapointTime(ev, dp),
		})
	}
	return out
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
