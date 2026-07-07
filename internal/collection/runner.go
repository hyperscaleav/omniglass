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
	DatapointICMPReachable  = "icmp.reachable"
	DatapointICMPRTTAvg     = "icmp.rtt_avg"
)

// defaultTCPTimeout bounds a connect attempt when the task sets none.
const defaultTCPTimeout = 5 * time.Second

// defaultICMPTimeout / defaultICMPCount bound a ping attempt when the task sets
// neither: one echo, a two-second window.
const (
	defaultICMPTimeout = 2 * time.Second
	defaultICMPCount   = 1
)

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

// ICMPTask is the parsed icmp (ping) reachability unit a node runs: the echo
// target (host or IP), how many echoes to send, and the per-run timeout. The
// node builds it from a worklist task's interface params.
type ICMPTask struct {
	Target  string
	Count   int
	Timeout time.Duration
}

// Runner runs a node's collection tasks against injected probe primitives. It
// assigns NO component identity: produced datapoints carry only the measurement,
// and the owning component is bound server-side at ingest from the task's
// interface. Checkpoint 3 wired the tcp probe; checkpoint 4 adds the icmp probe;
// http/snmp extend it further.
type Runner struct {
	TCP  TCPDialer
	Ping Pinger
}

// CollectTCP runs one tcp task and returns its datapoints. A tcp probe always
// emits tcp.open (1.0 open, 0.0 closed) carrying the verdict reason as a label,
// and emits tcp.connect_time (ms) ONLY when open (absent when closed). A failed
// connect is data, not an error; err is returned only when the target could not
// be attempted (an unresolved host), so the caller skips the task rather than
// recording a false down.
func (r *Runner) CollectTCP(ctx context.Context, t TCPTask) ([]Datapoint, error) {
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

// PingResult is one icmp probe run's outcome: how many echoes returned, the
// average round-trip time over them, and the classified reachability reason. A
// zero Received with a down Reason is a valid answer (the target did not echo),
// not an absence of data.
type PingResult struct {
	Received int
	AvgRTT   time.Duration
	Reason   Reachability
}

// Pinger is the ICMP probe boundary, faked in unit tests so collection logic is
// hermetic (no raw sockets, no privilege). A target that does not echo is DATA:
// Received==0 with a down Reason (Timedout / Prohibited / Unreachable). err is
// reserved for the one inconclusive case, a node that cannot do ICMP at all (or
// an unresolvable host), which the caller treats as no datapoint, never as down.
type Pinger interface {
	Ping(ctx context.Context, target string, count int, timeout time.Duration) (PingResult, error)
}

// CollectICMP runs one icmp (ping) task and returns its datapoints. A ping probe
// always emits icmp.reachable (1.0 if any echo returned, 0.0 otherwise) carrying
// the verdict reason as a label, and emits icmp.rtt_avg (ms) ONLY when reachable
// (absent when unreachable). A target that does not answer is data, not an error;
// err is returned only when the node cannot attempt the probe at all (no ICMP
// capability, or an unresolvable host), so the caller skips the task rather than
// recording a false down.
func (r *Runner) CollectICMP(ctx context.Context, t ICMPTask) ([]Datapoint, error) {
	if t.Target == "" {
		return nil, fmt.Errorf("collection: icmp task: empty target")
	}
	count := t.Count
	if count <= 0 {
		count = defaultICMPCount
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = defaultICMPTimeout
	}
	res, err := r.Ping.Ping(ctx, t.Target, count, timeout)
	if err != nil {
		return nil, fmt.Errorf("collection: icmp ping %s: %w", t.Target, err)
	}
	now := time.Now().UTC()
	reachable := res.Received > 0

	reachVal := 0.0
	if reachable {
		reachVal = 1.0
	}
	out := []Datapoint{{
		Name:   DatapointICMPReachable,
		Value:  reachVal,
		TS:     now,
		Labels: map[string]string{ReasonLabel: string(pingReason(res))},
	}}
	if reachable {
		out = append(out, Datapoint{
			Name:  DatapointICMPRTTAvg,
			Value: float64(res.AvgRTT) / float64(time.Millisecond),
			TS:    now,
		})
	}
	return out, nil
}
