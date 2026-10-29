package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func fanRegistry() collection.Registry {
	return collection.NewRegistry(
		[]storage.MetricType{{Name: "participant-count"}, {Name: "tcp-open"}},
		[]storage.PropertyType{{Name: "call-state", DataType: "string"}, {Name: "ucc-room-id", DataType: "string"}},
		[]storage.EventType{{Name: "call-started"}})
}

func room(id string) *ogv1.Identity { return &ogv1.Identity{Key: "ucc-room-id", Value: id} }

// TestSplitIdentified: a batch's identified samples are taken out for the
// fan-out, and what stays is exactly the conduit's own samples, untouched.
func TestSplitIdentified(t *testing.T) {
	ev := &ogv1.TelemetryBatch{
		Source:     "webhook",
		Metrics:    []*ogv1.MetricSample{{Name: "participant-count", Value: 3, Identity: room("zr-a")}, {Name: "tcp-open", Value: 1}},
		Properties: []*ogv1.PropertySample{{Name: "call-state", ValueJson: `"active"`, Identity: room("zr-a")}},
		Events:     []*ogv1.EventSample{{Name: "call-started", Message: "m", Identity: room("zr-b")}},
		Logs:       []*ogv1.LogLine{{Message: "raw"}},
	}
	own, fanned := splitIdentified(ev)
	if len(own.GetMetrics()) != 1 || own.GetMetrics()[0].GetName() != "tcp-open" || len(own.GetProperties()) != 0 || len(own.GetEvents()) != 0 || len(own.GetLogs()) != 1 {
		t.Fatalf("own = %v, want the unidentified metric and the log line", own)
	}
	if fanned == nil || len(fanned.GetMetrics()) != 1 || len(fanned.GetProperties()) != 1 || len(fanned.GetEvents()) != 1 {
		t.Fatalf("fanned = %v, want every identified sample", fanned)
	}
	if o, f := splitIdentified(&ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "tcp-open"}}}); f != nil || len(o.GetMetrics()) != 1 {
		t.Fatal("a batch naming no identity split off a fan-out")
	}
}

// TestPlanFanOut: each identified sample lands on the owner its identity's
// binding names, observed at the empty instance whatever the wire said,
// naming its conduit and endpoint in source, admitted only for a collected
// key; an identity no binding places lands on no owner and is aggregated for
// the conduit's inbox, dated by the server's clock and naming only catalog
// keys; an identity that is no valid identity is dropped
// whole, never recorded.
func TestPlanFanOut(t *testing.T) {
	t0 := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	fanned := &ogv1.TelemetryBatch{
		Metrics: []*ogv1.MetricSample{
			{Name: "participant-count", Value: 3, Instance: "forged", Identity: room("zr-a")},
			{Name: "tcp-open", Value: 1, Identity: room("zr-a")}, // a node-probe key: refused
			{Name: "participant-count", Value: 2, Identity: room("zr-x"), Ts: timestamppb.New(t0)},
			{Name: "bad\x00name", Value: 2, Identity: room("zr-x")}, // no catalog key: counted, never named
		},
		Properties: []*ogv1.PropertySample{
			{Name: "call-state", ValueJson: `"active"`, Identity: room("zr-x"), Ts: timestamppb.New(t0.Add(time.Minute))},
			{Name: "call-state", ValueJson: `"idle"`, Identity: &ogv1.Identity{Key: "ucc-room-id", Value: "bad\x00id"}},
		},
		Events: []*ogv1.EventSample{{Name: "call-started", Message: "started", Identity: room("zr-a")}},
	}
	owners := map[storage.IdentityKey]storage.IdentityOwner{{Key: "ucc-room-id", Value: "zr-a"}: {Kind: "system", ID: "sys-a"}}
	p := planFanOut(fanned, "conduit-1", "meetings-cloud", "webhook", owners, fanRegistry(), t0.Add(time.Hour))

	if len(p.rows.Metrics) != 1 || len(p.rows.Events) != 1 || len(p.rows.Properties) != 0 {
		t.Fatalf("rows = %+v, want zr-a's metric and event only", p.rows)
	}
	m := p.rows.Metrics[0]
	if m.OwnerKind != "system" || m.OwnerID != "sys-a" || m.Instance != "" || m.Source != "meetings-cloud/webhook" || m.Value != 3 {
		t.Errorf("fanned metric = %+v, want on sys-a, empty instance, source meetings-cloud/webhook", m)
	}
	if e := p.rows.Events[0]; e.OwnerID != "sys-a" || e.Instance != "" || e.Source != "meetings-cloud/webhook" {
		t.Errorf("fanned event = %+v", e)
	}
	if p.rows.Writer.Component != "conduit-1" {
		t.Errorf("writer = %+v, want the fan-out writer bound to the conduit", p.rows.Writer)
	}
	if len(p.unbound) != 1 {
		t.Fatalf("unbound = %+v, want zr-x alone (the invalid identity is not recorded)", p.unbound)
	}
	u := p.unbound[0]
	if u.ConduitID != "conduit-1" || u.Key != "ucc-room-id" || u.Value != "zr-x" || u.Count != 3 ||
		!u.FirstAt.Equal(t0.Add(time.Hour)) || !u.LastAt.Equal(t0.Add(time.Hour)) || strings.Join(u.Keys, ",") != "call-state,participant-count" {
		t.Errorf("unbound = %+v", u)
	}
	if p.invalid != 1 {
		t.Errorf("invalid = %d, want the one sample whose identity carries a NUL", p.invalid)
	}
}
