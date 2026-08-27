package collection

import (
	"context"
	"fmt"
	"time"
)

// The canonical reachability sample names the tcp probe emits. They are
// seeded metric_types (internal/seed/metric_types.yaml); the ingest
// consumer's reject-not-project drops any name absent from that registry.
const (
	SignalTCPOpen        = "tcp-open"
	SignalTCPConnectTime = "tcp-connect-time"
	SignalICMPReachable  = "icmp-reachable"
	SignalICMPRTTAvg     = "icmp-rtt-avg"
)

// defaultTCPTimeout bounds a connect attempt when the task sets none.
const defaultTCPTimeout = 5 * time.Second

// defaultICMPTimeout / defaultICMPCount bound a ping attempt when the task sets
// neither: one echo, a two-second window.
const (
	defaultICMPTimeout = 2 * time.Second
	defaultICMPCount   = 1
)

// Sample is one observation produced by a probe or computed by the node: a
// canonical name, a value, a timestamp, and labels. A metric rides Value (float);
// a state verdict (endpoint-reachable) rides Text with IsText set, so the same
// list carries both to buildBatch, which maps a text sample to the proto
// string_value and a metric to double_value. Labels (the reason) are not
// persisted yet, but the probe still produces them.
type Sample struct {
	Name   string
	Value  float64
	Text   string
	IsText bool
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
// assigns NO component identity: produced samples carry only the measurement,
// and the owning component is bound server-side at ingest from the task's
// interface. Checkpoint 3 wired the tcp probe; checkpoint 4 adds the icmp probe;
// http/snmp extend it further.
type Runner struct {
	TCP  TCPDialer
	Ping Pinger
	HTTP HTTPProber
	SSH  SSHProber
}

// CollectTCP runs one tcp task and returns its samples. A tcp probe always
// emits tcp-open (1.0 open, 0.0 closed) carrying the verdict reason as a label,
// and emits tcp-connect-time (ms) ONLY when open (absent when closed). A failed
// connect is data, not an error; err is returned only when the target could not
// be attempted (an unresolved host), so the caller skips the task rather than
// recording a false down.
func (r *Runner) CollectTCP(ctx context.Context, t TCPTask) ([]Sample, error) {
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
	out := []Sample{{
		Name:   SignalTCPOpen,
		Value:  openVal,
		TS:     now,
		Labels: map[string]string{ReasonLabel: string(reach)},
	}}
	if open {
		out = append(out, Sample{
			Name:  SignalTCPConnectTime,
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
// an unresolvable host), which the caller treats as no sample, never as down.
type Pinger interface {
	Ping(ctx context.Context, target string, count int, timeout time.Duration) (PingResult, error)
}

// CollectICMP runs one icmp (ping) task and returns its samples. A ping probe
// always emits icmp-reachable (1.0 if any echo returned, 0.0 otherwise) carrying
// the verdict reason as a label, and emits icmp-rtt-avg (ms) ONLY when reachable
// (absent when unreachable). A target that does not answer is data, not an error;
// err is returned only when the node cannot attempt the probe at all (no ICMP
// capability, or an unresolvable host), so the caller skips the task rather than
// recording a false down.
func (r *Runner) CollectICMP(ctx context.Context, t ICMPTask) ([]Sample, error) {
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
	out := []Sample{{
		Name:   SignalICMPReachable,
		Value:  reachVal,
		TS:     now,
		Labels: map[string]string{ReasonLabel: string(pingReason(res))},
	}}
	if reachable {
		out = append(out, Sample{
			Name:  SignalICMPRTTAvg,
			Value: float64(res.AvgRTT) / float64(time.Millisecond),
			TS:    now,
		})
	}
	return out, nil
}

// The layer-7 ladder signals (#812): the response metrics born five-lane in
// the seed catalogs, and the responds/auth verdict properties beside
// endpoint-reachable. EventCollectionFailed is the occurrence a task lands
// when its payload or config cannot be used, never a silent drop.
const (
	SignalHTTPResponseTime      = "http-response-time"
	SignalSSHHandshakeTime      = "ssh-handshake-time"
	SignalEndpointResponsive    = "endpoint-responsive"
	SignalEndpointAuthenticated = "endpoint-authenticated"
	EventCollectionFailed       = "collection-failed"
)

// HTTPTask is the parsed http reachability-plus-responsiveness unit a node
// runs: the request target (host:port) and the per-attempt timeout.
type HTTPTask struct {
	Target  string
	Timeout time.Duration
}

// SSHTask is the parsed ssh unit: the dial target, the optional credential the
// auth rung tries (params today; secret references with #813), and the
// per-attempt timeout.
type SSHTask struct {
	Target   string
	Username string
	Password string
	Timeout  time.Duration
}

// ladderVerdict renders one responds/auth verdict as a property-lane text
// sample: the value is up/down (or yes/no for the auth rung), the reason label
// says why when down. The ingest consumer stores transitions only, so a probe
// may emit its verdict every run.
func ladderVerdict(name, value string, reason Reachability, ts time.Time) Sample {
	return Sample{
		Name:   name,
		Text:   value,
		IsText: true,
		TS:     ts,
		Labels: map[string]string{ReasonLabel: string(reason)},
	}
}

// CollectHTTP runs one http task: the L4 dial facts (tcp-open, and
// tcp-connect-time when open) fall out of the same request, the L7 rung is
// whether a real HTTP response was drawn (http-response-time when it was, any
// status: a 500 is an API answering), and the endpoint-responsive verdict
// rides the property lane. A port that accepts but never answers is
// reached-but-not-responsive, distinct from port-closed. err is returned only
// when the target could not be attempted at all (inconclusive).
func (r *Runner) CollectHTTP(ctx context.Context, t HTTPTask) ([]Sample, error) {
	if t.Target == "" {
		return nil, fmt.Errorf("collection: http task: empty target")
	}
	res, err := r.HTTP.Probe(ctx, t.Target, t.Timeout)
	if err != nil {
		return nil, fmt.Errorf("collection: http probe %s: %w", t.Target, err)
	}
	now := time.Now().UTC()
	openVal := 0.0
	if res.Dialed {
		openVal = 1.0
	}
	out := []Sample{{
		Name:   SignalTCPOpen,
		Value:  openVal,
		TS:     now,
		Labels: map[string]string{ReasonLabel: string(res.Reason)},
	}}
	if res.Dialed {
		out = append(out, Sample{Name: SignalTCPConnectTime, Value: res.DialMS, TS: now})
	}
	responsive := "down"
	if res.Responded {
		responsive = "up"
		out = append(out, Sample{Name: SignalHTTPResponseTime, Value: res.ResponseMS, TS: now})
	}
	out = append(out, ladderVerdict(SignalEndpointResponsive, responsive, res.Reason, now))
	return out, nil
}

// CollectSSH runs one ssh task: the L4 dial facts, the L7 rung (the key
// exchange reached authentication: ssh-handshake-time when it did), the
// endpoint-responsive verdict, and, ONLY when a credential was supplied, the
// endpoint-authenticated verdict (yes/no). A daemon that accepts the port but
// never completes the exchange is reached-but-not-responsive; one that
// completes it and refuses the credential is responded-but-not-authenticated.
func (r *Runner) CollectSSH(ctx context.Context, t SSHTask) ([]Sample, error) {
	if t.Target == "" {
		return nil, fmt.Errorf("collection: ssh task: empty target")
	}
	res, err := r.SSH.Probe(ctx, t.Target, SSHCredential{Username: t.Username, Password: t.Password}, t.Timeout)
	if err != nil {
		return nil, fmt.Errorf("collection: ssh probe %s: %w", t.Target, err)
	}
	now := time.Now().UTC()
	openVal := 0.0
	if res.Dialed {
		openVal = 1.0
	}
	out := []Sample{{
		Name:   SignalTCPOpen,
		Value:  openVal,
		TS:     now,
		Labels: map[string]string{ReasonLabel: string(res.Reason)},
	}}
	if res.Dialed {
		out = append(out, Sample{Name: SignalTCPConnectTime, Value: res.DialMS, TS: now})
	}
	responsive := "down"
	if res.Responded {
		responsive = "up"
		out = append(out, Sample{Name: SignalSSHHandshakeTime, Value: res.HandshakeMS, TS: now})
	}
	out = append(out, ladderVerdict(SignalEndpointResponsive, responsive, res.Reason, now))
	if res.AuthAttempted {
		authed := "no"
		if res.Authenticated {
			authed = "yes"
		}
		out = append(out, ladderVerdict(SignalEndpointAuthenticated, authed, res.Reason, now))
	}
	return out, nil
}
