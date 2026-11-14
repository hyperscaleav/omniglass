package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestDeriveMetrics proves the pure metric-lane derivation: a registered metric
// name is stamped with the task's interface owner (component / source / instance),
// and reject-not-project drops a name unknown to the metric catalog, INCLUDING a
// name that is registered on another lane (the lane is the array, #594).
func TestDeriveMetrics(t *testing.T) {
	reg := collection.NewRegistry([]storage.MetricType{
		{Name: "icmp-rtt-avg"},
		{Name: "tcp-connect-time"},
	}, []storage.PropertyType{{Name: "video-input", DataType: "string"}}, nil)
	owner := storage.TaskOwner{ComponentID: "disp-1", EndpointName: "disp-1-tcp", Transport: "tcp"}
	ev := &ogv1.TelemetryBatch{
		TaskId: "t1",
		NodeId: "node-a",
		Metrics: []*ogv1.MetricSample{
			{Name: "icmp-rtt-avg", Value: 1},
			{Name: "tcp-connect-time", Value: 3.5},
			{Name: "not.registered", Value: 9},
			{Name: "video-input", Value: 2}, // a property name on the metric lane: refused by name
		},
	}

	metrics := deriveMetrics(ev, componentArc(owner), owner, reg, storage.NodeWriter(owner.ComponentID, owner.EndpointName), nil)
	if len(metrics) != 2 {
		t.Fatalf("derived %d metrics, want 2 (unregistered and cross-lane names dropped): %+v", len(metrics), metrics)
	}
	for _, e := range metrics {
		if e.OwnerKind != "component" || e.OwnerID != "disp-1" || e.Source != "tcp" || e.Instance != "disp-1-tcp" {
			t.Fatalf("owner stamping wrong: %+v", e)
		}
		if e.Key == "not.registered" || e.Key == "video-input" {
			t.Fatalf("reject-not-project failed: %q was projected onto the metric lane", e.Key)
		}
	}
}

// TestDeriveProperties: the property lane carries canonical JSON text; the value
// must be valid JSON of the type's data_type, and when the property_type carries
// a validation schema the value must satisfy it. A violating or malformed value
// is dropped (reject-not-project has no reply channel on the node lane); a valid
// one lands with the same owner stamping as a metric.
func TestDeriveProperties(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{{Name: "tcp-connect-time"}},
		[]storage.PropertyType{
			{Name: "endpoint-reachable", DataType: "string", Validation: []byte(`{"enum":["up","down"]}`)},
			{Name: "serial-number", DataType: "string"},
		}, nil)
	owner := storage.TaskOwner{ComponentID: "disp-1", EndpointName: "disp-1-tcp", Transport: "tcp"}
	ev := &ogv1.TelemetryBatch{Properties: []*ogv1.PropertySample{
		{Name: "endpoint-reachable", ValueJson: `"up"`},
		{Name: "endpoint-reachable", ValueJson: `"sideways"`}, // violates the enum: dropped
		{Name: "endpoint-reachable", ValueJson: `not json`},   // malformed: dropped
		{Name: "endpoint-reachable", ValueJson: `3`},          // not a string: dropped
		{Name: "serial-number", ValueJson: `"SN-1"`},          // no schema: any string lands
		{Name: "not.registered", ValueJson: `"up"`},
		{Name: "tcp-connect-time", ValueJson: `"3"`}, // a metric name on the property lane: refused by name
	}}
	states := deriveProperties(ev, componentArc(owner), owner, reg, storage.NodeWriter(owner.ComponentID, owner.EndpointName), nil)
	if len(states) != 2 {
		t.Fatalf("states = %+v, want exactly the valid up and SN-1", states)
	}
	s := states[0]
	if s.Key != "endpoint-reachable" || s.Value != "up" || s.OwnerKind != "component" ||
		s.OwnerID != "disp-1" || s.Instance != "disp-1-tcp" || s.Source != "tcp" {
		t.Fatalf("property owner stamping wrong: %+v", s)
	}
	if states[1].Key != "serial-number" || states[1].Value != "SN-1" {
		t.Fatalf("schemaless property = %+v, want serial-number SN-1", states[1])
	}
}

