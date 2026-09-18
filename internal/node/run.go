package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
)

// worklistTimeout bounds a single worklist request-reply.
const worklistTimeout = 5 * time.Second

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
	// SSH is the layer-7 ssh probe primitive (#812). Nil defaults to the
	// real collection.NewSSHProber(); tests inject a fake.
	SSH collection.SSHProber
	// SNMP is the driver-poll v2c GET client (#814). Nil defaults to the
	// real collection.NewSNMPGetter(); tests inject a fake.
	SNMP collection.SNMPGetter
	// Line is the driver-poll stateless line-protocol client (#814). Nil
	// defaults to the real collection.NewLineExchanger(); tests inject a fake.
	Line collection.LineExchanger
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
	nc, err := nats.Connect(creds.NatsURL,
		nats.UserInfo(creds.Username, creds.Password),
		nats.CustomInboxPrefix(collection.InboxPrefix(cfg.Name)),
		nats.Name("omniglass-node-"+cfg.Name),
	)
	if err != nil {
		return collection.WorklistReply{}, fmt.Errorf("node: connect bus at %s: %w", creds.NatsURL, err)
	}
	defer nc.Close()
	logger.Info("connected to bus", "facility", "enrollment", "url", creds.NatsURL)

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
	runner := &collection.Runner{TCP: dialer, Ping: pinger, HTTP: httpProber, SSH: sshProber, SNMP: snmpGetter, Line: lineExchanger}

	// verdicts remembers the last reachability verdict per task (keyed by the
	// node-unique task id, since interface names collide across components) across
	// ticks, so the node emits endpoint-reachable only on a flip or first
	// observation (transition-only). It lives for the whole run, not per tick.
	verdicts := map[string]string{}
	// faultseen remembers each driver task's last fault set so a static
	// misconfiguration lands one collection-failed, not one per tick (#814).
	faultseen := map[string]string{}

	wl, err := pullWorklist(nc, cfg.Name)
	if err != nil {
		return collection.WorklistReply{}, err
	}
	logger.Info("worklist pulled", "facility", "collection", "tasks", len(wl.Tasks))
	// executedCommands remembers each pulled command's outcome across ticks:
	// at-least-once delivery means redelivery, and idempotence per command id
	// lives here (the report repeats, the device is not touched twice). It is
	// pruned past the redelivery window so it does not grow for the life of the run.
	executedCommands := map[int64]commandOutcome{}
	if err := runTasks(ctx, nc, cfg.Name, wl, runner, verdicts, faultseen); err != nil {
		return wl, err
	}
	runCommands(ctx, nc, cfg.Name, runner, executedCommands)
	if err := publishHeartbeat(nc, cfg.Name); err != nil {
		return wl, err
	}
	publishSelfLogs(nc, cfg.Name, sink)

	if cfg.Once {
		_ = nc.Flush()
		return wl, nil
	}

	every := cfg.HeartbeatEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = nc.Flush()
			return wl, nil
		case <-ticker.C:
			_ = publishHeartbeat(nc, cfg.Name)
			// Re-pull each tick; a changed config generation drives a refresh in
			// later checkpoints. A pull failure is non-fatal (retry next tick).
			if next, err := pullWorklist(nc, cfg.Name); err == nil {
				wl = next
			}
			// Run the worklist's tcp tasks and publish their telemetry. A publish
			// failure is non-fatal (retry next tick).
			_ = runTasks(ctx, nc, cfg.Name, wl, runner, verdicts, faultseen)
			// Pull and actuate the pending command queue (#815).
			runCommands(ctx, nc, cfg.Name, runner, executedCommands)
			// Ship whatever the node logged this tick as self-logs.
			publishSelfLogs(nc, cfg.Name, sink)
		}
	}
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

// publishSelfLogs drains the node's captured log records and publishes them as a
// logs-only telemetry Event on the node's own subject, where the ingest consumer
// lands them in node_log (ADR-0066, #589). Non-fatal: a marshal or publish
// failure drops this batch and the loop continues. It deliberately does not log its
// own failure, since emitting from the drain path would feed the buffer it just
// emptied; a publish outage is visible as a gap on the lane and in the heartbeat.
func publishSelfLogs(nc *nats.Conn, name string, sink *logSink) {
	ev := selfLogBatch(name, sink.drain())
	if ev == nil {
		return
	}
	b, err := proto.Marshal(ev)
	if err != nil {
		return
	}
	_ = nc.Publish(collection.TelemetrySubject(name), b)
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
