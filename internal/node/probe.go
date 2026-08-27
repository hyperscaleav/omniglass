package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// endpointParams is the address config a probe reads off a task's endpoint.
// A tcp probe needs the dial target (host:port) and an optional connect timeout;
// an icmp probe needs the echo target (host or IP), an optional echo count, and
// an optional per-run timeout.
type endpointParams struct {
	Target   string `json:"target"`
	Count    int    `json:"count,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// runTasks executes every probe task in the worklist and publishes one telemetry
// Event per task to the node's own telemetry subject. The node stamps NO
// component identity: the Event carries only the task_id and the measurements;
// the server binds the owner at ingest. A probe that cannot be attempted (bad
// params, unresolved host, no capability) is skipped, not fatal, so one bad task
// never stalls the rest of the worklist. tcp and icmp are the wired probe types;
// their samples ride the same pipeline (the ingest consumer does not branch
// on probe type).
func runTasks(ctx context.Context, nc *nats.Conn, node string, wl collection.WorklistReply, runner *collection.Runner, verdicts map[string]string) error {
	for _, task := range wl.Tasks {
		dps, err := collectTask(ctx, runner, task)
		if err != nil {
			// A skipped task is the node's own operational story AND a
			// collection fact (#812): log it as a self-log, and land a
			// collection-failed event naming the task, so the failure is
			// visible on the component's timeline rather than only in the
			// node's own logs. Never a false-down state sample.
			slog.Warn("task skipped", "facility", "collection", "task", task.ID, "error", err.Error())
			if pubErr := publishCollectionFailed(nc, node, task.ID, err); pubErr != nil {
				return pubErr
			}
			continue // unusable config or inconclusive probe: skip, no false down
		}
		if dps == nil {
			continue // an unwired transport: nothing to publish
		}
		// Compute and, on a transition only, append the endpoint reachability
		// verdict as a state sample. The node remembers the last verdict per
		// task and emits endpoint-reachable only on a flip or first observation,
		// so the state series is transition-only, not one row per tick. The key is
		// the task id, not the endpoint name: endpoint names are unique only per
		// component, so a node routinely probes two components' endpoints that
		// share a friendly name (the default is the protocol), and a name-keyed map
		// would suppress the second one's verdict. The task id is node-unique (a
		// content hash over the endpoint). The ingest-side latest-value guard is
		// the net for a node restart.
		dps = appendVerdict(dps, task.ID, verdicts)
		ev := buildBatch(task.ID, node, dps)
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

// appendVerdict computes the endpoint reachability verdict from a probe's
// samples and appends it (a state sample carrying up/down) only when it
// differs from the last verdict remembered for that task, or is the first
// observation. It records the emitted verdict in verdicts (keyed by the
// node-unique task id, since endpoint names collide across components) so the
// next tick can tell a flip from a repeat. When the probe produced no
// reachability metric (nothing to judge) or the verdict is unchanged, dps is
// returned untouched.
func appendVerdict(dps []collection.Sample, taskID string, verdicts map[string]string) []collection.Sample {
	up, ok := collection.EndpointVerdict(dps)
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
	return append(dps, collection.Sample{
		Name:   collection.SignalEndpointReachable,
		Text:   verdict,
		IsText: true,
		TS:     time.Now().UTC(),
	})
}

// collectTask dispatches a task to its probe by transport and returns the
// produced samples. A nil, nil return is a transport this node does not
// run (skip, nothing to publish); an error is an unusable config or an
// inconclusive probe (skip, no false down). Since #812 the ladder climbs to
// layer 7: http issues a real request and ssh runs a real key exchange, each
// carrying its L4 dial facts along, so reached-but-not-responsive and
// responded-but-not-authenticated are observable states; tcp stays the plain
// connect and icmp pings.
func collectTask(ctx context.Context, runner *collection.Runner, task collection.TaskSpec) ([]collection.Sample, error) {
	switch task.Transport {
	case "tcp":
		t, err := parseTCPTask(task)
		if err != nil {
			return nil, err
		}
		return runner.CollectTCP(ctx, t)
	case "http":
		t, err := parseHTTPTask(task)
		if err != nil {
			return nil, err
		}
		return runner.CollectHTTP(ctx, t)
	case "ssh":
		t, err := parseSSHTask(task)
		if err != nil {
			return nil, err
		}
		return runner.CollectSSH(ctx, t)
	case "icmp":
		t, err := parseICMPTask(task)
		if err != nil {
			return nil, err
		}
		return runner.CollectICMP(ctx, t)
	default:
		return nil, nil // unwired transport: nothing to run
	}
}

// parseTCPTask reads the dial target and timeout from a task's endpoint params.
func parseTCPTask(task collection.TaskSpec) (collection.TCPTask, error) {
	var p endpointParams
	if len(task.EndpointParams) > 0 {
		if err := json.Unmarshal(task.EndpointParams, &p); err != nil {
			return collection.TCPTask{}, fmt.Errorf("node: bad endpoint params for task %s: %w", task.ID, err)
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

// parseICMPTask reads the echo target, count, and timeout from a task's endpoint
// params. An empty target is a usage error the caller skips on.
func parseICMPTask(task collection.TaskSpec) (collection.ICMPTask, error) {
	var p endpointParams
	if len(task.EndpointParams) > 0 {
		if err := json.Unmarshal(task.EndpointParams, &p); err != nil {
			return collection.ICMPTask{}, fmt.Errorf("node: bad endpoint params for task %s: %w", task.ID, err)
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

// buildBatch maps produced samples to a per-lane TelemetryBatch (#594). Pure:
// no I/O. The probes know their lanes statically: a numeric probe value rides
// the metric lane, a text verdict rides the property lane as canonical JSON
// text. A sample the probe did not stamp carries NO per-sample ts (the batch ts
// governs at ingest); stamping the zero time would read as the year one
// downstream. The per-sample labels are carried but not persisted in this
// checkpoint.
func buildBatch(taskID, node string, dps []collection.Sample) *ogv1.TelemetryBatch {
	ev := &ogv1.TelemetryBatch{
		TaskId: taskID,
		NodeId: node,
		Ts:     timestamppb.New(time.Now().UTC()),
	}
	for _, d := range dps {
		var ts *timestamppb.Timestamp
		if !d.TS.IsZero() {
			ts = timestamppb.New(d.TS)
		}
		if d.IsText {
			// json.Marshal of a string cannot fail; it yields the quoted canonical form.
			vj, _ := json.Marshal(d.Text)
			ev.Properties = append(ev.Properties, &ogv1.PropertySample{
				Name:      d.Name,
				ValueJson: string(vj),
				Ts:        ts,
				Labels:    d.Labels,
			})
			continue
		}
		ev.Metrics = append(ev.Metrics, &ogv1.MetricSample{
			Name:   d.Name,
			Value:  d.Value,
			Ts:     ts,
			Labels: d.Labels,
		})
	}
	return ev
}

// publishCollectionFailed lands the collection-failed occurrence for a task
// whose config or payload could not be used: the event carries the reason as
// its message and the task identity in its payload, so a failing check is a
// fact on the component's timeline, never only a line in the node's own logs.
func publishCollectionFailed(nc *nats.Conn, node, taskID string, cause error) error {
	payload, _ := json.Marshal(map[string]string{"task": taskID})
	ev := &ogv1.TelemetryBatch{
		TaskId: taskID,
		NodeId: node,
		Ts:     timestamppb.New(time.Now().UTC()),
		Events: []*ogv1.EventSample{{
			Name:    collection.EventCollectionFailed,
			Message: cause.Error(),
			Payload: payload,
		}},
	}
	b, err := proto.Marshal(ev)
	if err != nil {
		return fmt.Errorf("node: marshal collection-failed event: %w", err)
	}
	if err := nc.Publish(collection.TelemetrySubject(node), b); err != nil {
		return fmt.Errorf("node: publish collection-failed event: %w", err)
	}
	return nil
}

// parseHTTPTask reads the request target and timeout from a task's endpoint
// params.
func parseHTTPTask(task collection.TaskSpec) (collection.HTTPTask, error) {
	var p endpointParams
	if len(task.EndpointParams) > 0 {
		if err := json.Unmarshal(task.EndpointParams, &p); err != nil {
			return collection.HTTPTask{}, fmt.Errorf("node: bad endpoint params for task %s: %w", task.ID, err)
		}
	}
	if p.Target == "" {
		return collection.HTTPTask{}, fmt.Errorf("node: task %s: empty http target", task.ID)
	}
	var timeout time.Duration
	if p.Timeout != "" {
		d, err := time.ParseDuration(p.Timeout)
		if err != nil {
			return collection.HTTPTask{}, fmt.Errorf("node: task %s: bad timeout %q: %w", task.ID, p.Timeout, err)
		}
		timeout = d
	}
	return collection.HTTPTask{Target: p.Target, Timeout: timeout}, nil
}

// parseSSHTask reads the dial target, the optional credential the auth rung
// tries (plain params today; secret references arrive with the driver spec,
// #813), and the timeout from a task's endpoint params.
func parseSSHTask(task collection.TaskSpec) (collection.SSHTask, error) {
	var p endpointParams
	if len(task.EndpointParams) > 0 {
		if err := json.Unmarshal(task.EndpointParams, &p); err != nil {
			return collection.SSHTask{}, fmt.Errorf("node: bad endpoint params for task %s: %w", task.ID, err)
		}
	}
	if p.Target == "" {
		return collection.SSHTask{}, fmt.Errorf("node: task %s: empty ssh target", task.ID)
	}
	var timeout time.Duration
	if p.Timeout != "" {
		d, err := time.ParseDuration(p.Timeout)
		if err != nil {
			return collection.SSHTask{}, fmt.Errorf("node: task %s: bad timeout %q: %w", task.ID, p.Timeout, err)
		}
		timeout = d
	}
	return collection.SSHTask{Target: p.Target, Username: p.Username, Password: p.Password, Timeout: timeout}, nil
}
