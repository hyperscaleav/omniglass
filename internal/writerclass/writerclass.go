// Package writerclass declares who may write each telemetry key (ADR-0161).
// Every metric, property and event type has exactly one writer class, and each
// class admits a fixed set of writers: the entry points a write comes through.
// The Storage Gateway checks every sample and event write against it, the way it
// injects scope on every query, so no route can forget to.
//
// Three classes form the reserved system family, fixed and internal: node-probe
// (the keys a node's own probes and session machine write, declared in
// probekeys), platform (keys only the Omniglass process itself writes: the
// health rollup's verdict, the webhook receiver's counts, and its
// self-monitoring, a node's self-report among it) and
// command-lifecycle (the events a command's own lifecycle writes:
// command-issued, and its terminal command-settled, command-failed or
// command-timed-out). Their names are declared in code and reserved against
// operator-created types. The other two are resolved from the catalogs: calc (a
// calc rule names the key as its output) and collected (every other key: a
// driver's emits on the node lane, a push, the observers fusion reconciles by
// source).
//
// A leaf: storage, the push route, the node lane and the driver spec all read
// this one ruling.
package writerclass

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// Class is a key's writer class.
type Class string

// The writer classes. NodeProbe, Platform and CommandLifecycle are the
// reserved system family.
const (
	NodeProbe        Class = "node-probe"
	Platform         Class = "platform"
	CommandLifecycle Class = "command-lifecycle"
	Calc             Class = "calc"
	Collected        Class = "collected"
)

// Classes is every writer class, the reserved family first.
func Classes() []Class { return []Class{NodeProbe, Platform, CommandLifecycle, Calc, Collected} }

// Reserved reports a class of the reserved system family: fixed, internal, its
// keys declared in code and their names closed to operator-created types.
func (c Class) Reserved() bool { return c == NodeProbe || c == Platform || c == CommandLifecycle }

// Writer is the entry point a write comes through, which the gateway matches
// against the key's class. Each writer fixes the provenance it writes.
type Writer string

// The writers.
const (
	// Node is the node lane: a node's own collection (its probes, its session
	// machine, the drivers it runs), observed, bound to the endpoint its task
	// ran against.
	Node Writer = "node"
	// Push is the telemetry push lane, observed.
	Push Writer = "push"
	// Fanout is a conduit's value fanned out to the owner an identity
	// binding names (#1282, ADR-1282), on the node lane or the webhook lane,
	// observed. It speaks only for a collected key, held to an owner the
	// conduit's bindings name.
	Fanout Writer = "fan-out"
	// CalcRule is a calc rule's tick writing its own output, calculated.
	CalcRule Writer = "calc-rule"
	// Process is the Omniglass process itself (the server, later a node of
	// itself): the health rollup today, calculated.
	Process Writer = "process"
	// Command is a command issue: its intended value and its caused event.
	Command Writer = "command"
	// CommandReply is a command's reply read through its driver binding, an
	// observation of the command's target.
	CommandReply Writer = "command-reply"
	// Operator is an operator's declared value.
	Operator Writer = "operator"
	// KeyPin is an operator's key pin (an ssh host key, a tls certificate's
	// public key), declared through an endpoint's audited :retry.
	KeyPin Writer = "key-pin"
	// EventRule is an event rule's fire or clear event.
	EventRule Writer = "event-rule"
)

// Writers is every writer.
func Writers() []Writer {
	return []Writer{Node, Push, Fanout, CalcRule, Process, Command, CommandReply, Operator, KeyPin, EventRule}
}

// Health is the platform key the health rollup writes: the rolled-up verdict
// of a component, system or location.
const Health = "health"

// The webhook receiver's counts (#1369): an endpoint's webhook posts the
// server accepted, and those it refused, over a report window. The server
// counts them, so they are the process's to write, never a node's or a
// push's.
const (
	WebhookPostCount    = "webhook-post-count"
	WebhookRefusalCount = "webhook-refusal-count"
)

