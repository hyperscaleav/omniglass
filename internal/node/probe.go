package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/transport"
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
	// TLSMode is a tls endpoint's verification mode (#1035), derived from
	// its driver's tls-mode input; empty verifies.
	TLSMode string `json:"tls-mode,omitempty"`
}

// runTasks executes every probe task in the worklist and publishes one telemetry
// Event per task to the node's own telemetry subject. The node stamps NO
// component identity: the Event carries only the task_id and the measurements;
// the server binds the owner at ingest. A probe that cannot be attempted (bad
// params, unresolved host, no capability) is skipped, not fatal, so one bad task
// never stalls the rest of the worklist. tcp and icmp are the wired probe types;
// their samples ride the same pipeline (the ingest consumer does not branch
// on probe type).
func runTasks(ctx context.Context, nc *nats.Conn, node string, wl collection.WorklistReply, runner *collection.Runner, sessions *sessionManager, sched *pollSchedule, verdicts map[string]string, faultseen map[string]string) error {
	now := time.Now()
	present := make(map[string]bool, len(wl.Tasks))
	for _, task := range wl.Tasks {
		present[task.ID] = true
	}
	sched.retain(present)
	for _, task := range wl.Tasks {
		// A standing listen task runs no tick of its own: it is what holds
		// its endpoint's session (the session manager reconciles it, #816),
		// and its listeners arm over that session with #817. Skip without a
		// collection-failed, since an armed-later task is not a fault.
		if task.Mode == "listen" {
			continue
		}
		// A driver poll (a baked function, #814) interprets rather than
		// probes: fetch over the transport, locate and type each emit. Its
		// faults are per-emit collection-failed stories beside whatever
		// samples still landed, so a payload missing one value never
		// silently drops the rest. Faults publish transition-only (like the
		// reachability verdict): a static misconfiguration that faults every
		// tick lands one event, not one per tick forever, so a stuck driver
		// does not flood the event lane.
		if driver.IsBaked(task.Spec) {
			// A driver poll runs on its declared schedule (#822), not on
			// every heartbeat.
			interval := pollInterval(task)
			sched.rebook(task.ID, task.Fingerprint)
			sched.releaseRefused(task.ID, sessions.isUp(task.EndpointID))
			if !sched.due(task.ID, interval, now) {
				continue
			}
			var dps []collection.Sample
			var faults []error
			if gt, gated := sessions.blocked(task.EndpointID, statelessRung(task)); gated && sessions.lookup(task.EndpointID) == nil {
				// A stateless poll on a gated endpoint is not retried under
				// the fingerprint it was refused under (#1056 ruling).
				faults = []error{fmt.Errorf("node: task %s: the endpoint is gated: %w", task.ID, gt.refusal())}
			} else {
				dps, faults = collectDriverTask(ctx, runner, task, sessions)
				if sessions.lookup(task.EndpointID) == nil {
					for _, f := range faults {
						if terr, ok := collection.AsTerminal(f); ok {
							sessions.refuseOver(task.Transport, task.EndpointID, task.Fingerprint, terr)
						}
					}
				}
			}
			bookPollRun(sched, task.ID, interval, now, dps, faults)
			current := ""
			for _, f := range faults {
				current += f.Error() + "\n"
			}
			if current != faultseen[task.ID] {
				for _, f := range faults {
					slog.Warn("driver poll fault", "facility", "collection", "task", task.ID, "error", f.Error())
					if pubErr := publishCollectionFailed(nc, node, task.ID, f); pubErr != nil {
						return pubErr
					}
				}
				// Remembered only once published, so a fault set whose publish
				// failed is told again next tick rather than lost.
				faultseen[task.ID] = current
			}
			if len(dps) == 0 {
				continue
			}
			if err := publishBatch(nc, node, task.ID, dps); err != nil {
				return err
			}
			continue
		}
		// The ladder's informational ping runs beside the probe, bounded,
		// so gear that drops ICMP costs the tick nothing extra.
		pinged := make(chan []collection.Sample, 1)
		go func(task collection.TaskSpec) { pinged <- pingInformational(ctx, runner, task, nil) }(task)
		dps, err := collectProbe(ctx, runner, task, sessions)
		pings := <-pinged // joined here, before any path leaves the iteration
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
		dps = append(dps, pings...)
		if err := publishBatch(nc, node, task.ID, dps); err != nil {
			return err
		}
	}
	return nil
}

