package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// TestDeriveDatapoints proves the pure ingest derivation: a registered metric
// name is stamped with the task's interface owner (component / source / instance),
// and reject-not-project drops an unregistered name (no row produced for it).
func TestDeriveDatapoints(t *testing.T) {
	metric := "metric"
	reg := collection.NewRegistry([]storage.Property{
		{Name: "tcp.open", Kind: &metric},
		{Name: "tcp.connect_time", Kind: &metric},
	})
	owner := storage.TaskOwner{Component: "disp-1", InterfaceName: "disp-1-tcp", InterfaceType: "tcp"}
	ev := &ogv1.Event{
		TaskId: "t1",
		NodeId: "node-a",
		Datapoints: []*ogv1.Datapoint{
			{Name: "tcp.open", Value: &ogv1.Datapoint_DoubleValue{DoubleValue: 1}},
			{Name: "tcp.connect_time", Value: &ogv1.Datapoint_DoubleValue{DoubleValue: 3.5}},
			{Name: "not.registered", Value: &ogv1.Datapoint_DoubleValue{DoubleValue: 9}},
		},
	}

	metrics, states, events := deriveDatapoints(ev, owner, reg)
	if len(metrics) != 2 || len(states) != 0 || len(events) != 0 {
		t.Fatalf("derived %d metrics %d states %d events, want 2/0/0 (unregistered name dropped): %+v", len(metrics), len(states), len(events), metrics)
	}
	for _, e := range metrics {
		if e.OwnerKind != "component" || e.OwnerID != "disp-1" || e.Source != "tcp" || e.Instance != "disp-1-tcp" {
			t.Fatalf("owner stamping wrong: %+v", e)
		}
		if e.Key == "not.registered" {
			t.Fatal("reject-not-project failed: unregistered name was projected")
		}
	}
}

// TestDeriveDatapointsRoutesByKind: a name registered as state routes to the state
// slice (not metric_datapoint), a name registered as log routes to the event slice,
// each stamped with the same task-interface owner; an unregistered name is still
// dropped (reject-not-project).
func TestDeriveDatapointsRoutesByKind(t *testing.T) {
	metric, state, logKind := "metric", "state", "log"
	reg := collection.NewRegistry([]storage.Property{
		{Name: "tcp.open", Kind: &metric},
		{Name: "interface.reachable", Kind: &state},
		{Name: "some.log", Kind: &logKind},
	})
	owner := storage.TaskOwner{Component: "disp-1", InterfaceName: "disp-1-tcp", InterfaceType: "tcp"}
	ev := &ogv1.Event{Datapoints: []*ogv1.Datapoint{
		{Name: "tcp.open", Value: &ogv1.Datapoint_DoubleValue{DoubleValue: 1}},
		{Name: "interface.reachable", Value: &ogv1.Datapoint_StringValue{StringValue: "up"}},
		{Name: "some.log", Value: &ogv1.Datapoint_StringValue{StringValue: "line"}},
		{Name: "not.registered", Value: &ogv1.Datapoint_StringValue{StringValue: "up"}},
	}}
	metrics, states, events := deriveDatapoints(ev, owner, reg)
	if len(metrics) != 1 || metrics[0].Key != "tcp.open" {
		t.Fatalf("metrics = %+v, want one tcp.open", metrics)
	}
	if len(states) != 1 {
		t.Fatalf("states = %+v, want one interface.reachable (unregistered dropped)", states)
	}
	s := states[0]
	if s.Key != "interface.reachable" || s.Value != "up" || s.OwnerKind != "component" ||
		s.OwnerID != "disp-1" || s.Instance != "disp-1-tcp" || s.Source != "tcp" {
		t.Fatalf("state routing/owner wrong: %+v", s)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v, want one some.log (unregistered dropped)", events)
	}
	e := events[0]
	if e.Key != "some.log" || e.Message != "line" || e.OwnerKind != "component" ||
		e.OwnerID != "disp-1" || e.Instance != "disp-1-tcp" || e.Source != "tcp" {
		t.Fatalf("log routing/owner wrong: %+v", e)
	}
}

// fakeMsg is a minimal jetstream.Msg double so nakOrTerm's decision (redeliver
// vs. drop) can be unit-tested without a real embedded NATS server: only
// Metadata, Nak, and Term are exercised, the rest are unused stubs.
type fakeMsg struct {
	meta    *jetstream.MsgMetadata
	metaErr error
	naked   bool
	termed  bool
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return m.meta, m.metaErr }
func (m *fakeMsg) Data() []byte                              { return nil }
func (m *fakeMsg) Headers() nats.Header                      { return nil }
func (m *fakeMsg) Subject() string                           { return "og.v1.telemetry.node-a" }
func (m *fakeMsg) Reply() string                             { return "" }
func (m *fakeMsg) Ack() error                                { return nil }
func (m *fakeMsg) DoubleAck(context.Context) error           { return nil }
func (m *fakeMsg) Nak() error                                { m.naked = true; return nil }
func (m *fakeMsg) NakWithDelay(time.Duration) error          { m.naked = true; return nil }
func (m *fakeMsg) InProgress() error                         { return nil }
func (m *fakeMsg) Term() error                               { m.termed = true; return nil }
func (m *fakeMsg) TermWithReason(string) error               { m.termed = true; return nil }

// TestNakOrTerm proves the WorkQueue-cleanliness decision: a message under the
// maxTelemetryDeliveries bound is redelivered (Nak), one at or past the bound is
// dropped (Term) rather than redelivered forever, and an unreadable delivery
// count (Metadata error) also drops rather than guessing.
func TestNakOrTerm(t *testing.T) {
	s := &Server{}

	t.Run("below the bound redelivers", func(t *testing.T) {
		m := &fakeMsg{meta: &jetstream.MsgMetadata{NumDelivered: 1}}
		s.nakOrTerm(m)
		if !m.naked || m.termed {
			t.Fatalf("delivery 1 of %d: want Nak only, got naked=%v termed=%v", maxTelemetryDeliveries, m.naked, m.termed)
		}
	})

	t.Run("at the bound drops", func(t *testing.T) {
		m := &fakeMsg{meta: &jetstream.MsgMetadata{NumDelivered: maxTelemetryDeliveries}}
		s.nakOrTerm(m)
		if !m.termed || m.naked {
			t.Fatalf("delivery %d (== bound): want Term only, got naked=%v termed=%v", maxTelemetryDeliveries, m.naked, m.termed)
		}
	})

	t.Run("past the bound drops", func(t *testing.T) {
		m := &fakeMsg{meta: &jetstream.MsgMetadata{NumDelivered: maxTelemetryDeliveries + 3}}
		s.nakOrTerm(m)
		if !m.termed || m.naked {
			t.Fatalf("delivery past bound: want Term only, got naked=%v termed=%v", m.naked, m.termed)
		}
	})

	t.Run("unreadable metadata drops rather than guesses", func(t *testing.T) {
		m := &fakeMsg{metaErr: errors.New("metadata unavailable")}
		s.nakOrTerm(m)
		if !m.termed || m.naked {
			t.Fatalf("metadata error: want Term only, got naked=%v termed=%v", m.naked, m.termed)
		}
	})
}
