package node

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// TestParseTCPTask: the dial target and timeout come off the interface params;
// an empty target is a usage error the caller skips on.
func TestParseTCPTask(t *testing.T) {
	spec := collection.TaskSpec{ID: "t1", Transport: "tcp", EndpointParams: json.RawMessage(`{"target":"10.0.0.5:22","timeout":"2s"}`)}
	got, err := parseTCPTask(spec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Target != "10.0.0.5:22" || got.Timeout.String() != "2s" {
		t.Fatalf("parse = %+v, want target 10.0.0.5:22 timeout 2s", got)
	}

	if _, err := parseTCPTask(collection.TaskSpec{ID: "t2", Transport: "tcp", EndpointParams: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("empty target: want error")
	}
}

// TestParseICMPTask: the ping target, count, and timeout come off the interface
// params; an empty target is a usage error the caller skips on.
func TestParseICMPTask(t *testing.T) {
	spec := collection.TaskSpec{ID: "t1", Transport: "icmp", EndpointParams: json.RawMessage(`{"target":"10.0.0.5","count":3,"timeout":"2s"}`)}
	got, err := parseICMPTask(spec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Target != "10.0.0.5" || got.Count != 3 || got.Timeout.String() != "2s" {
		t.Fatalf("parse = %+v, want target 10.0.0.5 count 3 timeout 2s", got)
	}

	if _, err := parseICMPTask(collection.TaskSpec{ID: "t2", Transport: "icmp", EndpointParams: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("empty target: want error")
	}
}

// TestBuildBatchLanes: the batch carries the task id and node id, no component
// identity, and each produced sample rides its own lane (#594): a numeric probe
// result on metrics, a text verdict on properties as canonical JSON text. The
// probes know their lanes statically, so no registry is consulted node-side.
func TestBuildBatchLanes(t *testing.T) {
	dps := []collection.Sample{
		{Name: collection.SignalTCPOpen, Value: 1, Labels: map[string]string{collection.ReasonLabel: "responded"}},
		{Name: collection.SignalTCPConnectTime, Value: 3.5},
		{Name: collection.SignalEndpointReachable, Text: collection.VerdictUp, IsText: true},
	}
	ev := buildBatch("t1", "node-a", dps)
	if ev.GetTaskId() != "t1" || ev.GetNodeId() != "node-a" {
		t.Fatalf("batch ids = %q/%q, want t1/node-a", ev.GetTaskId(), ev.GetNodeId())
	}
	if len(ev.GetMetrics()) != 2 || len(ev.GetProperties()) != 1 {
		t.Fatalf("lanes = %d metrics / %d properties, want 2/1", len(ev.GetMetrics()), len(ev.GetProperties()))
	}
	first := ev.GetMetrics()[0]
	if first.GetName() != collection.SignalTCPOpen || first.GetValue() != 1 {
		t.Fatalf("first metric = %+v, want tcp-open=1", first)
	}
	verdict := ev.GetProperties()[0]
	if verdict.GetName() != collection.SignalEndpointReachable || verdict.GetValueJson() != `"up"` {
		t.Fatalf("verdict property = %+v, want endpoint-reachable with value_json %q", verdict, `"up"`)
	}
}

// TestBuildBatchTS: a sample the probe stamped keeps its ts verbatim; an
// unstamped sample carries NO per-sample ts, so the batch ts governs at ingest.
// The old wire stamped timestamppb.New(zero) on unstamped samples, which reads
// as the year one downstream, so absence must be encoded as absence.
func TestBuildBatchTS(t *testing.T) {
	stamped := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	dps := []collection.Sample{
		{Name: collection.SignalTCPOpen, Value: 1, TS: stamped},
		{Name: collection.SignalTCPConnectTime, Value: 3.5},
	}
	ev := buildBatch("t1", "node-a", dps)
	if ev.GetTs() == nil {
		t.Fatal("batch ts must be stamped so unstamped samples resolve to it")
	}
	if got := ev.GetMetrics()[0].GetTs(); got == nil || !got.AsTime().Equal(stamped) {
		t.Fatalf("stamped sample ts = %v, want %v verbatim", got, stamped)
	}
	if got := ev.GetMetrics()[1].GetTs(); got != nil {
		t.Fatalf("unstamped sample ts = %v, want none (the batch ts governs)", got)
	}
}

// TestAppendVerdict proves the node-side transition-only invariant: the first
// observation emits a verdict, an unchanged verdict on the next tick emits
// nothing, and a flip emits again. This is the primary defense; the ingest guard
// is the restart net.
func TestAppendVerdict(t *testing.T) {
	verdicts := map[string]string{}
	up := []collection.Sample{{Name: collection.SignalTCPOpen, Value: 1}}
	down := []collection.Sample{{Name: collection.SignalTCPOpen, Value: 0}}

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
	if got := appendVerdict([]collection.Sample{{Name: collection.SignalTCPConnectTime, Value: 3}}, "if-3", verdicts); len(got) != 1 {
		t.Fatalf("no reachability metric: want no verdict, got %+v", got)
	}
}

// TestParseHTTPTask / TestParseSSHTask: the L7 tasks come off the endpoint
// params (#812); the ssh credential is optional and rides plain params until
// the driver spec's secret references (#813).
func TestParseHTTPTask(t *testing.T) {
	spec := collection.TaskSpec{ID: "t1", Transport: "http", EndpointParams: json.RawMessage(`{"target":"10.0.0.5:80","timeout":"3s"}`)}
	got, err := parseHTTPTask(spec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Target != "10.0.0.5:80" || got.Timeout.String() != "3s" {
		t.Fatalf("parse = %+v", got)
	}
	if _, err := parseHTTPTask(collection.TaskSpec{ID: "t2", Transport: "http", EndpointParams: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("empty target: want error")
	}
}

func TestParseSSHTask(t *testing.T) {
	spec := collection.TaskSpec{ID: "t1", Transport: "ssh", EndpointParams: json.RawMessage(`{"target":"10.0.0.5:22","username":"svc","password":"pw"}`)}
	got, err := parseSSHTask(spec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Target != "10.0.0.5:22" || got.Username != "svc" || got.Password != "pw" {
		t.Fatalf("parse = %+v", got)
	}
}

// TestCollectTaskDispatchesTheLadder: the http and ssh transports reach their
// own L7 probes since #812, no longer the plain tcp connect.
func TestCollectTaskDispatchesTheLadder(t *testing.T) {
	runner := &collection.Runner{
		HTTP: dispatchHTTPFake{},
		SSH:  dispatchSSHFake{},
	}
	dps, err := collectTask(t.Context(), runner, collection.TaskSpec{ID: "h", Transport: "http", EndpointParams: json.RawMessage(`{"target":"a:80"}`)})
	if err != nil {
		t.Fatalf("http dispatch: %v", err)
	}
	if !hasSample(dps, "http-response-time") {
		t.Fatalf("http dispatch missed the L7 probe: %v", names(dps))
	}
	dps, err = collectTask(t.Context(), runner, collection.TaskSpec{ID: "s", Transport: "ssh", EndpointParams: json.RawMessage(`{"target":"a:22","username":"u","password":"p"}`)})
	if err != nil {
		t.Fatalf("ssh dispatch: %v", err)
	}
	if !hasSample(dps, "ssh-handshake-time") || !hasSample(dps, "endpoint-authenticated") {
		t.Fatalf("ssh dispatch missed the L7 probe: %v", names(dps))
	}
}

type dispatchHTTPFake struct{}

func (dispatchHTTPFake) Probe(_ context.Context, _ string, _ time.Duration) (collection.HTTPResult, error) {
	return collection.HTTPResult{Dialed: true, Responded: true, DialMS: 1, ResponseMS: 2, Reason: collection.Responded}, nil
}

type dispatchSSHFake struct{}

func (dispatchSSHFake) Probe(_ context.Context, _ string, _ collection.SSHCredential, _ time.Duration) (collection.SSHResult, error) {
	return collection.SSHResult{Dialed: true, Responded: true, AuthAttempted: true, Authenticated: true, DialMS: 1, HandshakeMS: 2, Reason: collection.Responded}, nil
}

func hasSample(dps []collection.Sample, name string) bool {
	for _, d := range dps {
		if d.Name == name {
			return true
		}
	}
	return false
}

func names(dps []collection.Sample) []string {
	var out []string
	for _, d := range dps {
		out = append(out, d.Name)
	}
	return out
}
