package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// worklistTimeout bounds a single worklist request-reply.
const worklistTimeout = 5 * time.Second

// onceDrainTimeout bounds how long a one-shot run waits for the stream to ack
// what it collected; shutdownDrainTimeout bounds the same wait when a running
// node is stopped.
const (
	onceDrainTimeout     = 15 * time.Second
	shutdownDrainTimeout = 5 * time.Second
)

// Config is the node run-mode configuration.
type Config struct {
	ServerURL      string
	Name           string
	Token          string
	HeartbeatEvery time.Duration
	// Once runs a single claim + pull + heartbeat cycle and returns, for tests
	// and one-shot invocations.
	Once bool
	// Dialer is the tcp probe primitive the node runs its tcp tasks with. Nil
	// defaults to the real collection.NewTCPDialer(); tests inject a fake.
	Dialer collection.TCPDialer
	// Pinger is the icmp probe primitive the node runs its icmp tasks with. Nil
	// defaults to the real collection.NewICMPPinger(); tests inject a fake.
	Pinger collection.Pinger
	// HTTP is the layer-7 http probe primitive (#812). Nil defaults to the
	// real collection.NewHTTPProber(); tests inject a fake.
	HTTP collection.HTTPProber
	// HTTPFetch sends a driver's http and https requests (#902). Nil
	// defaults to the real collection.NewHTTPFetcher().
	HTTPFetch collection.HTTPFetcher
	// SSH is the layer-7 ssh probe primitive (#812). Nil defaults to the
	// real collection.NewSSHProber(); tests inject a fake.
	SSH collection.SSHProber
	// SNMP is the driver-poll v2c GET client (#814). Nil defaults to the
	// real collection.NewSNMPGetter(); tests inject a fake.
	SNMP collection.SNMPGetter
	// Line is the driver-poll stateless line-protocol client (#814). Nil
	// defaults to the real collection.NewLineExchanger(); tests inject a fake.
	Line collection.LineExchanger
	// LineSession is the held line-session dialer (#816) an endpoint with a
	// standing listen task over tcp holds its connection with. Nil defaults
	// to the real collection.NewLineSessionDialer(); tests inject a fake.
	LineSession collection.SessionDialer
	// SSHSession is the held ssh dialer (#1032). Nil defaults to the real
	// collection.NewSSHSessionDialer(); tests inject a fake.
	SSHSession collection.SessionDialer
	// TelnetSession is the held telnet dialer (#1033). Nil defaults to the
	// real collection.NewTelnetSessionDialer(); tests inject a fake.
	TelnetSession collection.SessionDialer
	// Telnet is the telnet reachability probe (#1033). Nil defaults to the
	// real collection.NewTelnetProber().
	Telnet collection.TelnetProbe
	// TLS is the tls reachability probe (#1035). Nil defaults to the real
	// collection.NewTLSProber().
	TLS collection.TLSProber
	// TLSSession is the held tls dialer (#1035). Nil defaults to the real
	// collection.NewTLSSessionDialer(); tests inject a fake.
	TLSSession collection.SessionDialer
	// WebSocketSession is the held websocket dialer (#1034), websocket and
	// wss. Nil defaults to the real collection.NewWebSocketDialer(); tests
	// shorten its keepalive.
	WebSocketSession collection.SessionDialer
}