// TestDeriveEvents: the event lane carries a registered event_type occurrence
// (origin caught). A payload must be valid JSON and, when the event_type carries
// a payload_schema, satisfy it; an occurrence with neither message nor payload
// says nothing and is dropped, as the old oneof wire dropped a valueless sample.
func TestDeriveEvents(t *testing.T) {
	// The schema declares its properties: Huma's validator enforces required and
	// type only over declared property names.
	reg := collection.NewRegistry(nil, nil,
		[]storage.EventType{
			{Name: "call-started", PayloadSchema: []byte(`{"type":"object","properties":{"participants":{"type":"integer"}},"required":["participants"]}`)},
			{Name: "note"},
		})
	owner := storage.TaskOwner{ComponentID: "disp-1", EndpointName: "disp-1-tcp", Transport: "tcp"}
	ev := &ogv1.TelemetryBatch{Events: []*ogv1.EventSample{
		{Name: "call-started", Message: "call started", Payload: []byte(`{"participants":4}`)},
		{Name: "call-started", Payload: []byte(`{"other":1}`)}, // schema violation: dropped
		{Name: "call-started", Payload: []byte(`not json`)},    // malformed payload: dropped
		{Name: "note", Message: "line"},                        // message-only, schemaless: lands
		{Name: "note"},                                         // neither message nor payload: dropped
		{Name: "not.registered", Message: "x"},
	}}
	events := deriveEvents(ev, componentArc(owner), owner, reg, storage.NodeWriter(owner.ComponentID, owner.EndpointName), nil)
	if len(events) != 2 {
		t.Fatalf("events = %+v, want exactly the valid call-started and note", events)
	}
	e := events[0]
	if e.Key != "call-started" || e.Message != "call started" || string(e.Attributes) != `{"participants":4}` ||
		e.Origin != "caught" || e.OwnerKind != "component" || e.OwnerID != "disp-1" ||
		e.Instance != "disp-1-tcp" || e.Source != "tcp" {
		t.Fatalf("event routing/owner wrong: %+v", e)
	}
	if events[1].Key != "note" || events[1].Message != "line" {
		t.Fatalf("message-only event = %+v, want note/line", events[1])
	}
}

// TestSampleTimeRule pins the ts contract of #594 on every lane: a supplied
// per-sample ts survives verbatim, the batch ts fills an absent per-sample ts,
// and when both are absent the zero time is passed so the insert path stamps
// ingest time.
func TestSampleTimeRule(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{{Name: "tcp-connect-time"}},
		[]storage.PropertyType{{Name: "serial-number", DataType: "string"}},
		[]storage.EventType{{Name: "note"}})
	owner := storage.TaskOwner{ComponentID: "disp-1", EndpointName: "tcp"}
	sampleTS := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	batchTS := time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC)

	ev := &ogv1.TelemetryBatch{
		Ts: timestamppb.New(batchTS),
		Metrics: []*ogv1.MetricSample{
			{Name: "tcp-connect-time", Value: 1, Ts: timestamppb.New(sampleTS)},
			{Name: "tcp-connect-time", Value: 1},
		},
		Properties: []*ogv1.PropertySample{
			{Name: "serial-number", ValueJson: `"SN-1"`, Ts: timestamppb.New(sampleTS)},
			{Name: "serial-number", ValueJson: `"SN-1"`},
		},
		Events: []*ogv1.EventSample{
			{Name: "note", Message: "x", Ts: timestamppb.New(sampleTS)},
			{Name: "note", Message: "x"},
		},
	}
	metrics := deriveMetrics(ev, componentArc(owner), owner, reg, storage.NodeWriter(owner.ComponentID, owner.EndpointName), nil)
	states := deriveProperties(ev, componentArc(owner), owner, reg, storage.NodeWriter(owner.ComponentID, owner.EndpointName), nil)
	events := deriveEvents(ev, componentArc(owner), owner, reg, storage.NodeWriter(owner.ComponentID, owner.EndpointName), nil)
	if len(metrics) != 2 || len(states) != 2 || len(events) != 2 {
		t.Fatalf("derived %d/%d/%d, want 2 per lane", len(metrics), len(states), len(events))
	}
	for lane, tss := range map[string][2]time.Time{
		"metric":   {metrics[0].TS, metrics[1].TS},
		"property": {states[0].TS, states[1].TS},
		"event":    {events[0].TS, events[1].TS},
	} {
		if !tss[0].Equal(sampleTS) {
			t.Errorf("%s: supplied per-sample ts = %v, want it verbatim (%v)", lane, tss[0], sampleTS)
		}
		if !tss[1].Equal(batchTS) {
			t.Errorf("%s: absent per-sample ts = %v, want the batch ts (%v)", lane, tss[1], batchTS)
		}
	}

	// Both absent: zero, so the insert path stamps ingest time.
	bare := &ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "tcp-connect-time", Value: 1}}}
	if got := deriveMetrics(bare, componentArc(owner), owner, reg, storage.NodeWriter(owner.ComponentID, owner.EndpointName), nil); len(got) != 1 || !got[0].TS.IsZero() {
		t.Fatalf("no ts anywhere: want the zero time for the insert path, got %+v", got)
	}
}