// bookPollRun books a poll's next run from what its run landed. A run that
// landed nothing and faulted (the fetch failed) retries on a backoff rather
// than waiting its whole interval, unless the fault is terminal (#1049) or
// the request's own (an http 404, #902): a fault retrying cannot fix waits
// the interval, never the backoff. A run whose only landing is the session
// rung it proved (an http answer's endpoint-authenticated) landed no data,
// and a retryable answer's backoff waits at least its Retry-After.
func bookPollRun(sched *pollSchedule, taskID string, interval time.Duration, now time.Time, dps []collection.Sample, faults []error) {
	if !landedData(dps) && len(faults) > 0 {
		for _, f := range faults {
			_, terminal := collection.AsTerminal(f)
			_, own := collection.AsRequestFault(f)
			if terminal || own {
				sched.refusedRun(taskID)
				return
			}
		}
		atLeast := time.Duration(0)
		for _, f := range faults {
			if wait, ok := collection.RetryAfterOf(f); ok && wait > atLeast {
				atLeast = wait
			}
		}
		sched.failedAtLeast(taskID, interval, now, atLeast)
		return
	}
	sched.succeeded(taskID)
}

// landedData reports a run that landed a sample beyond the session rung its
// answer proved.
func landedData(dps []collection.Sample) bool {
	for _, d := range dps {
		if d.Name != collection.SignalEndpointAuthenticated {
			return true
		}
	}
	return false
}

// pollInterval reads a driver poll's declared cadence off its baked spec;
// zero (run every tick) when it declares none or does not parse, where the
// poll itself then faults by name.
func pollInterval(task collection.TaskSpec) time.Duration {
	fn, err := driver.ParseBaked(task.Spec)
	if err != nil || fn.Schedule == nil {
		return 0
	}
	return fn.Schedule.Interval()
}

// publishBatch maps samples to a per-lane TelemetryBatch and publishes it on
// the node's own telemetry subject.
func publishBatch(nc *nats.Conn, node, taskID string, dps []collection.Sample) error {
	ev := buildBatch(taskID, node, dps)
	b, err := proto.Marshal(ev)
	if err != nil {
		return fmt.Errorf("node: marshal telemetry event: %w", err)
	}
	if err := nc.Publish(collection.TelemetrySubject(node), b); err != nil {
		return fmt.Errorf("node: publish telemetry: %w", err)
	}
	return nil
}

