---
title: Nodes
description: How the edge runtime pulls its worklist, runs tasks and commands, manages sessions, gates reachability, and ships telemetry.
sidebar:
  badge:
    text: Partial
    variant: note
---

A node is the edge runtime that collects from and controls gear wherever it sits: it pulls its worklist from the server, runs it on the spot, and ships results back. This page covers worklist pull, placement, tasks and commands, sessions, inbound demux, the endpoint lane, reachability, and shipping telemetry; the declarative shape it executes lives in [templates](/architecture/templates/) and [collection](/architecture/collection/).

:::note[Partial]
Built: `omniglass node run` enrolls (node created server-side, enrollment token minted at
`POST /nodes/{name}:enroll`, exchanged for its NATS credential at `POST /nodes:claim`), connects
outbound-only to the server's in-process NATS server, pulls its worklist over a `og.v1.worklist.<node>`
request-reply (its enabled tasks plus a `config_generation`), and heartbeats on `og.v1.heartbeat.<node>`
(the server stamps `last_heartbeat_at`). **Per-node subject isolation is enforced**: each node's NATS
credential is permitted only its own `<node>` subjects. Also built: running tasks (the probes,
`internal/node/probe.go`), shipping telemetry (the JetStream `TelemetryBatch` ingested by
`internal/bus/consumer.go`, each publish waiting for the stream's ack under a minted `Nats-Msg-Id`, a
batch the stream has not acked held in a bounded spool and shipped on the next tick, one batch per
tick carrying a group per task,
[below](#delivery-a-batch-lands-once); a group whose task is not placed on the node is dropped by
the confinement fence, warned about once per node per minute and counted on the node's
`orphaned_batches` and `last_orphaned_at`, #1200), and the raw self-log lane (ADR-0066: `internal/node/logs.go`,
`GET /nodes/{name}/logs`, the console Self-logs panel), the command wire (a per-node pull, actuated
over the driver's binding, [ADR-0136](/architecture/decisions/#adr-0136-actuation-is-a-per-node-pull-rendered-at-dispatch)),
driver polls on their declared schedule (#822), each poll and probe on its own interval from a
dispatcher of its own, through one lane per endpoint on a bounded worker pool (#1634,
[below](#scheduling-concurrency-and-self-observability)), which an operator's commands, run nows and
previews take too, ahead of the scheduled work and each consulting the endpoint's gate as it is
taken (#1249, [below](#the-endpoint-lane)), and, for the line protocol over `tcp`, `tls` (#1035), `telnet` (#1033) and `ssh` (#1032), held sessions
(#816, [below](#sessions)) with their listeners and the inbound demux (#817), and the same line
protocol as datagrams over `udp` (#1037), stateless, each endpoint on its own bound socket
([udp](/architecture/collection/#udp)), and http polls and commands over `http` and `https`
(#902), stateless, each its own request with the endpoint's declared credential, the https pin shared
with the endpoint's probe ([http and https](/architecture/collection/#http-and-https)); an http
endpoint whose driver logs in (#1114) has one auth state on its node (the cookie jar, the login's
captured values, a digest challenge), shared by its polls and commands, held in memory alone and
dropped when its fingerprint moves, on a terminal refusal, and on restart
([logging in over http](/architecture/collection/#logging-in-over-http)); and an http endpoint's
server-sent events stream (#1038), held as a session sharing that state, its events routed to the
listeners as a line is, resumed from its last event id after a drop
([a server-sent events stream](/architecture/collection/#a-server-sent-events-stream)); and a
websocket or wss endpoint's socket (#1034), held as a session whose upgrade rides that state, its
frames routed as a line is, a poll's or a command's answer matched by the id the node minted into it,
kept alive by ping and pong ([websocket and wss](/architecture/collection/#websocket-and-wss)). Every
held session reconciles the moment its worklist lands, runs supervised (a panic costs that one
session, never the node), and makes its first dial through node-wide slots (`--session-dials`, #1636,
[below](#sessions)). Also built: a
node serving its own simulated components, `omniglass node run --simulate` (#1198,
[below](#serving-its-own-simulated-components)). Also built: the node's account of itself (#1625,
[below](#self-monitoring-the-node-reports-itself)), its schedule, spool, session and process figures
landed each tick on its own platform component, and the server's check-in age for every claimed node;
its own losses on every batch's ledger, read as the node hop of the loss table (#1692,
[below](#delivery-a-batch-lands-once));
and its command queue's health (#1626, [below](#self-monitoring-the-node-reports-itself)): depth,
oldest age, latencies, redeliveries, stuck and expired commands, and the node's failed pulls; and
a silent node's alarm, the seeded `node-silent` rule over its `checkin-age` (#1707,
[ADR-1707](/architecture/decisions/#adr-1707-a-silent-node-alarms-through-a-seeded-rule-over-its-checkin-age-not-a-node-liveness-sweep)).
Still `Design`: the
durable JetStream command queue, the session pool knobs and the held transports beyond these
(#1036 (mqtt), #1039 (modbus/tcp), #1040 (bacnet/ip), #1041 (knxnet/ip), [ADR-0153](/architecture/decisions/#adr-0153-the-held-session-proof-is-the-line-protocol-over-tcp-other-held-transports-are-their-own-issues)),
the
cascade-resolved buffer knobs, a probe concurrency apart from the worker pool, a per-task
deadline, and the last of the per-kind model that replaced the heartbeat tick, the heartbeat on its
own and a command run the moment it is issued (#1635, [ADR-1637](/architecture/decisions/#adr-1637-a-node-runs-each-kind-of-work-on-its-own-timer-and-pool-one-request-per-endpoint-at-a-time)). The credential is a shared
secret (the enrollment token doubles as the NATS password); the decentralized nkey/JWT model is
deferred. See [implementation status](/architecture/status/) and
[decision log](/architecture/decisions/) (ADR-0036).
:::

## The node

The node is the edge process (`omniglass node run`), one per site, or the **server itself** when no site-local edge exists (see *Placement*). Identity is **bound to `node.name`** (a compromised node cannot impersonate another, [identity-access](/architecture/identity-access/)); it holds no config, and its writes are confined to its **placement-derived `visible_set`** (the owners of its assigned tasks): **node mode**, not all-visibility system mode.

A node carries the identity triad with one exception. Its **`id`** is `principal_id`, the immutable primary key every reference stores (a node is the detail row of a principal, and `endpoint.node_name` holds that uuid whatever its column name suggests). Its **`name`** is the operator-typed identifier and fleet address, the NATS subject token and the enrollment identity, and it is the one name with no `:rename` custom method: moving it would move a live subject, so the PATCH body carries no name at all. Its **`label`** is the operator label (console falls back to the name). Its optional `location` is **descriptive**, **not** a scope: a node stays fleet-wide, `location` clearing if that location is deleted (`ON DELETE SET NULL`). Descriptive does not mean unguarded: the create and the update resolve that reference within the caller's own `location:read` scope, the seam every other placement bind uses, and a location outside it is refused as **absent** rather than as forbidden ([ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup), amended by [#705](https://github.com/hyperscaleav/omniglass/issues/705)). The console blade is read-edit-save via `PATCH /nodes/{name}` (Edit primary, gated `node:update`, editing label, description, location; name read-only). Creating a node does not enroll it, matching the API (`POST /nodes` needs only `node:create`; `POST /nodes/{name}:enroll` needs `node:enroll`): the create form registers the row, opens its blade and toasts, and the enrollment token is minted only when the operator presses Enroll there, the blade's primary action while the node has never been enrolled (Re-enroll moves to the secondary kebab once it has).

A node is a **taggable owner**: governed [tags](/architecture/tags/) whose `applies_to` includes `node` bind to it (fleet-wide, all-scope, `node:update`); effective tags are the `platform` layer plus direct bindings, no cascade (a node is not a scope tree); the blade carries a **Tags** panel, the list a Tags column and per-key facet. **Decommissioning** (`DELETE /nodes/{name}`, `node:delete`) hard-deletes, cascading its endpoints, their derived tasks, its node-owned tags and self-telemetry, its platform component ([below](#self-monitoring-the-node-reports-itself)), and its enrollment credential; collected component telemetry is untouched.

### Serving its own simulated components

A node can run the [simulator](/architecture/simulation/#in-the-nodes-own-process) in its own
process: `omniglass node run --simulate` serves every component placed on the node whose product a
profile simulates, each on its own `127.60.0.x` address on the node's loopback, and collects from them
as it would from hardware. The simulator holds the node's own identity (its name and enrollment
token), so there is no second enrollment and no grant beyond the node's own subjects
([ADR-0142](/architecture/decisions/#adr-0142-the-simulator-is-a-run-mode-beside-its-node-holding-that-nodes-identity)).
`--impairments`, `--port-offset` and `--poll` belong to the simulator and are refused without
`--simulate`; `--impairments` needs `NET_ADMIN`, so it belongs in a container of the node's own. The
[lab](/guides/lab/) runs its node `edge-hq` this way.

## Getting its instructions

The node pulls a **worklist** (the tasks and commands resolved for the components **placed on it**) over a NATS request-reply config pull, and **heartbeats** separately on its own subject ([the protocol](#the-node-server-protocol)), so liveness tracks independently of the pull.

Built: the server materializes every task before the node sees it. A driver task carries one baked
function (its request, schedule and emits, each emit's lane baked at attach), the driver's `session`
block and resolved ladder, its inputs defaulted at attach, and the secrets that cascade onto its
component ([ADR-0135](/architecture/decisions/#adr-0135-a-drivers-spec-is-data-one-engine-interprets-it)); the node never
sees a driver spec, only resolved task and command instances.

:::design[Target design: `$var:` values and placement in the worklist, tracked in #296]
The server, not the driver spec, decides placement (next; today an endpoint names its node) and
resolves the rest of the cascade (`$var:` values, an effective `interval` overriding the driver's
schedule) before handing the node concrete work. The node never sees a driver spec, only
materialized, resolved task and command instances.
:::

### Config propagation (declared change to running node)

:::note[Built: a declared change reaches a running node on its next pull]
The node keeps no endpoint config apart from its worklist: it **re-pulls the whole worklist on its
own interval** (`--worklist-every`, [below](#pulling-the-worklist)) and runs what the latest pull
carried, so a change the server has written is running within one pull interval (the pull
carries it, and the node reconciles its held sessions the moment it lands, #1636, and hands the
new worklist to its dispatcher at once, a new or moved task running then,
[below](#scheduling-concurrency-and-self-observability)), with no restart and nothing to apply.
The server side is the re-derive: an endpoint edit
(`PATCH /endpoints/{endpoint}`, its driver inputs included) and a driver spec change (#823) rewrite
the affected tasks in the same transaction, and a held session whose
[connection fingerprint](/architecture/collection/#the-connection-fingerprint-and-the-layered-gate)
moved is recycled on the pull that carries it. The worklist reply also carries a per-node
**`config_generation`**, the max `updated_at` across the endpoints placed on the node (epoch seconds,
`0` when none). It moves at **operator-config pace, not
telemetry pace**: the sample-write path never touches `endpoint.updated_at`. The node does not
gate on it today, since a full pull each tick leaves nothing to invalidate. An earlier design cached
endpoint config on the node and added a `:apply` custom method to re-render it from declared config;
neither was built, and `:apply` retired with the template authoring model
([ADR-0827](/architecture/decisions/#adr-0827-the-template-layer-owns-the-clone-model-and-the-componenttemplate-authoring-model-is-superseded-by-the-driver-spec)).
:::

### Pulling the worklist

:::note[Built (#1577): a pull that fails never stops the node]
The pull runs on its own goroutine and interval, apart from the heartbeat: `node run
--worklist-every` (default 30 s, each wait jittered within 10% so a fleet started together drifts
apart), so a short heartbeat is not a worklist rebuild as often. The lab pulls every 5 s and a
walkthrough every 2 s ([the lab's cadences](/guides/lab/)).

- **A missed pull is retried, never fatal.** The first pull and every later one retry with
  jittered exponential backoff (each wait in the upper half of a step that doubles from 1 s to a
  30 s cap: 0.5 to 1 s first, 15 to 30 s at most), each logged at WARN with its attempt,
  its wait and the error. Until the first pull lands the node does not heartbeat, so it reads as
  not up rather than as up with nothing to do. After it, a failed pull keeps the last good
  worklist, every task and every session of it.
- **The server fails a pull whole, and says why.** A worklist the server cannot read in full (a
  transient storage error), or that is still over the bus's `max_payload` compressed, is answered
  with an `Omniglass-Error` header naming the reason and no body. The node treats it as a failed
  pull, so a database blip never reaches the node as a short worklist (which would tear down the
  sessions of the tasks it dropped).
- **A large reply is compressed.** A reply past 64 KB is gzipped and marked
  `Content-Encoding: gzip`, and the node inflates it. The headquarters node of the 1,000-system
  corporate lab (4,434 tasks) is 3.3 MB of JSON and about 463 KB on the wire, against the bus's
  1 MB limit.
- **The server answers pulls from a bounded pool.** The node states how long it will wait in an
  `Omniglass-Timeout` header (a duration, so the clocks need not agree), and the server's deadline
  for that pull is its arrival plus that budget. One subscription stamps each pull's arrival and
  queues it, and a pool of workers (`bus.Config.WorklistWorkers`, default 8, held under the
  database pool's size less one connection kept for ingest) builds and answers it. A pull that
  waited past its deadline is dropped unread, one whose deadline passes mid-build is abandoned
  unsent, and one that arrives with the queue full (256 pulls waiting) is refused at once as busy,
  so the node fails fast and backs off rather than waiting out its budget; the subscription never
  blocks, so every pull is stamped on arrival. The server counts every outcome
  (`Server.WorklistCounts`: served, read failed, oversize, respond failed, expired, abandoned,
  busy) and logs every one but a served pull at
  WARN, or ERROR for an oversize reply. The telemetry consumer is counted the same way: every
  batch's handler time goes into one tally (`Server.LaneTally`: batches landed, busy time), and the
  server logs the cumulative figures every 30 s at INFO (`msg="ingest lane"`, only when a batch
  landed since the last line). The stream is a work queue with one message in flight, so busy time
  over wall time is the lane's utilisation; `omniglass lab measure` reads it
  ([the lab guide](/guides/lab/#measuring-a-scale-lab)). The server has no metrics endpoint.

Why it is shaped this way, and what it defers (paging, set-based reads, the materialized
worklist): [ADR-1577](/architecture/decisions/#adr-1577-a-nodes-worklist-is-compressed-served-by-a-bounded-responder-and-retried-without-exiting).
:::
## The node-server protocol

The edge is **outbound-only**: a node sits behind NAT, so the server never dials it. A node is a **NATS client over the WAN**, one authenticated outbound connection (an nkey/JWT credential bound to `node.name`); everything server-to-node arrives on subjects the node is permitted to consume. Three flows share the connection:

Telemetry up is built as a JetStream publish the stream acks, under a `Nats-Msg-Id` it
deduplicates ([below](#delivery-a-batch-lands-once)); the subject it lands on is the node's own
`og.v1.telemetry.<node>`, the stream the ingest consumer reads.

:::design[The full node NATS contract, per ADR-0036; the admission split is #430]
- **Telemetry up**: the node **publishes** `TelemetryBatch` batches (one per tick, a `TaskGroup` per
  task, [below](#shipping-samples)) to a **JetStream raw ingress subject**; the
  admission consumer preserves each batch's `Nats-Msg-Id` when republishing to the trusted stream.
- **Control down**: a **durable JetStream consumer** on the node's **command queue**, plus
  **worklist-change signals** (the config-generation bump); subjects scoped by placement.
- **Control up**: heartbeat (the last check-in `checkin-age` counts from), command-execution results (the
  `action`-row status), `session_log` transitions, and the `:report` self-telemetry, each on its
  own subject.

### Commands: a durable server queue, a stateless edge

A command is **issued server-side** (the action layer records it and writes intended state,
[alarms and actions](/architecture/alarms-actions/)) and dispatched onto a **durable server-side
JetStream command queue**. The **edge holds nothing durable**: the node pulls from its durable
consumer (resuming from its last ack on reconnect), runs, and reports the result, updating the
`action` row. A restart loses no command; the held consumer delivers with no poll latency.

**Command results** are an idempotent status update on a known `action` row by id.
:::

### Delivery: a batch lands once

The node publishes **at-least-once**, and the platform lands each batch **once**
([ADR-0453](/architecture/decisions/#adr-0453-telemetry-is-published-through-jetstream-with-an-ack-a-minted-nats-msg-id-and-a-bounded-spool),
[ADR-0311](/architecture/decisions/#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)).
Every telemetry publish (a task's samples, a collection-failed story, a session transition, the
self-logs) goes through one **spool** (`internal/node/spool.go`). A task's result is added to the
spool as a **group**, and the spool **coalesces the groups of a tick into one batch**: it flushes what
is pending after a second, or sooner when the next group would pass the batch's byte budget (512 KiB,
well under the bus's payload limit); a group too large for a batch on its own rides alone. A
session's **pushed** samples flush at once, the groups pending riding with them, so a change an
endpoint reports is not held for the timer. The flush
decides the batch's contents before anything else: it marshals the batch once and mints its
`Nats-Msg-Id` (a UUIDv7) then, so the id and the bytes are fixed from the flush and every retry ships
the same pair. The batch is counted sent only once the stream acks it. The self-logs ride a batch of
their own, after whatever groups are pending, so order is kept. A send only
queues: one shipper goroutine publishes through JetStream, keeping a window of up to 32 batches with
the stream, in order, so no caller (a session's goroutine, the tick) waits on the stream and the node
ships far faster than one batch per round trip. A batch the stream has not acked stays in the spool,
in order, and ships on the **next tick**, under the same id. A retry of an attempt the stream stored
but whose ack was lost is therefore a duplicate: inside the stream's five-minute window it is stored
once, and past it the server's batch **receipt** keeps it from landing twice
([messaging](/architecture/messaging/#delivery-a-batch-lands-once)). After a failed attempt the spool
holds, so a node that cannot reach the bus pays one failed round per tick, not one per batch. A batch
the bus can never take (over its payload limit) is dropped and told rather than held, since held at
the head it would block every batch behind it.

The window stays in order because one connection delivers in the order publishes were made and the
node **buffers nothing across a reconnect**: a publish during an outage fails at once and the spool
holds it. That order matters to the property lane, whose transition guard compares a value with its
series' latest. The node's connection **reconnects without limit**, so an outage of any length ends
with the spool shipping again rather than a connection that gave up.

The spool is **memory**, bounded at 10,000 batches, 16 MiB, or 12 hours: past any of them it drops
the oldest batch (never one with the stream), says so once when it starts dropping and once, with the
count, when it drains, and the run loop forgets the transitions it remembers as sent (the
reachability verdict, a poll's faults and occurrences), so the next tick sends current state again.
The age cap sits inside the receipt's 24-hour horizon, so a stored batch whose ack was lost cannot
come back after its receipt is pruned and land twice. A one-shot run (`--once`) ends only once the
stream has acked what it collected; a stopping node gives the spool five seconds to drain.

Every batch the spool flushes also carries a **delivery ledger** (#1641), stamped under the spool's
lock so it is dense in queue order: the spool's `boot` (a UUIDv7 minted when it starts), a `seq` from
1, the node clock at the flush, the items of each kind the batch carries and the items the boot
minted before it, and `resolved`, the highest seq below which nothing is still queued (the shipper
removes batches from the head only, on an ack or a bus refusal). A self-log batch counts its lines as
kind `log`. The ledger is encoded into the batch's bytes at the flush, so a retry carries it
unchanged, and a batch that never lands leaves a hole in the boot's `seq` that the server's read
counts by kind ([messaging](/architecture/messaging/#accounting-every-item-the-loss-ratio)).

The ledger also carries the **node's own losses** (#1692), so the read can tell the node hop from the
ingest hop. A batch the spool drops (`spool-overflow` at the count or byte cap, `spool-expired` at the
age cap, `bus-refused` when the bus refuses it for good) took a seq, so it leaves a hole; the spool
notes the drop as a **run** (its cause, its first and last seq, its items by kind) and every batch it
mints after carries the run, until the stream acks one of them. A run is then in the stream for good,
and the next batches stop carrying it. Items lost before any batch carried them have no seq at all:
self-log lines the bounded buffer dropped (`self-log-overflow`) and a group or batch that failed to
encode (`encode-failed`). For those the ledger carries a **running total** per cause and kind since
the boot began, so a window reads them as the most it carries less the most a batch before the window
carried. The spool is memory, so a restart mints a new boot: a boot killed with batches still spooled
can never settle, and the read names it **unshipped** (its last landed seq against its next boot's
first) and reads it as far as it landed, but cannot count what it held; #1733 counts it.

### On-demand runs: asked, answered once

The server can ask a node to **run work now** on the node's own run subject (`og.v1.run.<node>`), a
request the node answers once, the simulator operation's shape rather than the command queue's
([ADR-1163](/architecture/decisions/#adr-1163-a-poll-runs-once-on-demand-through-its-node-as-a-preview-that-stores-nothing-or-a-run-now-that-lands)). A run is wanted now or not at all, so nothing is queued: a node that is
not connected is an answer (the API's 503), and one that does not answer within the run's budget
another (504). Both kinds are operator work on the [endpoint lanes](#the-endpoint-lane): the operator
queue is capped, and a run past the cap answers at once that the lanes are full (the API's 429),
never queued without bound. A **preview** touches no held session and publishes nothing, its excerpt
scrubbed of the credential before it is cut; it takes the lane of the inventoried endpoint its
transport and target name, so it never talks to a device beside that endpoint's own work, and runs
off-lane when it names none. A **run now** takes its endpoint's lane ahead of the scheduled work
queued there, so the poll borrows the endpoint's session and honors its gate exactly as a scheduled
run does, and its samples and faults publish as a scheduled run's would, through the same change
memory (an event or a log line that has not changed is not landed twice); the schedule is untouched.
A run the lane could not take within its caller's budget is dropped unrun, since its caller was
already answered. The node's grant
covers its own run subject only, and its one reply stays permitted for the run's whole budget.

### Buffering and retention are cascade settings

When the server is unreachable the node **buffers in memory**, bounded, **not durable at the edge**:
the spool above, its caps (count, bytes, age) fixed in code and a drop told in the node's self-logs.

:::design[Target design: the buffer knobs, tracked in #430; retention is #417]
Both the **buffer** (size, shed policy) and **retention** are **cascade-resolved**
([cascade](/architecture/cascade/)) with an install-wide `platform` binding, overridable down the
tree. (Retention is built as one install-wide window per sample lane, not cascade-resolved:
[ADR-0417](/architecture/decisions/#adr-0417-the-server-prunes-each-sample-lane-to-a-window-in-bounded-chunks).) A full buffer **sheds oldest metrics first and surfaces it** as a
`node.buffer` sample (depth, drops): visible, never silent.
:::

### Credentials at the edge

Built: worklist materialization resolves credentials server-side down the cascade, so **device
secrets travel to the node**, confined to the components placed on it; the node holds them in
memory only and never writes them, and a rotated secret moves the connection fingerprint and
recycles the session ([ADR-0158](/architecture/decisions/#adr-0158-one-connection-fingerprint-gates-every-connection-on-a-ladder-the-driver-declares)).

:::design[Target design: a TLS bus and decrypt-on-use, tracked in #1250]
The secrets travel **over TLS** and are held **decrypt-on-use**: in memory, or encrypted at rest in a scratch dir with
the key from the [`SecretProvider`](/architecture/variables/), **never persisted in plaintext**,
scoped to the node's placement, re-fetched on the config-generation bump.
:::

### Enrollment

A node is **created server-side first** (its `node.name` and properties), the UI mints a **per-node enrollment token**, and the node **claims its identity** on first connect, exchanging the token for its **NATS credential** (a per-node JWT signed for its nkey, scoped to the subjects its placement allows, [identity and access](/architecture/identity-access/)).

:::design[Fleet auto-enrollment over a `discovery_rule`, tracked in #1025]
Later, a **shared enrollment token** plus a **`discovery_rule`** auto-enrolls a fleet: the node's
own properties (stable facts, selected ENV) derive its name, editable server-side after deploy; a
rollout mints no per-node token.
:::

## Placement (ETL, cascaded)

A listener runs where its transport's placement puts it
([ADR-1024](/architecture/decisions/#adr-1024-omniglass-receives-webhooks-on-the-server-and-a-webhook-token-is-a-sealed-secret)):
a webhook endpoint is hosted by the **server**, which a cloud sender can reach over its public
HTTPS and a node behind NAT cannot, so it takes no node and its URL is the server's
(`/api/v1/webhooks/<token>`). The push transports that should not cross a WAN (shared tcp and udp
ports, syslog, SNMP traps) are hosted by the **node** next to the gear (#1148, #1149, #1150), one
listener primitive serving both placements.

:::design[Target design: cascaded placement, the cross pairings tracked in #1157 and #1158]
Collection follows **ETL**: extract **and transform** (including the extractor's Expr transform)
default to the **edge**; the shaped samples **load** to the server, where resolve / bind / calc /
evaluate default to **central**. Placement is a **cascaded property**
([cascade](/architecture/cascade/)): `placement: central` makes the **server itself the node
target** (cloud APIs, SaaS pollers, inbound webhooks). A listener endpoint lives where placement
puts it (the on-site node for LAN devices, the server for cloud sources), so a registered
callback URL resolves to the placed listener's address, never a hardcode.
:::

## Load distribution across a site's nodes

Built today: an endpoint runs on the node its `node_name` names, set by hand, and nothing else. A
site with two nodes splits its components only as an operator splits it, and a lost node's endpoints
wait for someone to re-point them
([ADR-1638](/architecture/decisions/#adr-1638-a-sites-components-spread-across-its-nodes-by-expected-cost-guarded-by-measured-headroom)).

:::design[Target design: a site's components spread across its nodes, tracked in #1638]
**The unit is the component.** Every node-run endpoint of one component runs on one node (#1687). The node's
lanes are per endpoint (#1637), but a component's endpoints share a device, its driver's
component-wide exclusion (#1649) and the endpoint gate's default hold set. Splitting them would put
two sessions on one device and split one gate across two nodes. The system is not the unit: a
component can sit in several systems ([cascade](/architecture/cascade/)), so systems do not
partition a site, and a large system is too lumpy to balance. Systems pull together through
**affinity** instead (below). A push endpoint (a webhook, ADR-1024) is hosted by the server and takes
no node, so it stays outside the unit: it never inherits the component's pin or assignment, it adds
nothing to the component's cost, and its listen tasks never enter a node's worklist.

**The pool is the location.** A component's candidate nodes are the claimed nodes at its location,
or failing that at its nearest ancestor location that has any. A child component with no location of
its own takes its nearest located parent's. A component with no node up its chain stays unassigned
and says why. Once cascaded properties land (#1351), a `node-pool` property
resolved through the cascade can widen, narrow or name a pool at any level, with the location pool
as its default. An endpoint's explicit `node_name` stays the **pin**: it pins the whole component,
and assignment never moves it.

**Every node in a pool reaches every device in it.** This is the pool's one prerequisite: any node
may be assigned any component in its pool, so each must be able to dial each component's address.
Assignment does not check it; the operator builds the pool so it holds.

**Pushes land on any node in the pool.** A device's pushes over a shared port (syslog, SNMP traps,
tcp and udp push; #1148, #1149, #1150) do not follow its component's assignment. Every node in the
pool runs the pool's push listeners and claims each line by its source address, so each node's
worklist carries the pool's push claims beside its assigned tasks, and a device may send to any of
them. A move or a lost node then strands no sender aimed at a live peer. A sender aimed at a node
that is down loses its pushes until it is re-pointed: failover for a single push target is a
virtual IP the operator runs across the pool's nodes, not something omniglass manages. A shared-port
listener is not part of the component's unit and adds nothing to its cost; a listener on a session
a node holds (a line it dialed out) stays with the component's assigned node. The edge is the
default home for these pushes: much of that traffic is unencrypted, and a pool's nodes take it on the
site's LAN rather than over public ingress. The server hosts webhooks and other standard public web
traffic (ADR-1024); server-hosted shared-port listeners (#1158) are the exception, for a sender with
no site node near it.

**Capacity: expected cost balances, measured headroom guards.** The server balances on a
component's **expected cost**, the sum over its tasks of 1 / interval (runs a second). A poll
counts at its own interval and a reachability probe at the default probe interval (#1634). A listen
task has no interval, so a held session or a listener counts at a flagged weight, by default one
30 s poll, and a listen-only component never costs 0. This is pure, known before any node runs the
work, and does not move when the assignment does. Equal component counts are not enough, because a
5 s poll costs six times a 30 s one. A node's **measured headroom** is a guard, not the objective. A
node is **saturated**, and takes no new component, when its schedule loss ratio is above a ceiling
(#1659) or any pool's `worker-busy-fraction` (#1686) is above a ceiling. The loss ceiling is small
but not 0, so one late tick does not close a node. The guard reads only values from the node's
latest report window: a node that ran nothing publishes no loss ratio, and its last one must not
hold it saturated for good. Balancing on the measured figures would chase its own tail: moving load
changes the load it measured.

**Assignment is a server reconcile** (#1688): read state, run a pure `assign`, apply the moves in
one transaction.
- **Pins win.**
- **Sticky:** a component stays on its node while that node is in its pool and unsaturated.
- **Placement:** an unassigned component goes to the least-loaded unsaturated pool node, ties
  broken by fewer components, then by node name.
- **Affinity:** a component goes to a dependency-edge neighbour's node (power, host or path,
  #1648) when that node is unsaturated, which keeps the gate's upstream propagation node-local.
  Affinity is soft: a split pair is still correct, carried by the cross-node relay (#1655), only
  dearer.
- **Rebalance:** moves happen only past a hysteresis factor over the pool mean, and at most a capped
  number per pass.

The assignment lives in its own column beside the pin, so the reconcile never overwrites an
operator's choice. Deleting a node returns its assigned components to the pool; the endpoints it
pins keep today's cascade.

**Rebalance on loss and join** (#1689).
- **Loss.** A node is lost only after the node-silent window (#1094) plus a grace period, so a
  restart, an upgrade or a WAN blip does not reshuffle the site. Its assigned components then spread
  over the unsaturated peers. What the peers cannot take stays unassigned, and the site reads as
  under capacity rather than overloading a peer. Pinned components stay. A lost node that is in
  fact still running, cut off from the server but not from its devices, keeps its last worklist
  (#1577), so a moved component can see two sessions until that node reconnects and drops it. The
  design accepts that over leaving the site unpolled.
- **Join and return.** A joining or returning node starts empty and fills under the per-pass cap,
  so a join is not a stampede of dials. A returning node gets no automatic give-back.
- **Moves between live nodes are two-phase.** The old node's next pull drops the tasks and closes
  its session first. The new node is assigned only once the old node confirms it: its pull request
  names the worklist generation it is running, and a generation at or past the clear confirms it. A
  reply the server sent is not proof, since a pull can time out at the node after the server answers
  and the node then keeps its last worklist (#1577). A move unconfirmed at its deadline is abandoned
  and the component goes back to the old node, so two live nodes never hold a single-session
  device.

**The worklist wire is unchanged** but for one pull request header, the generation the node is
running (the two-phase confirmation above). A move reaches both nodes on their next worklist pull
(30 s, jittered), and the materialized worklist's nudge (#1586) shortens that later. The worklist
generation moves on both nodes on every move, because the max `updated_at` of the remaining
endpoints may not.

**Independent of server replicas.** Assignment maps components to nodes; any server answers any
node's pull. The reconcile is one of scaling.md's singletons: one by construction while there is one
server, then behind the NATS KV lock ([scaling](/architecture/scaling/)).

The baseline it is judged against is two nodes at one site with manual placement, measured
per node by `lab measure` (#1690).
:::

## Running tasks

Built for driver tasks: a poll runs its request on its schedule and a listen task claims pushed lines
over its endpoint's held session, both locating and transforming each emit at the edge and landing a
`collection-failed` for anything that does not parse ([collection](/architecture/collection/)). The
Expr extraction and the cascading labels below remain design. A task runs in one of the two
[collection](/architecture/collection/) modes, **poll** (on its declared schedule) or **listen**; a
held-open connection is a **stateful endpoint transport**, not a third task type, and both modes
assemble the same telemetry payload (below).

:::design[Target design: the Expr transform and cascading labels at the edge, tracked in #1253; raw on failure is #1251]
Per task the node runs the protocol over the endpoint's connection, then **normalizes at the
edge**: the locate + Expr extraction ([collection](/architecture/collection/)) produces samples and
stamps labels (cascading union + override), keeping the wire bytes as `raw` only on a parse or
validation failure (for `collection.failed`) or under dev raw-mode.
:::

Each kind of work runs on its own timer and its own bounded pool rather than inside one heartbeat
loop, so a slow device holds only its own endpoint's lane, never the heartbeat or another endpoint's
work
([ADR-1637](/architecture/decisions/#adr-1637-a-node-runs-each-kind-of-work-on-its-own-timer-and-pool-one-request-per-endpoint-at-a-time)):

| Kind | When it runs | Where it runs |
|---|---|---|
| heartbeat | the heartbeat interval | publishes liveness, pulls its commands, ships the spool, takes the dispatcher's window |
| worklist pull | its own interval ([ADR-1577](/architecture/decisions/#adr-1577-a-nodes-worklist-is-compressed-served-by-a-bounded-responder-and-retried-without-exiting)) | the pull goroutine |
| polls and probes | each task's own interval, from the dispatcher (#1634) | the bounded worker pool, through the endpoint's lane (#1249) |
| commands | pulled on the heartbeat | one at a time, each waiting for its endpoint's lane |
| held sessions | for as long as the worklist holds them, reconciled the moment it lands | one supervised goroutine each, the first dial through node-wide slots (#1636) |
| housekeeping | the shorter of the report window and the heartbeat | unclaimed lines, push counts, the collection report window |

Every discrete request rides its endpoint's lane ([the endpoint lane](#the-endpoint-lane)), and
every job taken off a lane consults the endpoint's gate first ([implicit reachability](#implicit-reachability)).
How a task's timer fires, skips and reports lateness is under
[scheduling](#scheduling-concurrency-and-self-observability).

:::design[Target design: the heartbeat on its own and commands on a server wake, tracked in #1635 under #1624 (ADR-1637)]
The heartbeat publishes liveness and nothing else, and a command runs the moment it is issued: the
server wakes the node, with a fallback pull, and the command takes a bounded command pool through
its endpoint's lane, so **no work waits on the heartbeat**.
:::

The built endpoint types, their per-task params, and the fixed samples each emits are the collection **type catalog** ([endpoint types and their config](/architecture/collection/#endpoint-types-and-their-config)); this page covers how the node *executes* them: reachability gating, sessions, the endpoint lane, scheduling.

## Sessions

Built (#816): the node holds one session per endpoint whose worklist carries a standing listen task
over a transport with a session dialer (the line protocol over `tcp`, `tls`, `telnet` and `ssh`), or
a driver poll whose driver declares a `session` block or rides a transport with no stateless line, reconciled against every worklist pull and torn down on task
removal, re-address, or node shutdown. It establishes,
lends its connection to the endpoint's line polls, recovers with a jittered, bounded backoff, and
reports each transition as the `session-state` property (`establishing`, `up`, `recovering`,
`down`, `rejected`) plus `session-reason` and a self-log carrying the reason; there is no
`session_log` table
([ADR-0150](/architecture/decisions/#adr-0150-a-held-sessions-state-is-a-property-series-and-node-logs-not-a-session-table)).
The machine's detail is on [collection](/architecture/collection/#sessions). Built with #817: the
subscription on establish (a listener's instantiation commands, re-sent on every recover) and the
demux on the shared connection (listeners first, answers by reply shape;
[routing](/architecture/collection/#listeners-over-a-held-session)). Built with #1049:
**authentication on establish**, as a layer that gates the session. The worklist carries each
driver task's `session` block (read off the live driver spec) and the endpoint's connection
fingerprint and published gate (ADR-0158); the node
renders the login against the delivered secrets (a credential it lacks refuses the session by
reference, never by value), runs it on every connection before any poll, command, or arm, and
publishes its verdict as `endpoint-authenticated`. A terminal refusal reads `rejected` and the node
dials no more: a poll over it fails fast and waits its interval (or runs the moment its
session is up again, so a fixed credential lands data at once), a command fails with the reason, and
neither falls back to a throwaway connection. The node gates the endpoint at the refusal's ladder
rung for its fingerprint, across its probes, polls, commands and session, publishes the gate as
`endpoint-rejection` with the refusal's reason beside it as `endpoint-rejection-reason` (so a probe's
gate on an endpoint holding no session still says why, #1110), and adopts it back from the worklist
after a restart (publishing no reason for a refusal it never saw), so it makes no attempt
against an unchanged fingerprint; a rotated secret, a new address, port or transport, a changed login
declaration, or an operator's `:retry` moves the fingerprint and lifts it
([auth and rejection](/architecture/collection/#auth-and-rejection),
[the gate](/architecture/collection/#the-connection-fingerprint-and-the-layered-gate),
[ADR-0155](/architecture/decisions/#adr-0155-auth-is-a-layer-that-gates-a-held-session-and-a-terminal-refusal-reads-rejected)).
Built with #1035: a **tls** endpoint's connections run the TLS handshake before anything else,
under the verification mode its params carry (derived from the driver's `tls-mode` input). In pin
mode the node keeps **one public key pin per endpoint**, shared by the held session and the
reachability probe and carried across a session's recycle: a key trusted on first use is published
as `tls-certificate` (and published again while the worklist does not yet deliver it back), a pin the
worklist delivers that differs (an operator's `:retry` with `certificate`) replaces it and recycles
the session, a pin the worklist delivered and then stops delivering (withdrawn) is forgotten, so the
next connection trusts on first use (the ssh host key likewise), and a key that differs from the pin
is a `certificate` refusal that gates the transport rung, beneath which the probe runs only the TCP
connect
([tls](/architecture/collection/#tls),
[ADR-0160](/architecture/decisions/#adr-0160-tls-is-its-own-transport-over-a-shared-primitive-and-its-pin-is-the-public-key)).

Built with #1636: the held sessions are a **supervised, independent pool** that no longer rides the
heartbeat ([ADR-1636](/architecture/decisions/#adr-1636-held-sessions-reconcile-on-arrival-are-supervised-and-dial-through-node-wide-slots)).

- **Reconcile on arrival.** A worklist reconciles the held sessions and datagram sockets the moment
  it lands: a held endpoint added, removed, re-addressed or re-keyed is started, torn down or
  recycled then, not at the next heartbeat. A reconcile on its own timer (the heartbeat interval)
  stays as the safety net for a gate or a retry that changed between worklists. Every reconcile
  runs on the node's one run-loop goroutine, the session maps' one writer, and a lane worker
  borrowing a session reads them under their lock (#1634), so a reconcile, a poll borrowing a
  session and a housekeeping pass never race.
- **Supervision.** Every session-layer goroutine (the session machine, each connection's reader,
  the arm conversation, the datagram arm) runs under a recover. A recovered panic is logged at
  error with its stack (facility `collection`), counted, and becomes that session's ordinary
  failure: the connection closes, the session reads `recovering` with the reason `panic: <value>`,
  and the normal backoff redials it, so one that panics on every attempt still reaches `down`
  after the attempt budget. A panic on one device's bytes costs that one session; the node and
  every other session keep running. A datagram arm that panics closes its socket, and the next
  reconcile binds and arms it again.
- **Bounded establish.** A session's first dial and handshake (the connect, the TLS or ssh
  handshake, the login dialog) share node-wide slots, `node run --session-dials` (env
  `OMNIGLASS_NODE_SESSION_DIALS`, default 16), so a node restarting with hundreds of held endpoints
  opens them a few at a time. A session waiting for a slot reads `establishing`, and an exchange
  against it follows the first-dial wait. A redial takes no slot, so endpoints that never answer a
  dial cannot hold up a healthy session's recovery; the jittered backoff spreads a mass redial.
- **Housekeeping on its own clock.** Unclaimed lines are flushed and pushes are counted into the
  collection report on a housekeeping timer at the report window, never longer than the
  heartbeat, so a long heartbeat does not hold them. A pass takes the report once its window
  reaches its grid end, and the next window starts at that end, not at the pass, so a pass running
  a second late never stretches the window that follows (#1779).

Still design below: the pool knobs.

A stateful endpoint (`ssh`, `mqtt`, anything held open) becomes a **session** at runtime: one
connection keyed by `(node, endpoint)`, shared by every task and command under it, handshake and
auth paid once. A pool holds it open across ticks (reconnect, backoff, keepalive); a listener
runtime wakes on its inbound. The socket is ephemeral on the node, which **reports lifecycle
transitions as the `session-state` property and its own logs** (ADR-0150 replaced the designed
`session_log` rows and `session` view).

Lifecycle: **establish** (connect, authenticate, subscribe if a stream rides the session),
**operate** (pollers and stream events over the held connection, demuxed; built for the line
protocol, #817), **recover**
(graceful retry with backoff, surfaced as `session-state` and a node log, never hammering: a rejected
credential risks lockout; a subscription is session-scoped, so a reconnect **re-subscribes**),
**teardown** (exit cleanly, set up again).

**Where failures land.** `session-state` and the node's logs own **connection health** (cannot connect, auth rejected,
dropped, timeout); the **data event owns parse health**: a parse failure emits a `collection.failed`
event carrying the cause (plus the `action` row for commands; the `raw` bytes beside it are #1251), surfacing as an alertable
collection-health sample. A command timeout can touch both.

::::design[Target design: the session pool's knobs, tracked in #1254; the held transports beyond the line protocol, the http stream and the websocket are #1036 (mqtt), #1039 (modbus/tcp), #1040 (bacnet/ip), #1041 (knxnet/ip)]
A pool holds each session open across ticks today with fixed reconnect, backoff and keepalive; its
knobs are not yet declared.

:::caution[Open question]
The exact `session` lifecycle state enum and pooling parameters (idle timeout, max lifetime, pool
size per endpoint, a shared versus dedicated session for a stream).
:::
::::

## Inbound handling on a shared connection

Built for the line protocol (#817): one reader per held connection routes every inbound line,
**listeners first** (a claimed line is never an answer unless it also matches the exchange's
declared reply shape), then the exchange in flight by its reply shape (or, with none declared, the
first unclaimed line), then unclaimed (counted, and a `collection-failed` per housekeeping pass
when the spec says so). This reverses the design's order below, which tried an outstanding poll's matcher first
([ADR-0151](/architecture/decisions/#adr-0151-a-held-line-session-routes-every-inbound-line-listeners-first-answers-by-reply-shape)); the precedence and its reasons are on
[collection](/architecture/collection/#listeners-over-a-held-session).

:::design[Target design: framing-driven routing and the orphan frame, tracked in #1057; raw on failure is #1251]
One connection carrying heterogeneous inbound frames cannot assume an arriving frame answers the
last command. Frames route through an **ordered matcher set**: every task
contributes a matcher (a poller's awaited-response shape, a listener/stream's `match:` predicate),
each frame tested **in order**, first match winning. While a poll is **outstanding** its response
matcher is tried first, then the standing matchers in declared order, so an event arriving mid-poll
falls through to its stream instead of being mis-eaten as the response (the built order reverses
this, above). Where the protocol itself frames responses vs events (xAPI `*r` vs `*e`, a request
id; built for a correlated websocket exchange's id), framing drives routing and the regex only
extracts within the matched frame, and a driver's declared prompt delimits a held answer (#1057).
An **unmatched** frame lands as `raw` (orphan, logged): a missing matcher surfaces, never fails
silently. Today it is counted and logged once a housekeeping pass (the report window, never longer
than a heartbeat, #1636), counted once a report window as the
endpoint's `push-unclaimed-count` ([collection telemetry](/architecture/collection/#collection-telemetry-every-endpoints-account-of-itself)),
and lands as a log line only where the spec asks (`unmatched: log`).
:::

## The endpoint lane

Built (#1634, #1249,
[ADR-1249](/architecture/decisions/#adr-1249-the-endpoint-lane-runs-one-request-at-a-time-operator-work-first),
under [ADR-1637](/architecture/decisions/#adr-1637-a-node-runs-each-kind-of-work-on-its-own-timer-and-pool-one-request-per-endpoint-at-a-time)):
every discrete request the node makes rides **its endpoint's lane** on the node's bounded worker
pool (`--lane-workers`, default 16, or `OMNIGLASS_LANE_WORKERS`). A lane is the unit of exclusion:
one request in flight per endpoint, while two endpoints of one component, or of fifty, run side by
side, so a slow device holds only its own lane. The node's work splits by shape:

- **discrete work** rides the lane: scheduled polls and probes from the
  [dispatcher](#scheduling-concurrency-and-self-observability), and an operator's commands, run nows
  and previews. Operator work waits in a class of its own, ahead of the scheduled work queued on
  the same lane; nothing is preempted, so the request in flight finishes first.
- **standing receivers** (listen tasks) never take a lane: a listen task is what holds its
  endpoint's session, sharing it with the polls and commands it demuxes for (#817), or owning its
  connection.

**Each job asks the gate as it is taken.** A lane holds no state of its own about its endpoint: a
job taken off it consults the endpoint's [gate](#implicit-reachability) first, so work queued behind
a request that closed the gate (a reboot's `disrupted`, a refusal) answers with the gate's reason
and sends nothing. A queued command fails naming the reason, a run now answers `Gated` (the API's
409), and a scheduled poll is held and counted; a probe a path upstream holds is not run, and is
counted held the same way. Where more than one reason holds, the most causal answers: `upstream`
before `disrupted` before `refused` before `unreachable`, the oldest within a kind. The wait after
a disruptive command that this section once designed (a lane blocked until the host reads up) is
not a lane at all: it is the gate's `disrupted` reason (#1651, [below](#implicit-reachability)), so
the lane stays free while the endpoint is held.

**A preview resolves to its endpoint.** The node indexes its worklist by transport and target; a
preview whose polls all name one inventoried endpoint's transport and target takes that endpoint's
lane and answers at once, 409 naming the reason, when that endpoint's gate is closed, a refusal
included, since a preview sends its own request. A preview that names no endpoint (a device not yet
attached) runs off-lane, gated only by its host: a host another endpoint's probe reads unreachable
answers 409 whatever transport the preview speaks.

**Overload is counted, never queued without bound.** Scheduled work never queues behind itself (a
due whose run is in flight is skipped and counted,
[below](#scheduling-concurrency-and-self-observability)). Operator work waiting across every lane is
capped (64): a run now or preview past the cap answers at once that the lanes are full (the API's
429), and the dispatch window counts the offers against the refusals (`operator-offer-count`,
`operator-refused-count`), saying them in its `dispatch window` self-log line for any window that
refused one. Commands are already bounded: the node actuates its pulled commands one at a time,
each waiting for its endpoint's lane.

The node-side queue is **not** durable: durability lives **server-side** (the command queue, the
cascade-configurable telemetry buffer). On reconnect the node re-pulls its worklist, pulls its
pending commands, and replays unacked telemetry publishes (each landed once by its batch receipt).

::::design[Target design: component-wide exclusion and a held session's pool size, tracked in #1649 and #1254]
A driver whose device cannot take two endpoints' requests at once opts its component into
component-wide exclusion, which keys that component's lane on the component, not the endpoint
(#1649). A shared poller runs once on its parent and fans out at binding. More than one request in
flight to one endpoint comes only from a held session's declared pool size (#1254, default 1), since
connection and order semantics differ per protocol.
::::

## Implicit reachability

Any endpoint with a host address gets reachability for free: the node pings the host and checks the declared port(s), continuously and out of band; a smart default, **bypassable per endpoint** (for endpoints that drop ICMP or have no port).

**Verdict-gated polls** are built (#1650,
[ADR-1650](/architecture/decisions/#adr-1650-an-endpoint-gate-holds-a-set-of-reasons)). The
endpoint's reachability probe feeds its gate: **two consecutive down verdicts** add an
`unreachable` reason to the gate, dated from the first down, so one lost probe never stops
collection; an inconclusive probe neither adds nor resets the streak, nor does a rung that reads
down only because a refusal holds it (snmp's held get, udp's gated request), so a refused endpoint
never also reads unreachable; the next up verdict removes it. While the gate holds `unreachable`, a due poll is **held**: nothing is sent, no failure and no
backoff are booked, and the window counts it as `poll-held-count`, apart from
`poll-failure-count`, because a poll the node chose not to send is not a lost sample. The first
up verdict releases it at once. A command or a run now on the held endpoint answers at once
naming the reason (`unreachable since <time>`), and a preview answers 409, not a transport
failure. An endpoint with no probe has no verdict, so its polls fail and back off as before.
Held sessions and listen tasks are not gated by `unreachable`; their own reconnect handles a
gone device.

**A disruptive command closes its endpoints' gates** (built, #1651,
[ADR-1651](/architecture/decisions/#adr-1651-a-disruptive-command-closes-its-endpoints-gates)).
A command whose type is [disruptive](/architecture/commands/#the-command_type-registry) (`reboot`)
arrives with its **hold set**, the endpoints on this node the command takes down (its target's
in-band endpoints and those of what it hosts, or every endpoint of what a power command's outlet
feeds; [the hold set follows dependency edges](/architecture/commands/#the-hold-set-follows-dependency-edges)),
and the node adds a
`disrupted` reason to each one's gate **before it sends**, dated at the delivery and naming the
command (`disrupted by reboot (command 42) since <time>`). For the type's quiet period nothing runs
against a held endpoint: polls are held and counted as `poll-held-count`, never as failures; the
probe does not run; a held session does not dial; a command, a run now or a preview answers at once
naming the reason. After quiet each endpoint reopens on **its own** first up verdict, and only a
probe that **began** after quiet counts, so an answer already in flight when the command went out
cannot release it; a down verdict releases nothing, and a sibling that has not answered stays held.
The delivery starts the probe's down streak over, and a verdict read before quiet ends neither feeds
nor resets it, so `unreachable` takes two downs read after quiet, dated from the first of them.
The type's hold is the backstop: an endpoint still held then reopens anyway and the window counts
it as `hold-expiry-count`, so a device that never comes back is seen rather than silently resumed.
The lane is never blocked: work on other endpoints runs throughout. A failed send leaves the
gates closed until up or the backstop, since whether the device received the command is unknown. A
restarted node does not adopt `disrupted` from its worklist (only a refusal and an `upstream` hold are adopted): the
restart itself outlasts the moment, and the probe reads the device fresh.

**A path upstream holds what is reached through it** (built, #1652,
[ADR-1652](/architecture/decisions/#adr-1652-a-path-upstream-holds-its-dependents-on-the-node)).
A component reached through another (a `path`
[dependency edge](/architecture/core-entities/#dependency-edges-what-a-component-needs-apart-from-where-it-sits): a peripheral
behind a codec, a device behind a gateway) gains the `upstream` reason on each of its in-band
endpoints while **any** endpoint of its upstream has a closed gate, for any reason, a refusal
included. The worklist carries each task's path upstreams (the upstream component and its in-band
endpoints on this node; an out-of-band endpoint carries none and is never held), and the node
derives every endpoint's `upstream` reason with one pure function each time any reason set moves, so
the dependent closes in the same pass as its upstream, not on the next pull. The reason has two
phases. **Held**, while the upstream is closed: due polls are held and counted as
`poll-held-count`, the probe does not run (its skipped runs count as held too), so the dependent's
probe result never moves and nothing fires on it, and a command, run now or preview answers at once
(`held by upstream gateway-1 since <time>`). A gateway going down raises one root cause on the
gateway, not a storm on everything behind it. **Waiting**, once every upstream opens: the probe
resumes, polls stay held, and the dependent's own next up verdict releases it; a dependent with no
probe has nothing to wait for and is released at once, and one that earns its own `unreachable`
while waiting drops `upstream`, so its gate names its own cause. Downs its probe read before the hold do
not count toward that: they saw the same outage through the upstream. **The reason names the root.** Down
a chain A to B to C, while A is closed B and C both read `upstream A`; once A opens and B waits on
its probe, C reads `upstream B`, since B is now the closure nearest it. The derivation never takes a
dependent's reason from a closure rooted in its own component, and a waiting upstream never holds a
component it depends on itself, so a path cycle terminates and never holds itself up. The reason is
published in the endpoint's rejection row naming the upstream's component id, the reachability read
serves that component's current name when the caller may read it. A restarted node adopts it from
the worklist and keeps it held until the upstream's probes read up (the upstream's own
`unreachable` is re-earned, not adopted, so until then nothing says the path is open), then
re-derives it live, so a restart neither drops a hold unseen nor probes through a dead upstream. An upstream
on another node is not followed (#1655), and a held session's own reconnect is not yet gated by
`upstream` (#1703).

**The layered availability gate** is built (#1650, #1249,
[ADR-1249](/architecture/decisions/#adr-1249-the-endpoint-lane-runs-one-request-at-a-time-operator-work-first)):
an **OSI-layered** set of cheap checks the endpoint's probe runs on its own interval. All applicable
checks run, each shipping a built-in sample usable in rules and dashboards, instanced (the ping by
host, the rest by endpoint) and owned by the queried component; the endpoint's
**`endpoint-reachable`** verdict is their AND, and it feeds the gate from the node's local copy, so
the connection detector and the dashboard signal are one always-on probe. The probe is not a
pre-pass run before each poll: the gate it feeds is consulted as each job is taken off its
[endpoint lane](#the-endpoint-lane), so a poll asks the verdict already read rather than waiting on
a fresh check. **The ping is shared per host**: every endpoint on one host whose probe falls due in
the same probe slot (the clock truncated to the probe interval) rides one ping, the first to ask
sending it and the rest reading its answer, so three endpoints on one host send one echo burst a
slot, not three.

| Layer | Check | Sample | Notes |
|---|---|---|---|
| L3 network | ICMP ping, **shared once per host** per probe slot | `icmp-reachable` (`up`/`down`, a transition-only property) / `icmp-rtt-avg` (a metric) | **informational** (see verdict below); shared by every endpoint on the host |
| L4 transport | TCP connect (tcp-family) | `tcp-open` (`open`/`closed`, a transition-only property) / `tcp-connect-time` (a metric) | udp has no presence check: an open udp port cannot be detected, so its transport rung is the driver's request answered (built, [ADR-0163](/architecture/decisions/#adr-0163-a-udp-endpoint-binds-its-own-socket-and-its-rung-is-a-request-answered)), and the host's port-unreachable report reads `refused` |
| L7 app | protocol handshake: SNMP `sysUpTime` get (built as snmp's transport rung, [ADR-0821](/architecture/decisions/#adr-0821-snmps-rung-is-a-standard-get-answered-and-only-an-authorizationerror-reads-as-auth)) · SSH handshake+auth · telnet login chain · an HTTP response drawn, after the TLS handshake for https (built, [ADR-0902](/architecture/decisions/#adr-0902-http-polls-and-commands-send-real-requests-read-through-one-extraction-authenticated-by-a-declared-scheme)) | (verdict) | the SNMP get is the SNMP liveness (ICMP-independent); SSH/telnet log in only where the driver's ladder declares a session rung, because their liveness credential can differ from the component's; the http probe never carries a credential, so an http endpoint's session rung is proven by its credentialed polls and commands |

**The verdict respects each layer's definitiveness.** A TCP connect, any L7 handshake and a
request answered are **definitive**, so the **ping is informational**, everywhere (an
ICMP-filtered host still reads up). The UDP presence check this design once leaned on the ping to
disambiguate (an ambiguous read timeout, `pingGates`) is retired: where no port can be opened (udp,
and snmp) the transport rung is a request answered. A definitively down layer (TCP refused,
UDP ICMP-unreachable, an L7 auth/no-answer) fails the verdict regardless; an inconclusive probe (a
setup/resolve error, a missing credential) does not gate.

**The per-check toggles retire into the ladder.** The `params.<name>_check` toggles and
`params.liveness` this design proposed are superseded by the ladder a driver declares
([ADR-0158](/architecture/decisions/#adr-0158-one-connection-fingerprint-gates-every-connection-on-a-ladder-the-driver-declares)):
`ladder.ping.enabled` turns the informational ping off, `ladder.transport` names the gate (a
connect, the protocol's handshake, or a request answered: the tls handshake shipped as the tls
transport's rung, [ADR-0160](/architecture/decisions/#adr-0160-tls-is-its-own-transport-over-a-shared-primitive-and-its-pin-is-the-public-key),
and the udp request as udp's, [ADR-0163](/architecture/decisions/#adr-0163-a-udp-endpoint-binds-its-own-socket-and-its-rung-is-a-request-answered)), and `ladder.session.enabled` whether a login
rung exists. Nothing is toggled per check on an endpoint; the snmp get shipped as snmp's rung
([ADR-0821](/architecture/decisions/#adr-0821-snmps-rung-is-a-standard-get-answered-and-only-an-authorizationerror-reads-as-auth)).

The honest limit, built as stated (#821): a v2c wrong community is a **silent drop**, so a get
failure alone cannot separate down from wrong-community; the rung reads not responsive, timed out,
retryable, never rejected. Only an agent that answers `authorizationError` refuses a community
detectably, and that is a terminal `auth` refusal at the session rung. Cross-referencing the ping
narrows it: host pings + get silent = "reachable, SNMP not answering this community, or stopped";
with ICMP fully blocked even that is lost. The ssh probe still tries a params credential on every probe
(#1060); the built telnet probe never logs in (it
reads the first negotiation or banner bytes; the login is the held session's auth step, so a probe
cannot retry a bad credential on every run, [ADR-0157](/architecture/decisions/#adr-0157-telnet-and-ssh-are-held-line-transports-their-login-is-the-drivers-and-the-ssh-host-key-is-pinned-on-first-use)). A driver's `ladder.transport.request.get` overrides the probe OID.

**One gate, a set of reasons.** Every endpoint has one gate, the layered gate generalized
([ADR-1637](/architecture/decisions/#adr-1637-a-node-runs-each-kind-of-work-on-its-own-timer-and-pool-one-request-per-endpoint-at-a-time), [ADR-1650](/architecture/decisions/#adr-1650-an-endpoint-gate-holds-a-set-of-reasons)).
It holds a set of reasons, each with a kind, a since time, a source and its own release rule, and
it is **open only when the set is empty**. The reasons above, side by side:

| Reason | Added by | Released by | Built in |
|---|---|---|---|
| `refused` | a terminal refusal on a rung | a fingerprint change, an operator's `:retry`, or the rung passing | ADR-0158, into the set with #1650 |
| `unreachable` | two consecutive down probe verdicts | the next up verdict | #1650 |
| `disrupted` | a disruptive command, before it is sent, on every endpoint of its hold set | after the quiet period, each endpoint's first up verdict from a probe begun after it; the hold backstop releases the rest and counts the expiry | #1651, the hold set #1648 |
| `upstream` | a closed gate on the component the endpoint is reached through (a path edge, cycle-guarded) | the upstream opening, then the dependent's own first up verdict | #1652 |

**Poller** tasks run only while the gate is open; **listener** (`mode=listen`) tasks run ungated,
never pinged; **inline probes** (`icmp`/`tcp` with the host on the task) *are* the check, ungated.
A down endpoint's gate samples ship in **one** batch. L5 (socket) and further L7 handshakes
extend the stack as rungs a driver's ladder declares, not as toggles.

:::design[Target design: the ping's rtt and loss as observations, tracked in #1063]
Each echo lands as one `rtt` observation labelled with its rung, a lost echo as a sample with
outcome `lost` rather than a gap, and `rtt-min`, `rtt-avg`, `rtt-max`, `rtt-variation` and
`packet-loss` derive from those observations as windowed calculations. Today the shared ping
reports `icmp-reachable` and an averaged `icmp-rtt-avg`, and no loss.
:::

## Shipping samples

The node ships a native `TelemetryBatch` per tick: a `TaskGroup` for each task it ran (the task, its observed `ts`, and its metric, property, event and task-log lanes) plus the node's own log lines, **published to the JetStream raw ingress subject** (protobuf-encoded, the proto surviving as the NATS message schema; the fields are the generated `proto/og/v1/telemetry.proto`). Each group is collected, normalized and bound to its own task on the server, so the batch carries many writers and one node. The task lanes at the top of a batch belong to the API push subject; a node-subject batch that carries them is terminated.

The publish is **held and retried** until the stream acks it ([delivery](#delivery-a-batch-lands-once)).

:::design[Target design: raw on failure, tracked in #1251; the OTLP adapter and the admission consumer, #430]
On a parse or validation failure the node also ships
the **raw** wire bytes so the server can emit a `collection.failed` event; on success raw is omitted
(there is no telemetry table), unless a **dev raw-mode** is on. An **OTLP adapter** at the edge
accepts OTLP from third-party tools and translates to the native shape.

```d2
direction: right
classes: { node: { style.border-radius: 8 } }
worklist: "pull worklist\n(placed tasks + commands)" { class: node }
execute: "execute:\nprotocol + locate/Expr extraction" { class: node }
normalize: "normalize: samples + labels\n(+ raw on failure)" { class: node }
ship: "buffer + publish\nraw ingress subject" { class: node }
admission: "admission: bind owner\n(consume time) → trusted" { class: node }
worker: "rule engine + persistence\n(trusted stream)" { class: node }
failed: "collection.failed\n(event, carries raw)" { class: node }
worklist -> execute
execute -> normalize
normalize -> ship
ship -> admission
admission -> worker
ship -> failed: "raw on failure" { style.stroke-dash: 4 }
```

Samples are already produced at the edge; an **admission consumer** binds owner (registry
lookup, owner attribution against the node's placement) at **consume time** and republishes to the
trusted stream the rule engine and persistence read, so a forged owner drops before evaluation,
not at the durable write. The server never re-derives observed samples; the node's job ends at the
ship.
:::

## Self-monitoring: the node reports itself

Every node reports its own health as platform metrics on its own component (#1625,
[ADR-1625](/architecture/decisions/#adr-1625-a-node-reports-itself-on-its-platform-component-and-platform-keys-take-no-prefix)).
Registering a node gives it an `omniglass-node` component in the reserved, unplaced `platform`
system, named on the node row (`node.platform_component_id`); decommissioning the node removes it.
An operator may relabel either and reshape neither, and only an all-scope read sees them.

Each heartbeat tick the node takes its own figures and sends them in the **node-self block** of the batch
the tick already publishes (`TelemetryBatch.node_self`). The block names no owner: the server lands it
on the publishing node's platform component under the platform writer class, so a node speaks only for
itself, and only in the keys declared as its own. Figures the node reads every tick (the dispatcher's longest pass,
the worst lateness and gap excess of the runs it dispatched, the spool depth, the sessions held by
state, the Go process figures) go every tick; counts (dues met in flight, skipped intervals) go once
per report window, every window, 0 when quiet, so a count returns to 0 after the window that had it.
The [lane pool](#the-endpoint-lane)'s busy share, `worker-busy-fraction` (#1686), is a level read once
per report window: the worker time spent running polls, probes, commands and runs, over the worker
time the pool had (each dispatch window's length times its workers, summed), from 0 (idle) to 1
(every worker busy all window). It is the node's headroom: near 1, dues start to wait for a worker.
The node runs one pool for every kind of discrete work, so the figure carries the single instance
`lane` rather than one per kind
([ADR-1686](/architecture/decisions/#adr-1686-a-node-reads-one-busy-fraction-for-its-one-lane-pool)).
The node hop's **delivery** goes the same way (#1659): per item kind as the instance,
`delivery-in-count` (items that settled at the spool this window), `delivery-out-count` (the ones the
stream acked) and `delivery-loss-ratio`, `(in - out) / in`, 0 when nothing came in. An item settles
when the stream acks its batch (in and out), or when the spool drops it at a cap, the bus refuses it,
it never encodes, or (a self-log line) the bounded log buffer drops it (in only, #1692); a batch held for a retry has not settled and counts in the window it
does. These three replace the old spool-drop and publish-failure counts, so every hop's loss reads
as the same ratio ([messaging](/architecture/messaging/#accounting-every-item-the-loss-ratio)), and
[Platform health](/guides/operator/platform-health/#where-work-is-lost) draws this hop beside the
server's ingest, with each node's own share as its Loss (#1646). The **schedule hop** writes the
same three keys at the instance `schedule/{kind}` (#1693): the runs due on the schedule that produce
the kind in, the runs made out, so a skipped due is that hop's loss; Platform health draws it with
#1739. The schedule figures read
the dispatcher's window ([below](#scheduling-concurrency-and-self-observability)): a poll keeping its
interval reads a zero `schedule-gap-excess`, and a late pass or a skipped slot reads the distance it
lost. A node that goes silent sends nothing, so the server writes the one figure that keeps
moving: `checkin-age`, every claimed node's seconds since its last heartbeat (or its claim, before the
first), every 15 s; it grows while the node is silent, and past 90 s the seeded `node-silent` rule
opens a `high` alarm on the node's platform component, cleared by the next heartbeat (#1707). The keys, their units and how each is sampled are in
[collection](/architecture/collection/#self-monitoring-every-nodes-account-of-itself).

The node's **command queue** reports on the same component (#1626). The server reads every node's
queue every 15 s and writes `command-pending-count`, `command-pending-age` (zero while nothing
waits) and `command-stuck-count`, and, over the window since its last read (zero for a quiet window),
`command-redelivery-count` and `command-expired-count` (instance `undelivered` or `unexecuted`). The
command wire writes `command-dispatch-latency` at a command's first take and
`command-execution-latency` at its report, one sample per command. The node itself counts
`command-pull-failure-count`, the pulls of its queue that got no answer or one it could not read,
once per report window, 0 when quiet, on its node-self lane. How each is read, and why expiry keys on the execution
stamp rather than the status, is on [commands](/architecture/commands/#queue-health-each-nodes-command-queue-measured).

What one poll costs the node is read from the kernel, not from the clock on the wall (#1870).
`poll-duration` is mostly time spent waiting on the device, so it cannot say what a Q-SYS poll costs
against an SNMP one. A lane worker runs each driver poll, scheduled or run now, with its goroutine
locked to its OS thread, and reads the thread's CPU clock (`CLOCK_THREAD_CPUTIME_ID`) before and
after: the difference is the user and system time the kernel charged that run, and none of the time
it spent blocked on the network. The node lands the mean per run as `poll-cpu-time`, in
milliseconds, beside `poll-duration` on the endpoint's component, once a report window
([collection telemetry](/architecture/collection/#collection-telemetry-every-endpoints-account-of-itself)).
The clock counts only the run's own thread. The reads and writes the run makes itself are on it, and
so is any garbage-collector assist the run pays while it allocates; work handed to another goroutine
is not. That leaves out a held session's or a websocket's reader goroutine (which does the session's
reads, writes and decryption while the run waits on its answer), the HTTP client's connection
goroutines, a helper a driver starts, and the collector's background workers, which no task is charged
for. So read it as a floor on a poll's cost, with the node's process CPU as the ceiling, and compare
drivers on it only within one transport: a poll over a held session reads cheaper than a direct SNMP
or bare TCP poll that does its own I/O. The comparison across transports is the cgroup sweep of #1871.
Reachability probes are not measured; the figure is a driver poll's. A thread locked through a run
that blocks is parked for that run, so the node holds at most one extra thread per lane worker. A platform with no per-thread clock lands no
`poll-cpu-time` rather than a false zero; every release platform but Windows has one. The per-driver
cost table that reads it against the process's own cgroup is #1871
([ADR-1870](/architecture/decisions/#adr-1870-a-polls-cpu-cost-is-its-threads-kernel-clock-cross-checked-by-the-cgroup)).

## Scheduling, concurrency, and self-observability

Built (#1634, under [ADR-1637](/architecture/decisions/#adr-1637-a-node-runs-each-kind-of-work-on-its-own-timer-and-pool-one-request-per-endpoint-at-a-time)): each poll and probe runs on **its own interval**, from a dispatcher with a timer of
its own, not on the heartbeat. Every task sits once in a due-time heap; the dispatcher sleeps until
the earliest due, hands each due task to its endpoint's lane on the bounded worker pool
(`--lane-workers`, default 16, [above](#the-endpoint-lane)), and books the task's next due.
The heartbeat publishes liveness, pulls commands, ships the spool and takes the dispatcher's window;
it sets no task's cadence
([ADR-1634](/architecture/decisions/#adr-1634-each-poll-and-probe-runs-on-its-own-interval-from-a-dispatcher-of-its-own),
amending [ADR-0152](/architecture/decisions/#adr-0152-a-driver-poll-runs-on-its-declared-schedule-at-heartbeat-granularity)
and [ADR-1273](/architecture/decisions/#adr-1273-a-reachability-probe-runs-on-the-nodes-probe-interval-not-every-heartbeat)).

A driver poll's interval is its declared `schedule.every`; a poll that declares none runs at the
heartbeat interval, still on the dispatcher's timer. A reachability probe's interval is the node's
`--probe-every`, default 30s, which no longer falls back to every check-in. Keep it under the
console's 150s staleness window: an endpoint only a probe watches reads stale once its last probe
is older than that (#1304).

**A stable phase.** A task's dues fall on a grid of its own: one interval apart, offset from the
Unix epoch by a hash of the task's id. A task new to the worklist runs at once, and so does every
task when the node starts; its next due is the first slot of its grid at least half an interval on,
so a first run just before a slot is not followed by a second a moment later, and from there each
due is an interval apart. Because the grid is a function of the task's id alone, a restarted node
lands each task on the same slots as before, and a fleet of tasks with one interval spreads across
it instead of firing together. A task whose connection fingerprint moves (an edit in place, a
rotated secret, an operator's `:retry`, #1061) runs at once, so an operator's fix reads its verdict
in seconds.

**Skip, not burst.** A task still in flight at its next due is **skipped and counted**, never queued
behind itself, so a slow device is never asked twice at once and falling behind surfaces instead of
adding load. A pass that comes late (a busy pool, a stalled host) runs a task once and books the
next slot ahead of now, never a catch-up burst. A run's outcome books on top of the grid:

- a fetch that failed retries after a backoff that starts at a fixed second, doubles on each
  consecutive failure and caps at the interval, so a 24h poll that fails during a device reboot
  retries in seconds, and the backoff never reads the heartbeat; a component's `Retry-After` is a
  floor on the wait (#902);
- a terminal refusal waits its interval, unless its session reads up again first, which runs it at
  once (#1049);
- a poll its endpoint's gate holds is decided in the pass and never takes a lane: counted held once
  per due and run the moment the gate opens
  ([ADR-1650](/architecture/decisions/#adr-1650-an-endpoint-gate-holds-a-set-of-reasons)).

A listener's pushes are never gated or scheduled: a listen task is what holds its endpoint's
session.

**What the node reports of its own schedule.** Each endpoint's collection report window counts the
polls that fell due (`poll-due-count`, held, refused and skipped ones included) and the dues skipped
because the last run was still in flight (`poll-skipped-count`), both landed only when nonzero; skipped
over due is the endpoint's schedule loss, 0 when healthy. Each heartbeat window the dispatcher also
keeps its own account: the longest pass (`tick-duration`), the dues that met their run in flight
(`tick-overrun-count`), every due passed over (`schedule-skip-count`), the latest dispatch past its
due, and the pool's busy worker time. The node sends them as its own platform metrics in the node-self
block ([above](#self-monitoring-the-node-reports-itself)), the latest dispatch as
`schedule-lateness` and, beside it, the worst gap between two scheduled starts of a task off its
interval as `schedule-gap-excess`; the busy time adds up across the report window and lands once, when
it ends, as `worker-busy-fraction`. The window's high-water
marks, the most workers busy and dispatches waiting at once (`worker-busy-count`,
`dispatch-queue-depth`), are not metrics: the node says them, with the window's other figures, in a
`dispatch window` self-log line, only for a window that passed a due over or refused an operator's
request, so a node keeping its schedule logs nothing.

::::design[Target design: a probe concurrency apart from the pool, a per-task deadline, and the remaining `node.self` counts, tracked in #1094 and #430; the seeded node rules are the `event_rule` (ADR-0050)]
The probes run at a **high fixed concurrency** (`gateConcurrency`) with short timeouts, so the cheap
gate is never throttled by a small `--lane-workers`, and a node facing many dead targets is bounded
by concurrency, not the serial sum of probe timeouts (a dead SNMP get costs `timeout * (retries+1)`,
via `--snmp-timeout` / `--snmp-retries`, default 3s x2). Each poll task gets a per-task deadline
(`--task-deadline`, default 30s).

The node's account of itself is built as the node-self block on the node's platform component
([above](#self-monitoring-the-node-reports-itself)), not as node-owned samples; the paragraph below
is the earlier design it replaces, kept for the counts it names that are not yet sent.
On its housekeeping timer the node publishes a `node.self` envelope: the dispatcher's window above,
task attempted/ran/skipped/failed counts, endpoint probed/up/down counts, the `node.overrun` state (a
due skipped because its run was still in flight). Not
special-cased: `node.self` is node-owned samples (the seeded `node.*` types) riding the **same
raw-ingress -> admission -> trusted** path; the self shape is **built into the binary** (no
operator-authored template), the admission consumer binds it to the **reporting node**
(`owner_kind = node`, the `node_id` arm of the exclusive arc), and the rule engine's batching and
amortized rule refresh apply for free. Self-telemetry is best-effort (a failed report is logged,
never fatal).

A node that goes dark reports nothing, but the server still writes its `checkin-age`, so the silent
node alarms through the ordinary rule path: the seeded `node-silent` rule, built (#1707). The earlier
design here, a node-liveness sweep raising a node-owned `node-down` alarm directly after
`OMNIGLASS_NODE_DOWN_AFTER`, is replaced by it ([ADR-1707](/architecture/decisions/#adr-1707-a-silent-node-alarms-through-a-seeded-rule-over-its-checkin-age-not-a-node-liveness-sweep)).

A degraded-but-alive node alarms through the ordinary **event_rule** path: a rule on a `node.*` key
opens a node-owned alarm. Two are seeded, `node-overrun` (while `node.overrun` is true) and
`node-tasks-failing` (while `node.tasks.failed > 0`), both resolving implicitly on the next clean
report, because `Evaluate` is owner-general (component, system, location, or node), which also unlocks
system- and location-owned alarms.
::::
