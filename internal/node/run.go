package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// DefaultHeartbeat is how often a node checks in, and runs every task that
// declares no slower schedule, when it is not told otherwise: the product's
// own cadence (the lab's `default` cadence profile runs at it).
const DefaultHeartbeat = 30 * time.Second

// DefaultWorklistEvery is how often a node pulls its worklist when it is not
// told otherwise (#1577): on its own interval, not the heartbeat's, so a
// short heartbeat does not mean a worklist rebuild as often.
const DefaultWorklistEvery = 30 * time.Second

// DefaultSessionDials is how many held sessions a node lets make their first
// dial and handshake at once when it is not told otherwise (#1636): enough that a restart with
// hundreds of held endpoints comes up in seconds, few enough that it does not
// open them all in one burst against the network and the devices.
const DefaultSessionDials = 16

// worklistTimeout bounds a single worklist request-reply; a var so a test can
// shorten it.
var worklistTimeout = 5 * time.Second

// pullBackoffBase and pullBackoffCap bound the wait after a failed worklist
// pull (#1577); vars so a test can shorten them.
var (
	pullBackoffBase = time.Second
	pullBackoffCap  = 30 * time.Second
)

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
	// ProbeEvery is how often each reachability probe runs (#1273), on the
	// dispatcher's timer, never the heartbeat (#1634). Zero is
	// DefaultProbeEvery.
	ProbeEvery time.Duration
	// LaneWorkers is how many endpoint lanes run at once (#1634, #1249):
	// each runs one request to its endpoint at a time, an operator's ahead
	// of the scheduled. Zero is DefaultLaneWorkers.
	LaneWorkers int
	// WorklistEvery is how often the node pulls its worklist, on a goroutine
	// of its own, so a pull never holds the heartbeat (#1577). Zero pulls at
	// the heartbeat's interval; `node run` defaults it to DefaultWorklistEvery.
	WorklistEvery time.Duration
	// ReportEvery is the node's own collection report window (#1369),
	// overriding the platform's the worklist carries: a test's, or a lab's.
	// Zero takes the worklist's.
	ReportEvery time.Duration
	// SessionDials bounds how many held sessions make their first dial and
	// handshake at once (#1636); a session waiting for a slot reads
	// establishing, and a redial takes none. Zero takes
	// DefaultSessionDials.
	SessionDials int
	// Once runs a single claim + pull + heartbeat cycle and returns, for tests
	// and one-shot invocations.
	Once bool
	// Dialer is the tcp probe primitive the node runs its tcp tasks with. Nil
	// defaults to the real collection.NewTalliedTCPDialer(Tally); tests inject a fake.
	Dialer collection.TCPDialer
	// Pinger is the icmp probe primitive the node runs its icmp tasks with. Nil
	// defaults to the real collection.NewICMPPinger(); tests inject a fake.
	Pinger collection.Pinger
	// HTTP is the layer-7 http probe primitive (#812). Nil defaults to the
	// real collection.NewTalliedHTTPProber(Tally); tests inject a fake.
	HTTP collection.HTTPProber
	// HTTPFetch sends a driver's http and https requests (#902). Nil
	// defaults to the real collection.NewTalliedHTTPFetcher(Tally).
	HTTPFetch collection.HTTPFetcher
	// SSH is the layer-7 ssh probe primitive (#812). Nil defaults to the
	// real collection.NewSSHProber(); tests inject a fake.
	SSH collection.SSHProber
	// SNMP is the driver-poll v2c GET client (#814). Nil defaults to the
	// real collection.NewTalliedSNMPGetter(Tally); tests inject a fake.
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
	// Tally counts what the node sends (#1639), read by a scale lab on the
	// node's tally subject. Nil defaults to a fresh tally; the transports the
	// node builds itself count into it, and an injected transport counts
	// only if it was built with the same tally.
	Tally *collection.RequestTally
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
	// and shipped on the next tick under the id it was minted with. A tick's
	// task groups ride one batch (#1582), flushed on the spool's own timer, at
	// its byte threshold, and by every drain below.
	js, err := newNodeJetStream(nc)
	if err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: jetstream: %w", err)
	}
	tally := cfg.Tally
	if tally == nil {
		tally = collection.NewRequestTally()
	}
	spool := newTelemetrySpool(collection.TelemetrySubject(cfg.Name), cfg.Name, jetStreamPublish(js))
	spool.tally = tally

	dialer := cfg.Dialer
	if dialer == nil {
		dialer = collection.NewTalliedTCPDialer(tally)
	}
	pinger := cfg.Pinger
	if pinger == nil {
		pinger = collection.NewICMPPinger()
	}
	httpProber := cfg.HTTP
	if httpProber == nil {
		httpProber = collection.NewTalliedHTTPProber(tally)
	}
	sshProber := cfg.SSH
	if sshProber == nil {
		sshProber = collection.NewSSHProber()
	}
	snmpGetter := cfg.SNMP
	if snmpGetter == nil {
		snmpGetter = collection.NewTalliedSNMPGetter(tally)
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
		httpFetcher = collection.NewTalliedHTTPFetcher(tally)
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
	// d is the dispatcher (#1634), made once the first worklist is in; a
	// session transition may open or close a gate, so it asks for a pass.
	var d *dispatcher
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
			if d != nil {
				d.kick()
			}
		},
		gateMoved: func() {
			// A disruptive delivery closed its endpoints' gates (#1651): a
			// pass defers their probes and re-arms the timer for the hold
			// backstop.
			if d != nil {
				d.kick()
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
			if err := publishPushed(spool, taskID, dps); err != nil {
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
	sessions.dials = collection.NewDialSlots(sessionDials(cfg.SessionDials))
	defer sessions.closeAll()

	heartbeat := cfg.HeartbeatEvery
	if heartbeat <= 0 {
		heartbeat = DefaultHeartbeat
	}
	// sched remembers what each poll's runs booked on top of its interval: a
	// retry backoff, a refusal, the fingerprint it runs under (#822, #1634).
	sched := newPollSchedule()

	wl, ok, err := firstWorklist(ctx, nc, cfg.Name, cfg.Once, logger)
	if err != nil {
		return collection.WorklistReply{}, err
	}
	if !ok {
		// Stopped before the server answered: there is nothing to run.
		return collection.WorklistReply{}, nil
	}
	logger.Info("worklist pulled", "facility", "collection", "tasks", len(wl.Tasks))
	// executedCommands remembers each pulled command's outcome across ticks:
	// at-least-once delivery means redelivery, and idempotence per command id
	// lives here (the report repeats, the device is not touched twice). It is
	// pruned past the redelivery window so it does not grow for the life of the run.
	// It also carries whether the last pull failed, so a failing pull is logged
	// on the transition only (#1630).
	executedCommands := newCommandState()
	// report accumulates each endpoint's collection telemetry over a report
	// window (#1369), published when the window is due. Both reports close on
	// one grid the node's name places (#1779), so each window holds one
	// window's worth however late the housekeep tick takes it.
	reportStart, reportSeed := time.Now(), phaseSeed(cfg.Name)
	report := newCollectionReport(cfg.ReportEvery, reportSeed, reportStart)
	// self is the node's account of itself (#1625): its dispatch, schedule,
	// spool, sessions and process, sent as platform metrics in the node-self
	// block of the batch each tick publishes, on the same window.
	self := newNodeReport(cfg.ReportEvery, reportSeed, reportStart)
	if cfg.ReportEvery <= 0 {
		report.setEvery(wl.ReportEvery())
		self.setEvery(wl.ReportEvery())
	}
	self.worklist(len(wl.Tasks), time.Now())
	// Each poll and probe runs on its own interval (#1634): the dispatcher
	// keeps every task's next due and hands what is due to a lane per
	// endpoint, on its own timer; the heartbeat below keeps the node's
	// housekeeping and sets no task's cadence.
	coll := newCollector(spool, cfg.Name, runner, sessions, sched, report)
	d = newDispatcher(dispatchConfig{workers: cfg.LaneWorkers, heartbeat: heartbeat, probeEvery: cfg.ProbeEvery,
		sched: sched, sessions: sessions, report: report, work: coll.run})
	coll.every, coll.wake = d.interval, d.kick
	// Shutdown waits for the runs in flight before the sessions close
	// (deferred calls run last first).
	defer d.settle()
	sessions.reconcile(wl.Tasks)
	d.sync(wl, time.Now())
	d.pass(ctx, time.Now())
	if cfg.Once {
		d.settle()
	}
	runCommands(ctx, nc, cfg.Name, runner, sessions, d.pool, executedCommands, self)
	// A heartbeat the connection cannot take right now (a reconnect in
	// flight, with no reconnect buffer) is retried next tick, as in the loop
	// below; it never ends the node (#1577).
	_ = publishHeartbeat(nc, cfg.Name)
	publishSelf(spool, cfg.Name, sink, self.account(d.takeWindow(), spool, sessions))

	if cfg.Once {
		// A one-shot run ends only once the stream has acked what it
		// collected (the drain flushes the groups still pending first): a
		// batch still held is an error, never a quiet exit.
		dctx, cancel := context.WithTimeout(ctx, onceDrainTimeout)
		defer cancel()
		if err := spool.drain(dctx); err != nil {
			return wl, err
		}
		_ = nc.Flush()
		return wl, nil
	}

	// The server's on-demand runs (#1163) are operator work on the endpoint
	// lanes (#1249): a run now on its endpoint's, a preview on the lane of
	// the endpoint it resolves to (off-lane when none), each answered from
	// the worker that ran it.
	runSub, err := subscribeRuns(ctx, nc, runRoute{out: spool, node: cfg.Name, runner: runner,
		sessions: sessions, sched: sched, report: report, pool: d.pool})
	if err != nil {
		return wl, fmt.Errorf("node: subscribe runs: %w", err)
	}
	defer func() { _ = runSub.Unsubscribe() }()
	tallySub, err := subscribeTally(nc, cfg.Name, tally)
	if err != nil {
		return wl, fmt.Errorf("node: subscribe tally: %w", err)
	}
	defer func() { _ = tallySub.Unsubscribe() }()

	// The worklist is pulled on its own goroutine and interval (#1577); the
	// loop takes the latest one pulled and runs the one it has until then.
	pullEvery := cfg.WorklistEvery
	if pullEvery <= 0 {
		pullEvery = heartbeat
	}
	worklists := make(chan collection.WorklistReply, 1)
	pctx, stopPulls := context.WithCancel(ctx)
	var pulls sync.WaitGroup
	pulls.Go(func() { pullWorklists(pctx, nc, cfg.Name, pullEvery, logger, self, worklists) })
	defer func() { stopPulls(); pulls.Wait() }()

	// Commands are pulled each heartbeat on a goroutine of their own, one
	// pull at a time, so a command waiting for a busy endpoint's lane never
	// holds the loop (#1634); shutdown waits for the pull in flight before
	// the sessions close.
	var commands sync.WaitGroup
	var commandsBusy atomic.Bool
	defer commands.Wait()

	// The run loop (#1636): sessions reconcile the moment a worklist lands
	// and on their own safety-net timer, the dispatcher takes the worklist
	// at once and wakes at its earliest due (#1634), the heartbeat carries
	// only its own work, and session housekeeping runs on a timer at the
	// report window. Every handler runs on the loop's goroutine, the
	// session maps' one writer.
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	reconciler := time.NewTicker(heartbeat)
	defer reconciler.Stop()
	keepEvery := housekeepEvery(heartbeat, report.every())
	housekeeper := time.NewTicker(keepEvery)
	defer housekeeper.Stop()
	// due wakes the loop at the dispatcher's earliest due (#1634); with
	// nothing booked it waits a heartbeat, and a pass ends early whenever a
	// run ends or a gate moves (d.wake).
	due := time.NewTimer(heartbeat)
	defer due.Stop()
	arm := func() {
		wait := heartbeat
		if at, ok := d.next(); ok {
			wait = max(0, time.Until(at))
		}
		due.Reset(wait)
	}
	arm()
	loop := &runLoop{
		worklists: worklists,
		due:       due.C,
		wake:      d.wake,
		heartbeat: ticker.C,
		reconcile: reconciler.C,
		housekeep: housekeeper.C,
		sessions:  sessions,
		onWorklist: func(wl collection.WorklistReply) {
			// The loop has reconciled the sessions; the dispatcher takes the
			// new worklist at once: a new task runs now, one gone stops, the
			// rest keep their dues.
			self.worklist(len(wl.Tasks), time.Now())
			d.sync(wl, time.Now())
			d.pass(ctx, time.Now())
		},
		onDue: func() { d.pass(ctx, time.Now()) },
		onHeartbeat: func(wl collection.WorklistReply) {
			_ = publishHeartbeat(nc, cfg.Name)
			// Ship what the spool held since the last tick, so batches land
			// in the order they were collected (#454).
			spool.retry()
			resendAfterOverflow(spool, coll, sched, sessions)
			if cfg.ReportEvery <= 0 {
				report.setEvery(wl.ReportEvery())
				self.setEvery(wl.ReportEvery())
				if every := housekeepEvery(heartbeat, report.every()); every != keepEvery {
					keepEvery = every
					housekeeper.Reset(every)
				}
			}
			// Pull and actuate the pending command queue (#815).
			if commandsBusy.CompareAndSwap(false, true) {
				commands.Go(func() {
					defer commandsBusy.Store(false)
					runCommands(ctx, nc, cfg.Name, runner, sessions, d.pool, executedCommands, self)
				})
			}
			// The dispatcher's account of the heartbeat window just ended
			// (#1634) is said in the log when it passed a due over, and sent as
			// the node's account of itself (#1625) beside the self-logs.
			w := d.takeWindow()
			logDispatchWindow(logger, w)
			publishSelf(spool, cfg.Name, sink, self.account(w, spool, sessions))
		},
		onHousekeep: func(wl collection.WorklistReply) {
			// Report the inbound lines no listener claimed since the last pass.
			sessions.flushUnclaimed()
			// Count the pushes since the last pass into the collection report,
			// and publish the report when its window is due (#1369). A publish
			// failure is non-fatal: the spool holds the batch and retries it.
			sessions.reportPushes(report)
			if now := time.Now(); report.due(now) {
				for _, row := range report.take(now.UTC(), publishTasks(wl.Tasks)) {
					_ = publishBatch(spool, cfg.Name, row.task, row.samples)
				}
			}
			// Ship the self-logs captured since the last pass; the node's
			// account of itself stays on the heartbeat window (#1625).
			publishSelf(spool, cfg.Name, sink, nil)
		},
		settle: arm,
	}
	wl = loop.run(ctx, wl)

	// Ship what the spool still holds, its pending groups flushed first,
	// before the connection closes, within a bound: the spool is memory, so a
	// batch left in it at exit is gone.
	dctx, cancel := context.WithTimeout(context.Background(), shutdownDrainTimeout)
	defer cancel()
	if err := spool.drain(dctx); err != nil {
		logger.Warn("telemetry left unshipped at shutdown", "facility", "delivery", "error", err.Error())
	}
	_ = nc.Flush()
	return wl, nil
}

// sessionDials is the node's session-dials bound: the configured one, or
// DefaultSessionDials when none is set.
func sessionDials(n int) int {
	if n <= 0 {
		return DefaultSessionDials
	}
	return n
}

// logDispatchWindow self-logs the dispatcher's account of the heartbeat
// window just ended (#1634): the longest pass, the dues that met their run
// still in flight, the dues passed over, the latest dispatch, and the most
// workers busy and dispatches waiting at once, and the operator work offered
// to the lanes against the offers refused at the operator queue's cap
// (#1249). Said only for a window that passed a due over or refused
// operator work, so a node keeping its schedule logs nothing.
func logDispatchWindow(logger *slog.Logger, w dispatchWindow) {
	if w.overruns == 0 && w.skips == 0 && w.operatorRefusals == 0 {
		return
	}
	logger.Warn("dispatch window", "facility", "collection",
		"passes", w.passes, "tick-duration-ms", w.passMax.Milliseconds(),
		"tick-overrun-count", w.overruns, "schedule-skip-count", w.skips,
		"dispatch-late-ms", w.lateMax.Milliseconds(),
		"worker-busy-count", w.busyMax, "dispatch-queue-depth", w.queueMax,
		"operator-offer-count", w.operatorOffers, "operator-refused-count", w.operatorRefusals)
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

// pullWorklist runs the worklist request-reply and decodes the reply. The
// request states the node's budget, so the server drops a pull it reaches only
// after the node stopped waiting (#1577); a reply the server refused is an
// error, never an empty worklist.
func pullWorklist(ctx context.Context, nc *nats.Conn, name string) (collection.WorklistReply, error) {
	rctx, cancel := context.WithTimeout(ctx, worklistTimeout)
	defer cancel()
	req := nats.NewMsg(collection.WorklistSubject(name))
	req.Header.Set(collection.WorklistTimeoutHeader, worklistTimeout.String())
	msg, err := nc.RequestMsgWithContext(rctx, req)
	if err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: pull worklist: %w", err)
	}
	reply, err := collection.DecodeWorklistReply(msg.Header, msg.Data)
	if err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: pull worklist: %w", err)
	}
	return reply, nil
}

// pullBackoff is the wait before the attempt-th retry of a failed pull: the
// base doubled each attempt up to the cap, jittered by rnd (in [0, 1)) over
// its upper half, so a fleet that failed together does not retry together.
func pullBackoff(attempt int, rnd float64) time.Duration {
	d := pullBackoffCap
	if attempt >= 1 && attempt <= 32 {
		d = min(pullBackoffBase<<(attempt-1), pullBackoffCap)
	}
	return d/2 + time.Duration(rnd*float64(d/2))
}

// jitterInterval is every spread by rnd (in [0, 1)) across ten percent either
// side, so nodes started together do not pull together.
func jitterInterval(every time.Duration, rnd float64) time.Duration {
	return time.Duration(float64(every) * (0.9 + 0.2*rnd))
}

// firstWorklist pulls the node's first worklist, retrying a failure on
// backoff until one arrives or ctx ends (false, nil), so a server that is
// still starting, or busy with the rest of the fleet, never ends the node
// (#1577). once tries one pull and returns its error: a one-shot run reports
// its failure rather than waiting on it.
func firstWorklist(ctx context.Context, nc *nats.Conn, name string, once bool, logger *slog.Logger) (collection.WorklistReply, bool, error) {
	for attempt := 1; ; attempt++ {
		wl, err := pullWorklist(ctx, nc, name)
		if err == nil {
			return wl, true, nil
		}
		if once {
			return collection.WorklistReply{}, false, err
		}
		if ctx.Err() != nil {
			return collection.WorklistReply{}, false, nil
		}
		wait := pullBackoff(attempt, rand.Float64())
		logger.Warn("first worklist pull failed, retrying", "facility", "collection",
			"attempt", attempt, "retry_in", wait.String(), "error", err.Error())
		if !sleepCtx(ctx, wait) {
			return collection.WorklistReply{}, false, nil
		}
	}
}

// pullWorklists pulls the node's worklist every interval, jittered, until ctx
// ends, handing each one to out in place of any the loop has not taken. A
// failed pull is logged with the running count and retried on backoff; the
// node runs the worklist it has meanwhile (#1577). Each pull, served or
// refused, is tallied on self toward the window's worklist pull loss (#1868).
func pullWorklists(ctx context.Context, nc *nats.Conn, name string, every time.Duration, logger *slog.Logger, self *nodeReport, out chan collection.WorklistReply) {
	attempt, failures := 0, 0
	for wait := jitterInterval(every, rand.Float64()); sleepCtx(ctx, wait); {
		next, err := pullWorklist(ctx, nc, name)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			self.worklistPulled(true)
			attempt++
			failures++
			wait = pullBackoff(attempt, rand.Float64())
			logger.Warn("worklist pull failed, the node keeps its last worklist", "facility", "collection",
				"attempt", attempt, "retry_in", wait.String(), "failures", failures, "error", err.Error())
			continue
		}
		self.worklistPulled(false)
		attempt = 0
		wait = jitterInterval(every, rand.Float64())
		// Only this goroutine sends, so once a stale worklist is taken back
		// the send cannot block.
		select {
		case <-out:
		default:
		}
		out <- next
	}
}