// collectDriverTask parses one derived driver poll and runs it: the baked
// function from the task spec, the target from the endpoint params, the
// unsealed secret inputs from the worklist, and the endpoint's held session
// when the node holds one (#816), which the poll borrows instead of dialing.
// Faults are the collection-failed stories; a parse fault covers the whole
// task.
func collectDriverTask(ctx context.Context, runner *collection.Runner, task collection.TaskSpec, sessions *sessionManager) ([]collection.Sample, []error) {
	fn, err := driver.ParseBaked(task.Spec)
	if err != nil {
		return nil, []error{fmt.Errorf("node: task %s: %w", task.ID, err)}
	}
	var p endpointParams
	if len(task.EndpointParams) > 0 {
		if err := json.Unmarshal(task.EndpointParams, &p); err != nil {
			return nil, []error{fmt.Errorf("node: bad endpoint params for task %s: %w", task.ID, err)}
		}
	}
	var timeout time.Duration
	if p.Timeout != "" {
		d, err := time.ParseDuration(p.Timeout)
		if err != nil {
			return nil, []error{fmt.Errorf("node: task %s: bad timeout %q: %w", task.ID, p.Timeout, err)}
		}
		timeout = d
	}
	pt := collection.DriverPollTask{
		Fn:          fn,
		Transport:   task.Transport,
		Target:      p.Target,
		Secrets:     task.Secrets,
		Timeout:     timeout,
		Session:     sessions.lookup(task.EndpointID),
		SessionOnly: task.Session != nil,
		Auth:        task.Session,
	}
	if driver.IsHTTP(task.Transport) {
		// An http poll holds no session (#902): it is its own request, its
		// credential from the session block, its trust (https) the one the
		// endpoint's probe shares, the pin included.
		pt.Session, pt.SessionOnly = nil, false
		if task.Transport == "https" {
			trust, err := tlsTrustOf(p)
			if err != nil {
				return nil, []error{fmt.Errorf("node: task %s: %w", task.ID, err)}
			}
			if trust.Mode == driver.TLSModePin {
				trust.Pin = sessions.certPinOf(task)
			}
			pt.TLS = &trust
		}
	}
	if task.Transport == "udp" {
		pt.Datagram = sessions.datagramOf(task.EndpointID)
		pt.Retries = fn.Request.RetriesOr(driver.DefaultPollRetries)
		if pt.Datagram == nil {
			return nil, []error{fmt.Errorf("node: task %s: no udp socket is bound for the endpoint: %s", task.ID, sessions.datagramRefusal(task.EndpointID))}
		}
	}
	return runner.CollectDriverPoll(ctx, pt)
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

// collectProbe runs a reachability probe, except over an endpoint whose held
// telnet session reads up (#1033): the up session is the proof the port and
// the protocol answer, and a second dial would be refused by gear that admits
// one telnet client, reading not responsive beside an up session.
func collectProbe(ctx context.Context, runner *collection.Runner, task collection.TaskSpec, sessions *sessionManager) ([]collection.Sample, error) {
	if task.Transport == "telnet" && sessions.isUp(task.EndpointID) {
		now := time.Now().UTC()
		return []collection.Sample{
			{Name: collection.SignalTCPOpen, Value: 1, TS: now, Labels: map[string]string{collection.ReasonLabel: string(collection.Responded)}},
			{Name: collection.SignalEndpointResponsive, Text: "up", IsText: true, TS: now,
				Labels: map[string]string{collection.ReasonLabel: "the held session is up"}},
		}, nil
	}
	// The ladder a driver declares (ADR-0158): its transport rung, the gate,
	// is a connect, the protocol's handshake, or a named request answered.
	if l := task.Ladder; l != nil {
		switch {
		case l.Transport == driver.RungRequest && l.Request != nil && task.Transport == "udp":
			return collectDatagramRung(ctx, task, l.Request, sessions)
		case l.Transport == driver.RungRequest && l.Request != nil && task.Transport == "snmp":
			return collectSNMPRung(ctx, runner, task, l.Request, sessions)
		case l.Transport == driver.RungRequest && l.Request != nil:
			return collectRequestRung(ctx, runner, task, l.Request)
		case l.Transport == driver.RungConnect && (task.Transport == "ssh" || transport.UsesTLS(task.Transport)):
			t, err := parseTCPTask(task)
			if err != nil {
				return nil, err
			}
			return runner.CollectTCP(ctx, t)
		}
	}
	switch task.Transport {
	case "ssh":
		return collectSSHProbe(ctx, runner, task, sessions)
	case "tls":
		return collectTLSProbe(ctx, runner, task, sessions)
	case "https":
		return collectHTTPSProbe(ctx, runner, task, sessions)
	}
	return collectTask(ctx, runner, task)
}

// collectHTTPSProbe runs the https probe on the gate (#902), as the tls
// probe does: with the transport rung gated (a changed key, a certificate
// that failed verification) only the TCP connect runs, so no handshake
// reaches a key nobody trusted; otherwise the handshake runs under the
// endpoint's mode and the pin its polls share, then one credential-free
// GET /, and a certificate refusal it meets gates the endpoint.
func collectHTTPSProbe(ctx context.Context, runner *collection.Runner, task collection.TaskSpec, sessions *sessionManager) ([]collection.Sample, error) {
	t, err := parseTCPTask(task)
	if err != nil {
		return nil, err
	}
	if gt, ok := sessions.blocked(task.EndpointID, rungResponsive); ok && gt.rung == rungResponsive {
		return runner.CollectTCP(ctx, t)
	}
	var p endpointParams
	_ = json.Unmarshal(task.EndpointParams, &p) // parseTCPTask has read it already
	trust, err := tlsTrustOf(p)
	if err != nil {
		return nil, fmt.Errorf("node: task %s: %w", task.ID, err)
	}
	if trust.Mode == driver.TLSModePin {
		trust.Pin = sessions.certPinOf(task)
	}
	dps, res, err := runner.CollectHTTPResult(ctx, collection.HTTPTask{Target: t.Target, Timeout: t.Timeout, TLS: &trust})
	if err != nil {
		return nil, err
	}
	if res.Refusal != nil {
		slog.Warn("https certificate refused", "facility", "collection", "task", task.ID, "endpoint", task.EndpointName, "reason", res.Refusal.Error())
		sessions.refuse(task.EndpointID, task.Fingerprint, res.Refusal)
	}
	return dps, nil
}

// collectSSHProbe runs the ssh probe on the gate (#1060). With the transport
// rung gated (a changed host key) only the rung below runs, a TCP connect.
// With the session rung gated (a refused credential) the key exchange still
// runs, so responsiveness stays observed, but the credential is not sent
// again under the same fingerprint. The probe honours the pinned key, so a
// credential never reaches a component whose key changed; what it meets
// (a refusal, a changed key, a pass) moves the gate.
func collectSSHProbe(ctx context.Context, runner *collection.Runner, task collection.TaskSpec, sessions *sessionManager) ([]collection.Sample, error) {
	t, err := parseSSHTask(task)
	if err != nil {
		return nil, err
	}
	t.HostKeyPin = task.HostKey
	if gt, ok := sessions.blocked(task.EndpointID, rungResponsive); ok && gt.rung == rungResponsive {
		return runner.CollectTCP(ctx, collection.TCPTask{Target: t.Target, Timeout: t.Timeout})
	}
	if _, ok := sessions.blocked(task.EndpointID, rungAuthenticated); ok {
		t.Username, t.Password = "", ""
	}
	dps, res, err := runner.CollectSSHResult(ctx, t)
	if err != nil {
		return nil, err
	}
	switch {
	case res.HostKeyChanged:
		sessions.refuse(task.EndpointID, task.Fingerprint, collection.HostKeyRefusal(t.HostKeyPin, res.OfferedKey))
	case res.AuthAttempted && !res.Authenticated:
		sessions.refuse(task.EndpointID, task.Fingerprint, &collection.TerminalError{Class: driver.ClassAuth, Reason: "the component refused the probe's credential"})
	case res.Authenticated:
		sessions.pass(task.EndpointID)
	}
	return dps, nil
}

// collectTLSProbe runs the tls probe on the gate (#1035). With the transport
// rung gated (a changed key, a certificate that failed verification) only
// the rung below runs, a TCP connect, so no handshake reaches a key nobody
// trusted. Otherwise the handshake runs under the endpoint's mode and its
// pin (the one its held session shares), and a certificate refusal it meets
// gates the endpoint, carrying both keys when a pin refused one.
func collectTLSProbe(ctx context.Context, runner *collection.Runner, task collection.TaskSpec, sessions *sessionManager) ([]collection.Sample, error) {
	t, err := parseTCPTask(task)
	if err != nil {
		return nil, err
	}
	if gt, ok := sessions.blocked(task.EndpointID, rungResponsive); ok && gt.rung == rungResponsive {
		return runner.CollectTCP(ctx, t)
	}
	var p endpointParams
	_ = json.Unmarshal(task.EndpointParams, &p) // parseTCPTask has read it already
	trust, err := tlsTrustOf(p)
	if err != nil {
		return nil, fmt.Errorf("node: task %s: %w", task.ID, err)
	}
	if trust.Mode == driver.TLSModePin {
		trust.Pin = sessions.certPinOf(task)
	}
	dps, res, err := runner.CollectTLSResult(ctx, collection.TLSTask{Target: t.Target, Timeout: t.Timeout, Trust: trust})
	if err != nil {
		return nil, err
	}
	if res.Refusal != nil {
		// The reason (an unknown authority, a name mismatch, an expiry, a
		// changed key) is what an operator acts on, and an endpoint with no
		// held session has no session-reason to carry it: say it once, here,
		// since beneath the gate this handshake is not tried again.
		slog.Warn("tls certificate refused", "facility", "collection", "task", task.ID, "endpoint", task.EndpointName, "reason", res.Refusal.Error())
		sessions.refuse(task.EndpointID, task.Fingerprint, res.Refusal)
	}
	return dps, nil
}

// collectRequestRung runs a request transport rung (ADR-0158): the driver's
// named request, answered, is the transport proven where an open port cannot
// be detected. The line protocol carries it here; snmp's own request is #821.
func collectRequestRung(ctx context.Context, runner *collection.Runner, task collection.TaskSpec, req *driver.Request) ([]collection.Sample, error) {
	t, err := parseTCPTask(task)
	if err != nil {
		return nil, err
	}
	if runner.Line == nil {
		return nil, fmt.Errorf("node: task %s: no line client for the request rung", task.ID)
	}
	now := time.Now().UTC()
	answer, xerr := runner.Line.Exchange(ctx, t.Target, req.Line, t.Timeout)
	answered := xerr == nil && req.Reply.Accepts(answer)
	reason := collection.Responded
	if xerr != nil {
		reason = collection.Timedout
		if r, ok := collection.Classify(xerr); ok && r != collection.Responded {
			reason = r
		}
	} else if !answered {
		reason = collection.Timedout
	}
	open := 0.0
	if xerr == nil || reason == collection.Timedout {
		open = 1.0 // the port took the request; whether it answered is the next fact
	}
	verdict := "down"
	if answered {
		verdict = "up"
	}
	return []collection.Sample{
		{Name: collection.SignalTCPOpen, Value: open, TS: now, Labels: map[string]string{collection.ReasonLabel: string(reason)}},
		{Name: collection.SignalEndpointResponsive, Text: verdict, IsText: true, TS: now, Labels: map[string]string{collection.ReasonLabel: string(reason)}},
	}, nil
}

// informationalPingTimeout bounds the ladder's ping: it is informational, so
// it never holds a tick.
const informationalPingTimeout = 500 * time.Millisecond

// pingInformational lands the ladder's ping (ADR-0158) beside an endpoint's
// probe: icmp-reachable and its rtt, informational only. It is appended after
// the verdict is judged, so a component that blocks ping never reads down
// for it; a node that cannot ping lands nothing.
func pingInformational(ctx context.Context, runner *collection.Runner, task collection.TaskSpec, dps []collection.Sample) []collection.Sample {
	if task.Ladder == nil || !task.Ladder.Ping || task.Transport == "icmp" || runner.Ping == nil {
		return dps
	}
	t, err := parseTCPTask(task)
	if err != nil {
		return dps
	}
	host, _, err := net.SplitHostPort(t.Target)
	if err != nil {
		host = t.Target
	}
	pings, err := runner.CollectICMP(ctx, collection.ICMPTask{Target: host, Timeout: informationalPingTimeout})
	if err != nil {
		return dps
	}
	// One observation per probe (the architect's ruling on probe samples):
	// the reachable fact only, no averaged figure.
	for _, p := range pings {
		if p.Name != collection.SignalICMPReachable {
			continue
		}
		if p.Labels == nil {
			p.Labels = map[string]string{}
		}
		p.Labels["informational"] = "true"
		dps = append(dps, p)
	}
	return dps
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
	case "telnet":
		t, err := parseTCPTask(task)
		if err != nil {
			return nil, err
		}
		return runner.CollectTelnet(ctx, collection.TelnetTask{Target: t.Target, Timeout: t.Timeout})
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
				Labels:    validLabels(d.Labels),
			})
			continue
		}
		ev.Metrics = append(ev.Metrics, &ogv1.MetricSample{
			Name:   d.Name,
			Value:  d.Value,
			Ts:     ts,
			Labels: validLabels(d.Labels),
		})
	}
	return ev
}

// validLabels is a sample's labels as valid UTF-8: a reason may carry a
// component's own text (an http error page), and a protobuf string refuses
// invalid UTF-8, which would fail the whole batch.
func validLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return labels
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[strings.ToValidUTF8(k, "\uFFFD")] = strings.ToValidUTF8(v, "\uFFFD")
	}
	return out
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
			Name: collection.EventCollectionFailed,
			// A cause may carry a component's own text; a protobuf string
			// refuses invalid UTF-8.
			Message: strings.ToValidUTF8(cause.Error(), "\uFFFD"),
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
