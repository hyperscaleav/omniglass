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

// TestBuildEventText: a text (state) datapoint rides the proto string_value, not
// double_value, so the ingest consumer routes it to state.
func TestBuildEventText(t *testing.T) {
	dps := []collection.Datapoint{
		{Name: collection.DatapointTCPOpen, Value: 1},
		{Name: collection.DatapointInterfaceReachable, Text: collection.VerdictUp, IsText: true},
	}
	ev := buildEvent("t1", "node-a", dps)
	verdict := ev.GetDatapoints()[1]
	if verdict.GetName() != collection.DatapointInterfaceReachable || verdict.GetStringValue() != "up" {
		t.Fatalf("verdict datapoint = %+v, want interface.reachable=up on string_value", verdict)
	}
	if verdict.GetDoubleValue() != 0 {
		t.Fatalf("a text datapoint must not set double_value, got %v", verdict.GetDoubleValue())
	}
}

// TestAppendVerdict proves the node-side transition-only invariant: the first
// observation emits a verdict, an unchanged verdict on the next tick emits
// nothing, and a flip emits again. This is the primary defense; the ingest guard
// is the restart net.
func TestAppendVerdict(t *testing.T) {
	verdicts := map[string]string{}
	up := []collection.Datapoint{{Name: collection.DatapointTCPOpen, Value: 1}}
	down := []collection.Datapoint{{Name: collection.DatapointTCPOpen, Value: 0}}

	// First observation (up): the verdict is appended.
	got := appendVerdict(up, "if-1", verdicts)
	if len(got) != 2 || !got[1].IsText || got[1].Text != collection.VerdictUp {
		t.Fatalf("first tick: want an up verdict appended, got %+v", got)
	}

	// Same up again: no transition, nothing appended.
	if got := appendVerdict(up, "if-1", verdicts); len(got) != 1 {
		t.Fatalf("unchanged tick: want no verdict appended, got %+v", got)
	}

	// Flip to down: the verdict is appended again.
	got = appendVerdict(down, "if-1", verdicts)
	if len(got) != 2 || got[1].Text != collection.VerdictDown {
		t.Fatalf("flip tick: want a down verdict appended, got %+v", got)
	}

	// A different interface tracks independently (first observation emits).
	if got := appendVerdict(up, "if-2", verdicts); len(got) != 2 {
		t.Fatalf("other interface: want its own first verdict, got %+v", got)
	}

	// An interface whose probe carries no reachability metric has no verdict.
	if got := appendVerdict([]collection.Datapoint{{Name: collection.DatapointTCPConnectTime, Value: 3}}, "if-3", verdicts); len(got) != 1 {
		t.Fatalf("no reachability metric: want no verdict, got %+v", got)
	}
}