// Run claims the node's NATS credential, connects outbound-only to the bus,
// pulls its worklist, and heartbeats until the context is cancelled (or a single
// cycle if Once). It returns the last worklist it pulled.
func Run(ctx context.Context, cfg Config) (collection.WorklistReply, error) {
	// The node's own logger: an slog handler that writes to the console and buffers
	// every record for shipment as a self-log (ADR-0066). The run loop drains and
	// publishes the buffer on the node's telemetry subject, so the node's operational
	// story (enrolled, worklist pulled, a task skipped) reaches the platform.
	//
	// It is installed as the PROCESS DEFAULT, which is what makes this a node-wide
	// emitter: any node code emits a self-log with a plain `slog.Info(...)`, with no
	// logger threaded down to it. The node run mode owns its process, so a global
	// default is the right scope; the prior default is restored on return so a test
	// binary (many Runs in one process) does not leak a dead sink. Emitting a new
	// self-log is one slog call, conventionally carrying a `facility` attr (and
	// optionally `source`), which the mapping lifts into the node_log columns.
	sink := newLogSink(slog.NewTextHandler(os.Stderr, nil))
	logger := slog.New(sink).With("node", cfg.Name)
	prior := slog.Default()
	defer slog.SetDefault(prior)
	slog.SetDefault(logger)

	creds, err := Claim(ctx, cfg.ServerURL, cfg.Name, cfg.Token)
	if err != nil {
		return collection.WorklistReply{}, err
	}
	nc, err := connectBus(creds.NatsURL, creds.Username, creds.Password, cfg.Name)
	if err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: connect bus at %s: %w", creds.NatsURL, err)
	}
	defer nc.Close()
	logger.Info("connected to bus", "facility", "enrollment", "url", creds.NatsURL)
	// Every telemetry publish goes through the spool (#453, #454): a JetStream
	// publish that waits for the stream's ack, a batch it has not acked held
	// and shipped on the next tick under the id it was minted with.
	js, err := newNodeJetStream(nc)
	if err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: jetstream: %w", err)
	}
	spool := newTelemetrySpool(collection.TelemetrySubject(cfg.Name), jetStreamPublish(js))

	dialer := cfg.Dialer
	if dialer == nil {
		dialer = collection.NewTCPDialer()
	}
	pinger := cfg.Pinger
	if pinger == nil {
		pinger = collection.NewICMPPinger()
	}
	httpProber := cfg.HTTP
	if httpProber == nil {
		httpProber = collection.NewHTTPProber()
	}
	sshProber := cfg.SSH
	if sshProber == nil {
		sshProber = collection.NewSSHProber()
	}
	snmpGetter := cfg.SNMP
	if snmpGetter == nil {
		snmpGetter = collection.NewSNMPGetter()
	}
	lineExchanger := cfg.Line
	if lineExchanger == nil {
		lineExchanger = collection.NewLineExchanger()
	}
	telnetProber := cfg.Telnet
	if telnetProber == nil {
		telnetProber = collection.NewTelnetProber()
	}
	tlsProber := cfg.TLS
	if tlsProber == nil {
		tlsProber = collection.NewTLSProber()
	}
	httpFetcher := cfg.HTTPFetch
	if httpFetcher == nil {
		httpFetcher = collection.NewHTTPFetcher()
	}
	runner := &collection.Runner{TCP: dialer, Ping: pinger, HTTP: httpProber, SSH: sshProber, SNMP: snmpGetter, Line: lineExchanger, Telnet: telnetProber, TLS: tlsProber, HTTPFetch: httpFetcher}

	// The held sessions (#816): one per endpoint whose worklist carries a
	// standing listen task over a transport with a session dialer (or a
	// poll whose driver declares a session, or over a transport with no
	// stateless line, #1049). A
	// transition lands at once (not on the next tick) as the session-state
	// property under the listen task, and as a self-log carrying the reason;
	// the deferred closeAll is the node-shutdown teardown, run before the bus
	// connection closes.
	lineSession := cfg.LineSession
	if lineSession == nil {
		lineSession = collection.NewLineSessionDialer()
	}
	// A listener's claimed push lands the same way, at once and under its
	// listen task (#817); the arm conversation is plumbing, so it is a
	// self-log and never a command row.
	telnetSession := cfg.TelnetSession
	if telnetSession == nil {
		telnetSession = collection.NewTelnetSessionDialer()
	}
	var hostKeyMu sync.Mutex
	hostKeyLogged := map[string]string{}
	sshSession := cfg.SSHSession
	if sshSession == nil {
		sshSession = collection.NewSSHSessionDialer()
	}
	tlsSession := cfg.TLSSession
	if tlsSession == nil {
		tlsSession = collection.NewTLSSessionDialer()
	}
	certLogged := map[string]string{}
	// An http endpoint's server-sent events stream is held as a session
	// (#1038), over plain http or the https handshake.
	streamSession := collection.NewHTTPStreamDialer()
	// A websocket is held as a session (#1034), over plain websocket or the
	// wss handshake.
	wsSession := cfg.WebSocketSession
	if wsSession == nil {
		wsSession = collection.NewWebSocketDialer()
	}
	sessions := newSessionManager(map[string]collection.SessionDialer{"tcp": lineSession, "tls": tlsSession, "telnet": telnetSession, "ssh": sshSession,
		"http": streamSession, "https": streamSession, "websocket": wsSession, "wss": wsSession}, sessionHooks{
		transition: func(taskID, endpoint string, tr collection.SessionTransition) {
			// A rejection is logged once, at the transition, never per tick
			// (#1049): the rejected session dials no more, so nothing repeats.
			level := slog.LevelInfo
			if tr.State == collection.SessionRejected {
				level = slog.LevelWarn
			}
			logger.Log(ctx, level, "session "+string(tr.State), "facility", "session", "task", taskID, "endpoint", endpoint, "reason", tr.Reason)
			if err := publishBatch(spool, cfg.Name, taskID, sessionSamples(tr)); err != nil {
				logger.Warn("session state not published", "facility", "session", "task", taskID, "error", err.Error())
			}
		},
		auth: func(taskID, endpoint string, v collection.AuthVerdict) {
			// The auth step's verdict is the endpoint-authenticated rung, the
			// ladder's own vocabulary, published when it changes.
			logger.Info("session authenticated", "facility", "session", "task", taskID, "endpoint", endpoint, "authenticated", v.Authenticated, "reason", v.Reason)
			if err := publishBatch(spool, cfg.Name, taskID, []collection.Sample{authSample(v)}); err != nil {
				logger.Warn("auth verdict not published", "facility", "session", "task", taskID, "error", err.Error())
			}
		},
		rejection: func(taskID, endpoint, value, reason string) {
			// The endpoint's gate, published when it changes so the server
			// carries it back to a restarted node (ADR-0158). The value holds
			// the connection fingerprint, a digest only.
			if err := publishBatch(spool, cfg.Name, taskID, rejectionSamples(value, reason, time.Now().UTC())); err != nil {
				logger.Warn("endpoint rejection not published", "facility", "session", "task", taskID, "error", err.Error())
			}
		},
		hostKey: func(taskID, endpoint, fp string) {
			// The ssh host key the session trusted: on first use this is the
			// pin the server keeps and delivers back (#1032). It publishes on
			// every authenticated connection; the log says it once per key.
			hostKeyMu.Lock()
			fresh := hostKeyLogged[taskID] != fp
			hostKeyLogged[taskID] = fp
			hostKeyMu.Unlock()
			if fresh {
				logger.Info("session host key trusted", "facility", "session", "task", taskID, "endpoint", endpoint, "fingerprint", fp)
			}
			if err := publishBatch(spool, cfg.Name, taskID, []collection.Sample{{
				Name: collection.SignalSSHHostKey, Text: fp, IsText: true, TS: time.Now().UTC(),
			}}); err != nil {
				logger.Warn("host key not published", "facility", "session", "task", taskID, "error", err.Error())
			}
		},
		certificate: func(taskID, endpoint, fp string) {
			// The tls public key the endpoint trusted on first use: the pin
			// the server keeps and delivers back (#1035), republished while
			// the server lacks it; the log says it once per key.
			hostKeyMu.Lock()
			fresh := certLogged[taskID] != fp
			certLogged[taskID] = fp
			hostKeyMu.Unlock()
			if fresh {
				logger.Info("tls certificate key trusted", "facility", "session", "task", taskID, "endpoint", endpoint, "fingerprint", fp)
			}
			if err := publishBatch(spool, cfg.Name, taskID, []collection.Sample{{
				Name: collection.SignalTLSCertificate, Text: fp, IsText: true, TS: time.Now().UTC(),
			}}); err != nil {
				logger.Warn("certificate key not published", "facility", "session", "task", taskID, "error", err.Error())
			}
		},
		samples: func(taskID string, dps []collection.Sample) {
			if err := publishBatch(spool, cfg.Name, taskID, dps); err != nil {
				logger.Warn("pushed samples not published", "facility", "session", "task", taskID, "error", err.Error())
			}
		},
		fault: func(taskID string, cause error) {
			logger.Warn("listener fault", "facility", "collection", "task", taskID, "error", cause.Error())
			_ = publishCollectionFailed(spool, cfg.Name, taskID, cause)
		},
		arm: func(taskID, endpoint, line, reply string, err error) {
			if errors.Is(err, collection.ErrUnacknowledged) {
				logger.Warn("session armed, unacknowledged", "facility", "session", "task", taskID, "endpoint", endpoint, "line", line)
				return
			}
			if err != nil {
				logger.Warn("session arm failed", "facility", "session", "task", taskID, "endpoint", endpoint, "line", line, "error", err.Error())
				return
			}
			logger.Info("session armed", "facility", "session", "task", taskID, "endpoint", endpoint, "line", line, "reply", reply)
		},
	})
	sessions.httpFetch = httpFetcher
	defer sessions.closeAll()

	// verdicts remembers the last reachability verdict per task (keyed by the
	// node-unique task id, since interface names collide across components) across
	// ticks, so the node emits endpoint-reachable only on a flip or first
	// observation (transition-only). It lives for the whole run, not per tick.
	verdicts := map[string]string{}
	// faultseen remembers each driver task's last fault set so a static
	// misconfiguration lands one collection-failed, not one per tick (#814).
	faultseen := map[string]string{}
	// sched remembers when each driver poll is next due, so a poll runs on
	// its declared schedule rather than every heartbeat (#822).
	heartbeat := cfg.HeartbeatEvery
	if heartbeat <= 0 {
		heartbeat = 30 * time.Second
	}
	sched := newPollSchedule(heartbeat)

	wl, err := pullWorklist(nc, cfg.Name)
	if err != nil {
		return collection.WorklistReply{}, err
	}
	logger.Info("worklist pulled", "facility", "collection", "tasks", len(wl.Tasks))
	sessions.reconcile(wl.Tasks)
	// executedCommands remembers each pulled command's outcome across ticks:
	// at-least-once delivery means redelivery, and idempotence per command id
	// lives here (the report repeats, the device is not touched twice). It is
	// pruned past the redelivery window so it does not grow for the life of the run.
	executedCommands := map[int64]commandOutcome{}
	if err := runTasks(ctx, spool, cfg.Name, wl, runner, sessions, sched, verdicts, faultseen); err != nil {
		return wl, err
	}
	runCommands(ctx, nc, cfg.Name, runner, sessions, executedCommands)
	if err := publishHeartbeat(nc, cfg.Name); err != nil {
		return wl, err
	}
	publishSelfLogs(spool, cfg.Name, sink)

	if cfg.Once {
		// A one-shot run ends only once the stream has acked what it
		// collected: a batch still held is an error, never a quiet exit.
		dctx, cancel := context.WithTimeout(ctx, onceDrainTimeout)
		defer cancel()
		if err := spool.drain(dctx); err != nil {
			return wl, err
		}
		_ = nc.Flush()
		return wl, nil
	}

	// The server's on-demand runs (#1163): a preview runs on its request's
	// goroutine, a run now waits for the loop below, which owns the sessions.
	runJobs := make(chan runJob, 1)
	runSub, err := subscribeRuns(ctx, nc, cfg.Name, runner, runJobs)
	if err != nil {
		return wl, fmt.Errorf("node: subscribe runs: %w", err)
	}
	defer func() { _ = runSub.Unsubscribe() }()

	every := cfg.HeartbeatEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Ship what the spool still holds before the connection closes,
			// within a bound: the spool is memory, so a batch left in it at
			// exit is gone.
			dctx, cancel := context.WithTimeout(context.Background(), shutdownDrainTimeout)
			if err := spool.drain(dctx); err != nil {
				logger.Warn("telemetry left unshipped at shutdown", "facility", "delivery", "error", err.Error())
			}
			cancel()
			_ = nc.Flush()
			return wl, nil
		case job := <-runJobs:
			if time.Now().Before(job.deadline) {
				job.respond(runNowTask(ctx, spool, cfg.Name, runner, sessions, sched, job.task, job.deadline))
			}
			publishSelfLogs(spool, cfg.Name, sink)
		case <-ticker.C:
			_ = publishHeartbeat(nc, cfg.Name)
			// Ship what the spool held since the last tick before this tick's
			// work, so batches land in the order they were collected (#454).
			spool.retry()
			// A batch the spool dropped at its cap may have carried a
			// transition the node remembers as sent: forget what was sent, so
			// this tick sends current state again (the property lane's
			// transition guard keeps an unchanged value from landing twice).
			if spool.takeOverflow() {
				clear(verdicts)
				clear(faultseen)
				sched.forgetOccurrences()
			}
			// Re-pull each tick; a changed config generation drives a refresh in
			// later checkpoints. A pull failure is non-fatal (retry next tick).
			if next, err := pullWorklist(nc, cfg.Name); err == nil {
				wl = next
			}
			// Hold, recycle, or tear down sessions to match the worklist.
			sessions.reconcile(wl.Tasks)
			// Run the worklist's tcp tasks and publish their telemetry. A publish
			// failure is non-fatal (retry next tick).
			_ = runTasks(ctx, spool, cfg.Name, wl, runner, sessions, sched, verdicts, faultseen)
			// Pull and actuate the pending command queue (#815).
			runCommands(ctx, nc, cfg.Name, runner, sessions, executedCommands)
			// Report the inbound lines no listener claimed this tick.
			sessions.flushUnclaimed()
			// Ship whatever the node logged this tick as self-logs.
			publishSelfLogs(spool, cfg.Name, sink)
		}
	}
}

