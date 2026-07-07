package node

import (
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// TestParseTCPTask: the dial target and timeout come off the interface params;
// an empty target is a usage error the caller skips on.
func TestParseTCPTask(t *testing.T) {
	spec := collection.TaskSpec{ID: "t1", InterfaceType: "tcp", InterfaceParams: json.RawMessage(`{"target":"10.0.0.5:22","timeout":"2s"}`)}
	got, err := parseTCPTask(spec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Target != "10.0.0.5:22" || got.Timeout.String() != "2s" {
		t.Fatalf("parse = %+v, want target 10.0.0.5:22 timeout 2s", got)
	}

	if _, err := parseTCPTask(collection.TaskSpec{ID: "t2", InterfaceType: "tcp", InterfaceParams: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("empty target: want error")
	}
}

// TestParseICMPTask: the ping target, count, and timeout come off the interface
// params; an empty target is a usage error the caller skips on.
func TestParseICMPTask(t *testing.T) {
	spec := collection.TaskSpec{ID: "t1", InterfaceType: "icmp", InterfaceParams: json.RawMessage(`{"target":"10.0.0.5","count":3,"timeout":"2s"}`)}
	got, err := parseICMPTask(spec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Target != "10.0.0.5" || got.Count != 3 || got.Timeout.String() != "2s" {
		t.Fatalf("parse = %+v, want target 10.0.0.5 count 3 timeout 2s", got)
	}

	if _, err := parseICMPTask(collection.TaskSpec{ID: "t2", InterfaceType: "icmp", InterfaceParams: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("empty target: want error")
	}
}

// TestBuildEvent: the Event carries the task id and node id, no component
// identity, and one proto datapoint per produced datapoint with double_value set.
func TestBuildEvent(t *testing.T) {
	dps := []collection.Datapoint{
		{Name: collection.DatapointTCPOpen, Value: 1, Labels: map[string]string{collection.ReasonLabel: "responded"}},
		{Name: collection.DatapointTCPConnectTime, Value: 3.5},
	}
	ev := buildEvent("t1", "node-a", dps)
	if ev.GetTaskId() != "t1" || ev.GetNodeId() != "node-a" {
		t.Fatalf("event ids = %q/%q, want t1/node-a", ev.GetTaskId(), ev.GetNodeId())
	}
	if len(ev.GetDatapoints()) != 2 {
		t.Fatalf("datapoints = %d, want 2", len(ev.GetDatapoints()))
	}
	first := ev.GetDatapoints()[0]
	if first.GetName() != collection.DatapointTCPOpen || first.GetDoubleValue() != 1 {
		t.Fatalf("first datapoint = %+v, want tcp.open=1", first)
	}
}
