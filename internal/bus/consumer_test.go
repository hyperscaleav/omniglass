package bus

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// TestDeriveDatapoints proves the pure ingest derivation: a registered metric
// name is stamped with the task's interface owner (component / source / instance),
// and reject-not-project drops an unregistered name (no row produced for it).
func TestDeriveDatapoints(t *testing.T) {
	reg := collection.NewRegistry([]storage.DatapointType{
		{Name: "tcp.open", Kind: "metric"},
		{Name: "tcp.connect_time", Kind: "metric"},
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

	evs := deriveDatapoints(ev, owner, reg)
	if len(evs) != 2 {
		t.Fatalf("derived %d rows, want 2 (unregistered name dropped): %+v", len(evs), evs)
	}
	for _, e := range evs {
		if e.OwnerKind != "component" || e.OwnerID != "disp-1" || e.Source != "tcp" || e.Instance != "disp-1-tcp" {
			t.Fatalf("owner stamping wrong: %+v", e)
		}
		if e.Key == "not.registered" {
			t.Fatal("reject-not-project failed: unregistered name was projected")
		}
	}
}

// TestDeriveDatapointsRejectsNonMetricKind: a name registered as state/log has no
// metric sink in this checkpoint, so it is dropped rather than written.
func TestDeriveDatapointsRejectsNonMetricKind(t *testing.T) {
	reg := collection.NewRegistry([]storage.DatapointType{{Name: "some.state", Kind: "state"}})
	owner := storage.TaskOwner{Component: "disp-1", InterfaceName: "i", InterfaceType: "tcp"}
	ev := &ogv1.Event{Datapoints: []*ogv1.Datapoint{
		{Name: "some.state", Value: &ogv1.Datapoint_StringValue{StringValue: "up"}},
	}}
	if evs := deriveDatapoints(ev, owner, reg); len(evs) != 0 {
		t.Fatalf("non-metric kind should not project to metric_datapoint, got %+v", evs)
	}
}