// reconnectWait and reconnectJitter space the node's reconnect attempts
// (nats.go's own defaults); vars so a test can shorten an outage.
var (
	reconnectWait      = 2 * time.Second
	reconnectJitter    = 100 * time.Millisecond
	reconnectJitterTLS = time.Second
)

// connectBus opens the node's outbound connection to the bus under its own
// credential and inbox, reconnecting without limit.
func connectBus(url, user, password, name string) (*nats.Conn, error) {
	return nats.Connect(url,
		nats.UserInfo(user, password),
		nats.CustomInboxPrefix(collection.InboxPrefix(name)),
		nats.Name("omniglass-node-"+name),
		nats.ReconnectWait(reconnectWait),
		nats.ReconnectJitter(reconnectJitter, reconnectJitterTLS),
		// A node behind a WAN outage keeps trying for as long as it runs:
		// a connection that gave up would leave the node running and its
		// spool never shipping again until a restart lost it (#454).
		nats.MaxReconnects(-1),
		// Nothing published during an outage is buffered to send after it:
		// such a publish fails at once and the spool holds it. One
		// connection delivers in the order publishes were made, so with no
		// buffer across a reconnect a batch never lands ahead of an earlier
		// one that failed, and the spool's window stays in order (a
		// transition-only property series depends on it). The heartbeat
		// and a command's report are loss-tolerant by design.
		nats.ReconnectBufSize(-1),
	)
}