// The platform's self-monitoring keys (#1094, #1625), with no prefix: the
// platform class reserves them by name, as it does health. CheckinAge is the
// server's reading of a node's last check-in; the rest a node reports of
// itself, on its own platform component. The process-* keys are generic: any
// Omniglass process reports them of itself.
const (
	CheckinAge            = "checkin-age"
	WorklistTaskCount     = "worklist-task-count"
	TickDuration          = "tick-duration"
	TickOverrunCount      = "tick-overrun-count"
	ScheduleLateness      = "schedule-lateness"
	ScheduleGapExcess     = "schedule-gap-excess"
	ScheduleSkipCount     = "schedule-skip-count"
	SpoolBatchCount       = "spool-batch-count"
	SpoolDropCount        = "spool-drop-count"
	PublishFailureCount   = "publish-failure-count"
	SessionHeldCount      = "session-held-count"
	ProcessGoroutineCount = "process-goroutine-count"
	ProcessHeapSize       = "process-heap-size"
	ProcessGCPause        = "process-gc-pause"
	ProcessCPUSeconds     = "process-cpu-seconds"
	ProcessFDCount        = "process-fd-count"
)

// The server's request and database keys (#1627): what the server's HTTP
// surface served and how its database pool held up, over a report window,
// landed by the server on its own platform component. An http-request-* key
// is per instance, the request's method, route template and status class; the
// db-* keys have no instance.
const (
	HTTPRequestCount       = "http-request-count"
	HTTPRequestDuration    = "http-request-duration"
	HTTPRequestDurationMax = "http-request-duration-max"
	DBSlowQueryCount       = "db-slow-query-count"
	DBPoolAcquired         = "db-pool-acquired"
	DBPoolIdle             = "db-pool-idle"
	DBPoolTotal            = "db-pool-total"
	DBPoolMax              = "db-pool-max"
	DBPoolWaitCount        = "db-pool-wait-count"
	DBPoolWaitDuration     = "db-pool-wait-duration"
)

// The server's pipeline keys (#1628): how far behind ingest is, how deep the
// telemetry consumer, the calc queue and the notification outbox stand, and
// whether the bus request-reply paths and the sweeps are failing, over a
// report window, landed by the server on its own platform component. Every
// failure count has the count it is a share of: a drop or an ack failure of
// ingest-batch-count, a request failure of bus-request-count, a sweep failure
// of sweep-count, so each loss reads as a ratio where zero is healthy.
const (
	IngestLag                   = "ingest-lag"
	IngestLagMax                = "ingest-lag-max"
	IngestBatchCount            = "ingest-batch-count"
	IngestSampleCount           = "ingest-sample-count"
	IngestLaneBusy              = "ingest-lane-busy"
	BusConsumerPending          = "bus-consumer-pending"
	BusConsumerAckPending       = "bus-consumer-ack-pending"
	BusConsumerRedelivered      = "bus-consumer-redelivered"
	BusTelemetryAckFailureCount = "bus-telemetry-ack-failure-count"
	BusTelemetryDropCount       = "bus-telemetry-drop-count"
	BusWorklistPullCount        = "bus-worklist-pull-count"
	BusWorklistReplyBytesMax    = "bus-worklist-reply-bytes-max"
	BusRequestCount             = "bus-request-count"
	BusRequestFailureCount      = "bus-request-failure-count"
	CalcPendingDepth            = "calc-pending-depth"
	CalcServedCount             = "calc-served-count"
	CalcPassDuration            = "calc-pass-duration"
	OutboxQueuedDepth           = "outbox-queued-depth"
	OutboxDueAge                = "outbox-due-age"
	SweepDuration               = "sweep-duration"
	SweepCount                  = "sweep-count"
	SweepFailureCount           = "sweep-failure-count"
)

// serverPipelineWriter is what writes the server's pipeline keys.
const serverPipelineWriter = "the server's pipeline recorder"

