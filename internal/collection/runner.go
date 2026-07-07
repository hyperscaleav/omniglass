package collection

import (
	"context"
	"fmt"
	"time"
)

// The canonical reachability datapoint names the tcp probe emits. They are
// seeded datapoint_types (internal/seed/datapoint_types.yaml); the ingest
// consumer's reject-not-project drops any name absent from that registry.
const (
	DatapointTCPOpen        = "tcp.open"
	DatapointTCPConnectTime = "tcp.connect_time"
)

// defaultTCPTimeout bounds a connect attempt when the task sets none.
const defaultTCPTimeout = 5 * time.Second

// Datapoint is one observation produced by a probe: a canonical name, a numeric
// value, a timestamp, and labels. Labels (the reason) are not persisted in
// checkpoint 3 (only the typed row lands), but the probe still produces them, so
// a later checkpoint can carry them without changing the primitive.
type Datapoint struct {
	Name   string
	Value  float64
	TS     time.Time
	Labels map[string]string
}

// TCPTask is the parsed tcp reachability unit a node runs: the dial target
// (host:port) and the connect timeout. The node builds it from a worklist task's
// interface params.
type TCPTask struct {
	Target  string
	Timeout time.Duration
}

// Runner runs a node's collection tasks against injected probe primitives. It
// assigns NO component identity: produced datapoints carry only the measurement,
// and the owning component is bound server-side at ingest from the task's
// interface. Checkpoint 3 wires only the tcp probe; ping/http/snmp extend it.
type Runner struct {
	TCP TCPDialer
}

// Collect runs one tcp task and returns its datapoints. A tcp probe always emits
// tcp.open (1.0 open, 0.0 closed) carrying the verdict reason as a label, and
// emits tcp.connect_time (ms) ONLY when open (absent when closed). A failed
// connect is data, not an error; err is returned only when the target could not
// be attempted (an unresolved host), so the caller skips the task rather than
// recording a false down.
func (r *Runner) Collect(ctx context.Context, t TCPTask) ([]Datapoint, error) {
	if t.Target == "" {
		return nil, fmt.Errorf("collection: tcp task: empty target")
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = defaultTCPTimeout
	}
	connectMS, reach, err := r.TCP.Dial(ctx, t.Target, timeout)
	if err != nil {
		return nil, fmt.Errorf("collection: tcp dial %s: %w", t.Target, err)
	}
	now := time.Now().UTC()
	open := reach.Up()

	openVal := 0.0
	if open {
		openVal = 1.0
	}
	out := []Datapoint{{
		Name:   DatapointTCPOpen,
		Value:  openVal,
		TS:     now,
		Labels: map[string]string{ReasonLabel: string(reach)},
	}}
	if open {
		out = append(out, Datapoint{
			Name:  DatapointTCPConnectTime,
			Value: connectMS,
			TS:    now,
		})
	}
	return out, nil
}
