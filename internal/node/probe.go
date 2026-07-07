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
// A tcp probe needs the dial target (host:port) and an optional connect timeout.
type interfaceParams struct {
	Target  string `json:"target"`
	Timeout string `json:"timeout,omitempty"`
}

// runTasks executes every tcp task in the worklist and publishes one telemetry
// Event per task to the node's own telemetry subject. The node stamps NO
// component identity: the Event carries only the task_id and the measurements;
// the server binds the owner at ingest. A probe that cannot be attempted (bad
// params, unresolved host) is skipped, not fatal, so one bad task never stalls
// the rest of the worklist.
func runTasks(ctx context.Context, nc *nats.Conn, node string, wl collection.WorklistReply, dialer collection.TCPDialer) error {
	runner := &collection.Runner{TCP: dialer}
	for _, task := range wl.Tasks {
		if task.InterfaceType != "tcp" {
			continue // only tcp is wired in this checkpoint
		}
		t, err := parseTCPTask(task)
		if err != nil {
			continue // unusable task config: skip, do not record a false down
		}
		dps, err := runner.Collect(ctx, t)
		if err != nil {
			continue // inconclusive (could not attempt): skip
		}
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
		ev.Datapoints = append(ev.Datapoints, &ogv1.Datapoint{
			Name:   d.Name,
			Value:  &ogv1.Datapoint_DoubleValue{DoubleValue: d.Value},
			Ts:     timestamppb.New(d.TS),
			Labels: d.Labels,
		})
	}
	return ev
}