// serverSelfWriter is what writes the server's request and database keys.
const serverSelfWriter = "the server's request and database recorder"

// A node's command queue (#1626): what waits for the node, how long the
// oldest has waited, each command's issue-to-dispatch and dispatch-to-execute
// latency, its redeliveries, the commands stuck dispatched and those that
// aged out unexecuted. The server reads them off the command rows; a node
// counts only the pulls of its queue that failed.
const (
	CommandPendingCount     = "command-pending-count"
	CommandPendingAge       = "command-pending-age"
	CommandDispatchLatency  = "command-dispatch-latency"
	CommandExecutionLatency = "command-execution-latency"
	CommandRedeliveryCount  = "command-redelivery-count"
	CommandStuckCount       = "command-stuck-count"
	CommandExpiredCount     = "command-expired-count"
	CommandPullFailureCount = "command-pull-failure-count"
)

// The instances of command-expired-count: a command that aged out before any
// node took it, and one taken but never reported executed.
const (
	CommandExpiredUndelivered = "undelivered"
	CommandExpiredUnexecuted  = "unexecuted"
)

// The server's identity (#1644): the build a server runs and when it last
// started, two properties its boot writes on its own platform component, so
// Platform health can say which Omniglass is running and for how long.
const (
	ServerVersion   = "server-version"
	ServerStartedAt = "server-started-at"
)

// serverBootWriter is what writes the server's identity properties.
const serverBootWriter = "the server boot"

// nodeSelfWriter is what writes a node's self-report: the node, of itself,
// landed by the process on the node's platform component.
const nodeSelfWriter = "a node, of itself (its node-self report)"

// processSelfWriter is what writes the generic process-* keys: any Omniglass
// process, of itself. A node reports them on its node-self lane, so they are
// node-self keys too; the server lands them on its own component.
const processSelfWriter = "a process, of itself (a node on its node-self report, the server on its own component)"

// platform maps each platform key to the part of the process that writes it.
// A key only the Omniglass process writes about itself joins here with its
// writer.
var platform = map[string]string{
	Health:                "the health rollup",
	WebhookPostCount:      "the webhook receiver",
	WebhookRefusalCount:   "the webhook receiver",
	CheckinAge:            "the server's check-in timer",
	WorklistTaskCount:     nodeSelfWriter,
	TickDuration:          nodeSelfWriter,
	TickOverrunCount:      nodeSelfWriter,
	ScheduleLateness:      nodeSelfWriter,
	ScheduleGapExcess:     nodeSelfWriter,
	ScheduleSkipCount:     nodeSelfWriter,
	SpoolBatchCount:       nodeSelfWriter,
	SpoolDropCount:        nodeSelfWriter,
	PublishFailureCount:   nodeSelfWriter,
	SessionHeldCount:      nodeSelfWriter,
	ProcessGoroutineCount: processSelfWriter,
	ProcessHeapSize:       processSelfWriter,
	ProcessGCPause:        processSelfWriter,
	ProcessCPUSeconds:     processSelfWriter,
	ProcessFDCount:        processSelfWriter,

	// The server, of itself (#1627).
	HTTPRequestCount:       serverSelfWriter,
	HTTPRequestDuration:    serverSelfWriter,
	HTTPRequestDurationMax: serverSelfWriter,
	DBSlowQueryCount:       serverSelfWriter,
	DBPoolAcquired:         serverSelfWriter,
	DBPoolIdle:             serverSelfWriter,
	DBPoolTotal:            serverSelfWriter,
	DBPoolMax:              serverSelfWriter,
	DBPoolWaitCount:        serverSelfWriter,
	DBPoolWaitDuration:     serverSelfWriter,

	// The server's pipeline (#1628).
	IngestLag:                   serverPipelineWriter,
	IngestLagMax:                serverPipelineWriter,
	IngestBatchCount:            serverPipelineWriter,
	IngestSampleCount:           serverPipelineWriter,
	IngestLaneBusy:              serverPipelineWriter,
	BusConsumerPending:          serverPipelineWriter,
	BusConsumerAckPending:       serverPipelineWriter,
	BusConsumerRedelivered:      serverPipelineWriter,
	BusTelemetryAckFailureCount: serverPipelineWriter,
	BusTelemetryDropCount:       serverPipelineWriter,
	BusWorklistPullCount:        serverPipelineWriter,
	BusWorklistReplyBytesMax:    serverPipelineWriter,
	BusRequestCount:             serverPipelineWriter,
	BusRequestFailureCount:      serverPipelineWriter,
	CalcPendingDepth:            serverPipelineWriter,
	CalcServedCount:             serverPipelineWriter,
	CalcPassDuration:            serverPipelineWriter,
	OutboxQueuedDepth:           serverPipelineWriter,
	OutboxDueAge:                serverPipelineWriter,
	SweepDuration:               serverPipelineWriter,
	SweepCount:                  serverPipelineWriter,
	SweepFailureCount:           serverPipelineWriter,
	// The server's identity (#1644).
	ServerVersion:   serverBootWriter,
	ServerStartedAt: serverBootWriter,

	// A node's command queue (#1626).
	CommandPendingCount:     "the server's command queue timer",
	CommandPendingAge:       "the server's command queue timer",
	CommandDispatchLatency:  "the command wire, at a command's first dispatch",
	CommandExecutionLatency: "the command wire, at a command's execution report",
	CommandRedeliveryCount:  "the server's command queue timer",
	CommandStuckCount:       "the server's command queue timer",
	CommandExpiredCount:     "the server's command queue timer",
	CommandPullFailureCount: nodeSelfWriter,
}