// newNodeJetStream is the node's JetStream context, the spool's publisher:
// each batch's ack is waited on for spoolPublishTimeout, and the window the
// spool keeps with the stream fits under its pending cap.
func newNodeJetStream(nc *nats.Conn) (jetstream.JetStream, error) {
	return jetstream.New(nc,
		jetstream.WithPublishAsyncTimeout(spoolPublishTimeout),
		jetstream.WithPublishAsyncMaxPending(2*spoolWindow))
}

// pullWorklist runs the worklist request-reply and decodes the reply.
func pullWorklist(nc *nats.Conn, name string) (collection.WorklistReply, error) {
	msg, err := nc.Request(collection.WorklistSubject(name), nil, worklistTimeout)
	if err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: pull worklist: %w", err)
	}
	var reply collection.WorklistReply
	if err := json.Unmarshal(msg.Data, &reply); err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: decode worklist: %w", err)
	}
	return reply, nil
}

// publishSelfLogs drains the node's captured log records and hands them to the
// spool as a logs-only telemetry batch on the node's own subject, where the
// ingest consumer lands them in node_log (ADR-0066, #589). The spool holds a
// batch the stream has not acked, so a drained line survives a publish
// outage up to the spool's cap (#454). It deliberately does not log its own
// failure, since emitting from the drain path would feed the buffer it just
// emptied.
func publishSelfLogs(out *telemetrySpool, name string, sink *logSink) {
	ev := selfLogBatch(name, sink.drain())
	if ev == nil {
		return
	}
	_ = out.send(ev)
}

// publishHeartbeat sends one liveness heartbeat on the node's own subject.
func publishHeartbeat(nc *nats.Conn, name string) error {
	b, err := json.Marshal(collection.Heartbeat{Node: name, At: time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("node: encode heartbeat: %w", err)
	}
	if err := nc.Publish(collection.HeartbeatSubject(name), b); err != nil {
		return fmt.Errorf("node: publish heartbeat: %w", err)
	}
	return nil
}
