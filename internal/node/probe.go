package node

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// interfaceParams is the endpoint config a probe reads off a task's interface.
// A tcp probe needs the dial target (host:port) and an optional connect timeout;
// an icmp probe needs the echo target (host or IP), an optional echo count, and
// an optional per-run timeout.
type interfaceParams struct {
	Target  string `json:"target"`
	Count   int    `json:"count,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// runTasks executes every probe task in the worklist and publishes one telemetry
// Event per task to the node's own telemetry subject. The node stamps NO
// component identity: the Event carries only the task_id and the measurements;
// the server binds the owner at ingest. A probe that cannot be attempted (bad
// params, unresolved host, no capability) is skipped, not fatal, so one bad task
// never stalls the rest of the worklist. tcp and icmp are the wired probe types;
// their datapoints ride the same pipeline (the ingest consumer does not branch
// on probe type).
func runTasks(ctx context.Context, nc *nats.Conn, node string, wl collection.WorklistReply, dialer collection.TCPDialer, pinger collection.Pinger, verdicts map[string]string) error {
	runner := &collection.Runner{TCP: dialer, Ping: pinger}
	for _, task := range wl.Tasks {
		dps, err := collectTask(ctx, runner, task)
		if err != nil {
			continue // unusable config or inconclusive probe: skip, no false down
		}
		if dps == nil {
			continue // an unwired interface type: nothing to publish
		}
		// Compute and, on a transition only, append the interface reachability
		// verdict as a state datapoint. The node remembers the last verdict per
		// task and emits interface.reachable only on a flip or first observation,
		// so the state series is transition-only, not one row per tick. The key is
		// the task id, not the interface name: interface names are unique only per
		// component, so a node routinely probes two components' interfaces that
		// share a friendly name (the default is the protocol), and a name-keyed map
		// would suppress the second one's verdict. The task id is node-unique (a
		// content hash over the interface). The ingest-side latest-value guard is
		// the net for a node restart.
		dps = appendVerdict(dps, task.ID, verdicts)
		ev := buildEvent(task.ID, node, dps)
		b, err := proto.Marshal(ev)
		if err != nil {
			return fmt.Errorf("node: marshal telemetry event: %w", err)
		}
		if err := nc.Publish(collection.TelemetrySubject(node), b); err != nil {
			return fmt.Errorf("node: publish telemetry: %w", err)
		}
	}
	return nil
}

// appendVerdict computes the interface reachability verdict from a probe's
// datapoints and appends it (a state Datapoint carrying up/down) only when it
// differs from the last verdict remembered for that task, or is the first
// observation. It records the emitted verdict in verdicts (keyed by the
// node-unique task id, since interface names collide across components) so the
// next tick can tell a flip from a repeat. When the probe produced no
// reachability metric (nothing to judge) or the verdict is unchanged, dps is
// returned untouched.
func appendVerdict(dps []collection.Datapoint, taskID string, verdicts map[string]string) []collection.Datapoint {
	up, ok := collection.InterfaceVerdict(dps)
	if !ok {
		return dps
	}
	verdict := collection.VerdictDown
	if up {
		verdict = collection.VerdictUp
	}
	if prev, seen := verdicts[taskID]; seen && prev == verdict {
		return dps // no transition: transition-only, emit nothing
	}
	verdicts[taskID] = verdict
	return append(dps, collection.Datapoint{
		Name:   collection.DatapointInterfaceReachable,
		Text:   verdict,
		IsText: true,
		TS:     time.Now().UTC(),
	})
}

// collectTask dispatches a task to its probe by interface type and returns the
// produced datapoints. A nil, nil return is an interface type this node does not
// run (skip, nothing to publish); an error is an unusable config or an
// inconclusive probe (skip, no false down). The transport is the reachability
// axis: tcp, ssh, and http all reach by opening the tcp port (the driver that
// speaks the protocol over the transport is a later collection layer), so they
// share the tcp-connect probe; icmp pings.
func collectTask(ctx context.Context, runner *collection.Runner, task collection.TaskSpec) ([]collection.Datapoint, error) {
	switch task.InterfaceType {
	case "tcp", "ssh", "http":
		t, err := parseTCPTask(task)
		if err != nil {
			return nil, err
		}
		return runner.CollectTCP(ctx, t)
	case "icmp":
		t, err := parseICMPTask(task)
		if err != nil {
			return nil, err
		}
		return runner.CollectICMP(ctx, t)
	default:
		return nil, nil // unwired interface type: nothing to run
	}
}

// parseTCPTask reads the dial target and timeout from a task's interface params.
func parseTCPTask(task collection.TaskSpec) (collection.TCPTask, error) {
	var p interfaceParams
	if len(task.InterfaceParams) > 0 {
		if err := json.Unmarshal(task.InterfaceParams, &p); err != nil {
			return collection.TCPTask{}, fmt.Errorf("node: bad interface params for task %s: %w", task.ID, err)
		}
	}
	if p.Target == "" {
		return collection.TCPTask{}, fmt.Errorf("node: task %s: empty tcp target", task.ID)
	}
	var timeout time.Duration
	if p.Timeout != "" {
		d, err := time.ParseDuration(p.Timeout)
		if err != nil {
			return collection.TCPTask{}, fmt.Errorf("node: task %s: bad timeout %q: %w", task.ID, p.Timeout, err)
		}
		timeout = d
	}
	return collection.TCPTask{Target: p.Target, Timeout: timeout}, nil
}

// parseICMPTask reads the echo target, count, and timeout from a task's interface
// params. An empty target is a usage error the caller skips on.
func parseICMPTask(task collection.TaskSpec) (collection.ICMPTask, error) {
	var p interfaceParams
	if len(task.InterfaceParams) > 0 {
		if err := json.Unmarshal(task.InterfaceParams, &p); err != nil {
			return collection.ICMPTask{}, fmt.Errorf("node: bad interface params for task %s: %w", task.ID, err)
		}
	}
	if p.Target == "" {
		return collection.ICMPTask{}, fmt.Errorf("node: task %s: empty icmp target", task.ID)
	}
	var timeout time.Duration
	if p.Timeout != "" {
		d, err := time.ParseDuration(p.Timeout)
		if err != nil {
			return collection.ICMPTask{}, fmt.Errorf("node: task %s: bad timeout %q: %w", task.ID, p.Timeout, err)
		}
		timeout = d
	}
	return collection.ICMPTask{Target: p.Target, Count: p.Count, Timeout: timeout}, nil
}

// buildEvent maps produced datapoints to a telemetry Event. Pure: no I/O. The
// numeric probe values ride double_value; the per-datapoint labels are carried
// but not persisted in this checkpoint.
func buildEvent(taskID, node string, dps []collection.Datapoint) *ogv1.Event {
	ev := &ogv1.Event{
		TaskId: taskID,
		NodeId: node,
		Ts:     timestamppb.New(time.Now().UTC()),
	}
	for _, d := range dps {
		pd := &ogv1.Datapoint{
			Name:   d.Name,
			Ts:     timestamppb.New(d.TS),
			Labels: d.Labels,
		}
		if d.IsText {
			pd.Value = &ogv1.Datapoint_StringValue{StringValue: d.Text}
		} else {
			pd.Value = &ogv1.Datapoint_DoubleValue{DoubleValue: d.Value}
		}
		ev.Datapoints = append(ev.Datapoints, pd)
	}
	return ev
}
