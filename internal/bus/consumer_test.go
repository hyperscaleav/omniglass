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

	metrics, states := deriveDatapoints(ev, owner, reg)
	if len(metrics) != 2 || len(states) != 0 {
		t.Fatalf("derived %d metrics %d states, want 2/0 (unregistered name dropped): %+v", len(metrics), len(states), metrics)
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
// slice (not metric_datapoint), stamped with the same task-interface owner; an
// unregistered name is still dropped (reject-not-project), and a log kind (no sink
// this checkpoint) lands in neither.
func TestDeriveDatapointsRoutesByKind(t *testing.T) {
	reg := collection.NewRegistry([]storage.DatapointType{
		{Name: "tcp.open", Kind: "metric"},
		{Name: "interface.reachable", Kind: "state"},
		{Name: "some.log", Kind: "log"},
	})
	owner := storage.TaskOwner{Component: "disp-1", InterfaceName: "disp-1-tcp", InterfaceType: "tcp"}
	ev := &ogv1.Event{Datapoints: []*ogv1.Datapoint{
		{Name: "tcp.open", Value: &ogv1.Datapoint_DoubleValue{DoubleValue: 1}},
		{Name: "interface.reachable", Value: &ogv1.Datapoint_StringValue{StringValue: "up"}},
		{Name: "some.log", Value: &ogv1.Datapoint_StringValue{StringValue: "line"}},
		{Name: "not.registered", Value: &ogv1.Datapoint_StringValue{StringValue: "up"}},
	}}
	metrics, states := deriveDatapoints(ev, owner, reg)
	if len(metrics) != 1 || metrics[0].Key != "tcp.open" {
		t.Fatalf("metrics = %+v, want one tcp.open", metrics)
	}
	if len(states) != 1 {
		t.Fatalf("states = %+v, want one interface.reachable (log + unregistered dropped)", states)
	}
	s := states[0]
	if s.Key != "interface.reachable" || s.Value != "up" || s.OwnerKind != "component" ||
		s.OwnerID != "disp-1" || s.Instance != "disp-1-tcp" || s.Source != "tcp" {
		t.Fatalf("state routing/owner wrong: %+v", s)
	}
}