// resendAfterOverflow answers a spool overflow: a batch dropped at its cap may
// have carried a transition the node remembers as sent, so it sends current
// state again (ADR-0453, point 4). The polls forget what they landed, and their
// next run says it (the property lane's transition guard keeps an unchanged
// value from landing twice); the held sessions, which say a change only when
// it happens, republish their state, auth verdict and listener faults now
// (#1213). Without an overflow nothing is resent.
func resendAfterOverflow(spool *telemetrySpool, coll *collector, sched *pollSchedule, sessions *sessionManager) {
	if !spool.takeOverflow() {
		return
	}
	coll.forgetTransitions()
	sched.forgetOccurrences()
	sessions.resync()
}

// sleepCtx waits d, or until ctx ends, and reports whether d elapsed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// publishSelf drains the node's captured log records and hands them to the
// spool, with the node's self-report samples, as one task-less telemetry
// batch on the node's own subject: the ingest consumer lands the lines in
// node_log (ADR-0066, #589) and the samples on the node's platform component
// (#1625). The spool holds a batch the stream has not acked, so a drained
// line survives a publish outage up to the spool's cap (#454). It
// deliberately does not log its own failure, since emitting from the drain
// path would feed the buffer it just emptied.
func publishSelf(out *telemetrySpool, name string, sink *logSink, self []*ogv1.MetricSample) {
	recs, dropped := sink.drainCounted()
	if dropped > 0 {
		// The lines the buffer dropped are the node hop's loss (#1692): the
		// batch below is the first to carry the count.
		out.lost(delivery.SelfLogOverflow, map[delivery.Kind]int{delivery.Log: dropped})
	}
	ev := selfLogBatch(name, recs)
	if len(self) > 0 {
		if ev == nil {
			ev = &ogv1.TelemetryBatch{NodeId: name}
		}
		ev.NodeSelf = &ogv1.NodeSelf{Metrics: self}
	}
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