// fakeMsg is a minimal jetstream.Msg double so nakOrTerm's decision (redeliver
// vs. drop) can be unit-tested without a real embedded NATS server, and so a
// test can hand handleTelemetry a batch (data and its Msg-Id header). The
// rest are unused stubs.
type fakeMsg struct {
	meta    *jetstream.MsgMetadata
	metaErr error
	data    []byte
	header  nats.Header
	naked   bool
	termed  bool
	ackErr  error
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return m.meta, m.metaErr }
func (m *fakeMsg) Data() []byte                              { return m.data }
func (m *fakeMsg) Headers() nats.Header                      { return m.header }
func (m *fakeMsg) Subject() string                           { return "og.v1.telemetry.node-a" }
func (m *fakeMsg) Reply() string                             { return "" }
func (m *fakeMsg) Ack() error                                { return nil }
func (m *fakeMsg) DoubleAck(context.Context) error           { return m.ackErr }
func (m *fakeMsg) Nak() error                                { m.naked = true; return nil }
func (m *fakeMsg) NakWithDelay(time.Duration) error          { m.naked = true; return nil }
func (m *fakeMsg) InProgress() error                         { return nil }
func (m *fakeMsg) Term() error                               { m.termed = true; return nil }
func (m *fakeMsg) TermWithReason(string) error               { m.termed = true; return nil }

// TestNakOrTerm proves the WorkQueue-cleanliness decision: a message under the
// maxTelemetryDeliveries bound is redelivered (Nak), one at or past the bound is
// dropped (Term) rather than redelivered forever, and an unreadable delivery
// count (Metadata error) also drops rather than guessing. A redelivery counts
// nothing yet; a drop counts its items as lost at the server's hop (#1659).
func TestNakOrTerm(t *testing.T) {
	items := map[delivery.Kind]int{delivery.Metric: 2, delivery.Log: 1}
	t.Run("below the bound redelivers", func(t *testing.T) {
		s := &Server{}
		m := &fakeMsg{meta: &jetstream.MsgMetadata{NumDelivered: 1}}
		s.nakOrTerm(m, termed{}, items)
		if !m.naked || m.termed {
			t.Fatalf("delivery 1 of %d: want Nak only, got naked=%v termed=%v", maxTelemetryDeliveries, m.naked, m.termed)
		}
		for _, rd := range s.flow.Take().Readings() {
			if rd.Value != 0 {
				t.Fatalf("%s[%s] = %v for a batch held for redelivery, want it counted once it settles", rd.Key, rd.Instance, rd.Value)
			}
		}
	})

	for _, tc := range []struct {
		name string
		msg  *fakeMsg
	}{
		{"at the bound drops", &fakeMsg{meta: &jetstream.MsgMetadata{NumDelivered: maxTelemetryDeliveries}}},
		{"past the bound drops", &fakeMsg{meta: &jetstream.MsgMetadata{NumDelivered: maxTelemetryDeliveries + 3}}},
		{"unreadable metadata drops rather than guesses", &fakeMsg{metaErr: errors.New("metadata unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			s.nakOrTerm(tc.msg, termed{}, items)
			if !tc.msg.termed || tc.msg.naked {
				t.Fatalf("want Term only, got naked=%v termed=%v", tc.msg.naked, tc.msg.termed)
			}
			for _, rd := range s.flow.Take().Readings() {
				want, ok := map[string]float64{
					writerclass.DeliveryInCount + "/metric": 2, writerclass.DeliveryOutCount + "/metric": 0, writerclass.DeliveryLossRatio + "/metric": 1,
					writerclass.DeliveryInCount + "/log": 1, writerclass.DeliveryOutCount + "/log": 0, writerclass.DeliveryLossRatio + "/log": 1,
				}[rd.Key+"/"+rd.Instance]
				if !ok {
					want = 0
				}
				if rd.Value != want {
					t.Errorf("%s[%s] = %v, want %v", rd.Key, rd.Instance, rd.Value, want)
				}
			}
		})
	}
}

// The encoding the storage reads depend on (#1202): a node-lane property
// value lands as its text form, a bool as true/false and a number as its
// digits, which the gateway reads back typed by the property's data_type.
func TestDerivePropertiesStoresTheTextForm(t *testing.T) {
	reg := collection.NewRegistry(nil,
		[]storage.PropertyType{
			{Name: "panel-lit", DataType: "bool"},
			{Name: "seat-count", DataType: "json"},
		}, nil)
	owner := storage.TaskOwner{ComponentID: "disp-1", EndpointName: "disp-1-tcp", Transport: "tcp"}
	ev := &ogv1.TelemetryBatch{Properties: []*ogv1.PropertySample{
		{Name: "panel-lit", ValueJson: `true`},
		{Name: "seat-count", ValueJson: `3`},
	}}
	got := deriveProperties(ev, componentArc(owner), owner, reg, storage.NodeWriter(owner.ComponentID, owner.EndpointName), nil)
	if len(got) != 2 || got[0].Value != "true" || got[1].Value != "3" {
		t.Fatalf("derived %+v, want the text forms true and 3", got)
	}
}