// NodeSelf reports a platform key a node may report of itself on the
// node-self lane (#1625), exactly (the wire carries the declared name). A
// node's self-report never speaks for another writer's key: a node naming
// health or checkin-age on the lane is refused like any undeclared key.
func NodeSelf(key string) bool { return nodeSelf(platform[key]) }

// nodeSelf reports a writer whose keys a node reports of itself.
func nodeSelf(by string) bool { return by == nodeSelfWriter || by == processSelfWriter }

// NodeSelfKeys is every key a node reports of itself, sorted.
func NodeSelfKeys() []string {
	var out []string
	for k, by := range platform {
		if nodeSelf(by) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// CommandIssued is the event a command's issue lands on its owner's timeline,
// the event its intended value is caused by.
const CommandIssued = "command-issued"

// The events a command's terminal status lands on its owner's timeline
// (#1155), one per recorded outcome, so a failure reads by its type rather
// than out of a message.
const (
	CommandSettled  = "command-settled"
	CommandFailed   = "command-failed"
	CommandTimedOut = "command-timed-out"
)

// commandLifecycle maps each event of a command's lifecycle to what it says.
// Only a command writes one: a push, a node, a driver or an event rule landing
// one would put a command on the timeline that nobody issued, or settle one
// nobody's evidence decided.
var commandLifecycle = map[string]string{
	CommandIssued:   "a command's issue, on the timeline of the owner it was issued to",
	CommandSettled:  "a command settling, from its reply or an observation of its target",
	CommandFailed:   "a command failing: its execution failed, or its evidence contradicted its intent",
	CommandTimedOut: "a command timing out: its settle window closed with no evidence",
}

// CommandOutcomeEvent is the lifecycle event a command's terminal status
// writes, and false for a status that is not terminal.
func CommandOutcomeEvent(status string) (string, bool) {
	switch status {
	case "settled":
		return CommandSettled, true
	case "failed":
		return CommandFailed, true
	case "timed-out":
		return CommandTimedOut, true
	}
	return "", false
}

// pinnable are the node-probe keys an operator may also declare, each with its
// path: a declared row is never the node's observation (the reads serve only
// observed rows), and this one is a control input the worklist hands back.
var pinnable = map[string]string{
	probekeys.SSHHostKey:     "an operator's pin, set with an ssh endpoint's :retry (ADR-0157)",
	probekeys.TLSCertificate: "an operator's pin, set with a tls endpoint's :retry (ADR-0160)",
}

// Pinnable reports a node-probe key an operator may pin through :retry: the
// key a transport's key pin rides.
func Pinnable(key string) bool { return pinnable[key] != "" }

// admits is the whole ruling: the writers each class admits.
//
//   - node-probe: the node lane alone (and KeyPin on a pinnable key): a
//     push, a calc rule, a command, an operator or an event rule never writes
//     what the console reads as the node's own observation.
//   - platform: the process alone.
//   - command-lifecycle: a command alone, its issue landing command-issued
//     and its terminal status the outcome event.
//   - calc: the rule that outputs it, and an operator's declared value, which
//     resolves ahead of the calculated one by design (properties page).
//   - collected: every observer (the node lane carrying a driver's emits, a
//     push, a conduit's value fanned out to its bound owner, a command's
//     reply), a command's intended value, an operator's
//     declared value, an event rule's event. A calc rule never: its output is a
//     key it owns, so a collected key is never one.
var admits = map[Class][]Writer{
	NodeProbe:        {Node},
	Platform:         {Process},
	CommandLifecycle: {Command},
	Calc:             {CalcRule, Operator},
	Collected:        {Node, Push, Fanout, Command, CommandReply, Operator, EventRule},
}

// Admits reports whether w may write key, whose class is c.
func Admits(c Class, w Writer, key string) bool {
	if slices.Contains(admits[c], w) {
		return true
	}
	return c == NodeProbe && w == KeyPin && pinnable[key] != ""
}

// Reserved returns the reserved class a key's name holds, if any. It matches
// case-insensitively and ignores surrounding space, failing closed the way the
// probekeys ruling does: a variant is refused as reserved should a lookup that
// would refuse it as unregistered ever loosen.
func Reserved(name string) (Class, bool) {
	if probekeys.IsNodeOnly(name) {
		return NodeProbe, true
	}
	if _, ok := platformKey(name); ok {
		return Platform, true
	}
	if _, ok := lifecycleKey(name); ok {
		return CommandLifecycle, true
	}
	return "", false
}

// lifecycleKey resolves name to its command lifecycle event,
// case-insensitively.
func lifecycleKey(name string) (string, bool) {
	name = strings.TrimSpace(name)
	for k := range commandLifecycle {
		if strings.EqualFold(k, name) {
			return k, true
		}
	}
	return "", false
}

// platformKey resolves name to its platform key, case-insensitively.
func platformKey(name string) (string, bool) {
	name = strings.TrimSpace(name)
	for k := range platform {
		if strings.EqualFold(k, name) {
			return k, true
		}
	}
	return "", false
}

// ReservedNames is every reserved key, every reserved class, sorted, a fresh
// slice.
func ReservedNames() []string {
	out := probekeys.NodeOnly()
	for k := range platform {
		out = append(out, k)
	}
	for k := range commandLifecycle {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Of resolves a key's class: its reserved class by name, else calc when a calc
// rule outputs it (calcOwned, read from the catalog by the caller), else
// collected.
func Of(name string, calcOwned bool) Class {
	if c, ok := Reserved(name); ok {
		return c
	}
	if calcOwned {
		return Calc
	}
	return Collected
}

// Refusal is the reason a write of key by w is refused, naming the key and the
// writer that owns it. rule names the calc rule a calc key belongs to.
func Refusal(c Class, key string, w Writer, rule string) string {
	switch c {
	case NodeProbe:
		if w == Node {
			return key + " is a native probe key: a node writes it only under the endpoint whose task it ran"
		}
		return key + " is a native probe key, a node's own observation: only the node writes it"
	case Platform:
		by := "the Omniglass process"
		if k, ok := platformKey(key); ok {
			by = platform[k]
		}
		return key + " is a platform key: only " + by + " writes it"
	case CommandLifecycle:
		return key + " is a command's lifecycle event: only a command's issue writes it"
	case Calc:
		if rule == "" {
			return key + " is written by a calc rule: only that rule writes it"
		}
		return key + " is written by the calc rule " + rule + ": only that rule writes it"
	default:
		return key + " is a collected key: a " + string(w) + " does not write it"
	}
}

// classDoc is each class in one line, for the docs render.
var classDoc = map[Class]string{
	NodeProbe:        "what a node's own probes and session machine write: the node's account of an endpoint",
	Platform:         "what only the Omniglass process writes about itself or its rollups",
	CommandLifecycle: "the events a command's own lifecycle writes on its owner's timeline",
	Calc:             "a key a calc rule names as its output",
	Collected:        "every other key: what a driver's emits, a push, a command or an operator say",
}

// writerDoc is each writer in one line, for the docs render.
var writerDoc = map[Writer]string{
	Node:         "the node lane, under the endpoint whose task the node ran",
	Push:         "the telemetry push lane",
	Fanout:       "a conduit's value fanned out to the owner its identity binding names (ADR-1282)",
	CalcRule:     "the calc rule's tick",
	Process:      "the Omniglass process: the health rollup, the webhook receiver, and the platform's self-monitoring, a node's self-report landed on its platform component",
	Command:      "a command's intended value and caused event",
	CommandReply: "a command's reply, read through its driver binding",
	Operator:     "an operator's declared value",
	KeyPin:       "an operator's key pin (an ssh host key, a tls certificate's public key), through an endpoint's :retry",
	EventRule:    "an event rule's fire or clear event",
}

// ClassFact is one class as data: what it holds, and who may write it.
type ClassFact struct {
	Class    Class        `json:"class"`
	Reserved bool         `json:"reserved"`
	Doc      string       `json:"doc"`
	Keys     []string     `json:"keys,omitempty"`
	Admits   []WriterFact `json:"admits"`
	Pins     []PinFact    `json:"pins,omitempty"`
}

// PinFact is the one declared row a node-probe key admits: the key, the
// writer that may pin it, and its path.
type PinFact struct {
	Key    string `json:"key"`
	Writer Writer `json:"writer"`
	Doc    string `json:"doc"`
}

// WriterFact is one writer as data.
type WriterFact struct {
	Writer Writer `json:"writer"`
	Doc    string `json:"doc"`
}

// Facts is the whole ruling as data, in class order: the docs render it
// (cmd/writerclassgen writes docs/src/generated/writerclass.json) so the page
// that teaches the ruling cannot drift from the one the gateway enforces.
func Facts() []ClassFact {
	out := make([]ClassFact, 0, len(Classes()))
	for _, c := range Classes() {
		f := ClassFact{Class: c, Reserved: c.Reserved(), Doc: classDoc[c]}
		switch c {
		case NodeProbe:
			f.Keys = probekeys.NodeOnly()
			for _, k := range slices.Sorted(maps.Keys(pinnable)) {
				f.Pins = append(f.Pins, PinFact{Key: k, Writer: KeyPin, Doc: pinnable[k]})
			}
		case Platform:
			for k := range platform {
				f.Keys = append(f.Keys, k)
			}
			slices.Sort(f.Keys)
		case CommandLifecycle:
			for k := range commandLifecycle {
				f.Keys = append(f.Keys, k)
			}
			slices.Sort(f.Keys)
		}
		for _, w := range admits[c] {
			f.Admits = append(f.Admits, WriterFact{Writer: w, Doc: writerDoc[w]})
		}
		out = append(out, f)
	}
	return out
}

// FactsJSON is Facts as the indented JSON the docs read, newline-terminated.
func FactsJSON() ([]byte, error) {
	b, err := json.MarshalIndent(Facts(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
