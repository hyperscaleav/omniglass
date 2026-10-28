---
title: Nodes
description: How the edge runtime pulls its worklist, runs tasks and commands, manages sessions, gates reachability, and ships telemetry.
sidebar:
  badge:
    text: Partial
    variant: note
---

A node is the edge runtime that collects from and controls gear wherever it sits: it pulls its worklist from the server, runs it on the spot, and ships results back. This page covers worklist pull, placement, tasks and commands, sessions, inbound demux, the task queue, reachability, and shipping telemetry; the declarative shape it executes lives in [templates](/architecture/templates/) and [collection](/architecture/collection/).

:::note[Partial]
Built: `omniglass node run` enrolls (node created server-side, enrollment token minted at
`POST /nodes/{name}:enroll`, exchanged for its NATS credential at `POST /nodes:claim`), connects
outbound-only to the server's in-process NATS server, pulls its worklist over a `og.v1.worklist.<node>`
request-reply (its enabled tasks plus a `config_generation`), and heartbeats on `og.v1.heartbeat.<node>`
(the server stamps `last_heartbeat_at`). **Per-node subject isolation is enforced**: each node's NATS
credential is permitted only its own `<node>` subjects. Also built: running tasks (the probes,
`internal/node/probe.go`), shipping telemetry (the JetStream `TelemetryBatch` ingested by
`internal/bus/consumer.go`, each publish waiting for the stream's ack under a minted `Nats-Msg-Id`, a
batch the stream has not acked held in a bounded spool and shipped on the next tick,
[below](#delivery-a-batch-lands-once); a batch whose task is not placed on the node is dropped by
the confinement fence, warned about once per node per minute and counted on the node's
`orphaned_batches` and `last_orphaned_at`, #1200), and the raw self-log lane (ADR-0066: `internal/node/logs.go`,
`GET /nodes/{name}/logs`, the console Self-logs panel), the command wire (a per-node pull, actuated
over the driver's binding, [ADR-0136](/architecture/decisions/#adr-0136-actuation-is-a-per-node-pull-rendered-at-dispatch)),
driver polls on their declared schedule (#822), and, for the line protocol over `tcp`, `tls` (#1035), `telnet` (#1033) and `ssh` (#1032), held sessions
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
kept alive by ping and pong ([websocket and wss](/architecture/collection/#websocket-and-wss)). Also built: a
node serving its own simulated components, `omniglass node run --simulate` (#1198,
[below](#serving-its-own-simulated-components)). Still `Design`: the
durable JetStream command queue, the session pool knobs and the held transports beyond these
(#1036 (mqtt), #1039 (modbus/tcp), #1040 (bacnet/ip), #1041 (knxnet/ip), [ADR-0153](/architecture/decisions/#adr-0153-the-held-session-proof-is-the-line-protocol-over-tcp-other-held-transports-are-their-own-issues)),
config-generation-driven cache invalidation, node self-telemetry, the node-down sweep, the
cascade-resolved buffer knobs, tick scheduling and concurrency knobs, and config apply (the `:apply` flow). The credential is a shared
secret (the enrollment token doubles as the NATS password); the decentralized nkey/JWT model is
deferred. See [implementation status](/architecture/status/) and
[decision log](/architecture/decisions/) (ADR-0036).
:::

## The node

The node is the edge process (`omniglass node run`), one per site, or the **server itself** when no site-local edge exists (see *Placement*). Identity is **bound to `node.name`** (a compromised node cannot impersonate another, [identity-access](/architecture/identity-access/)); it holds no config, and its writes are confined to its **placement-derived `visible_set`** (the owners of its assigned tasks): **node mode**, not all-visibility system mode.

A node carries the identity triad with one exception. Its **`id`** is `principal_id`, the immutable primary key every reference stores (a node is the detail row of a principal, and `endpoint.node_name` holds that uuid whatever its column name suggests). Its **`name`** is the operator-typed identifier and fleet address, the NATS subject token and the enrollment identity, and it is the one name with no `:rename` custom method: moving it would move a live subject, so the PATCH body carries no name at all. Its **`label`** is the operator label (console falls back to the name). Its optional `location` is **descriptive**, **not** a scope: a node stays fleet-wide, `location` clearing if that location is deleted (`ON DELETE SET NULL`). Descriptive does not mean unguarded: the create and the update resolve that reference within the caller's own `location:read` scope, the seam every other placement bind uses, and a location outside it is refused as **absent** rather than as forbidden ([ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup), amended by [#705](https://github.com/hyperscaleav/omniglass/issues/705)). The console blade is read-edit-save via `PATCH /nodes/{name}` (Edit primary, gated `node:update`, editing label, description, location; name read-only; enrolling secondary).

A node is a **taggable owner**: governed [tags](/architecture/tags/) whose `applies_to` includes `node` bind to it (fleet-wide, all-scope, `node:update`); effective tags are the `platform` layer plus direct bindings, no cascade (a node is not a scope tree); the blade carries a **Tags** panel, the list a Tags column and per-key facet. **Decommissioning** (`DELETE /nodes/{name}`, `node:delete`) hard-deletes, cascading its endpoints, their derived tasks, its node-owned tags and self-telemetry, and its enrollment credential; collected component telemetry is untouched.

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

:::design[Target design: templates, `$var:` values and placement in the worklist, tracked in #827 and #296]
The server, not the template, decides placement (next; today an endpoint names its node) and
resolves the rest of the cascade (`$var:` values, an effective `interval` overriding the driver's
schedule) before handing the node concrete work. The node never sees a template, only
materialized, resolved task and command instances.
:::

### Config propagation (declared change to running node)

:::design[Target design: config propagation to a running node (`:apply` and the generation-driven cache), tracked in #827]
An endpoint's connection config (endpoint, snmp community, http auth header) is a **projection** of
the component's declared config through its template. The node re-pulls the worklist every tick but
**caches endpoint config for its process lifetime**, so a changed input must propagate:
**reconcile on the server** (a changed declared input, via `/components/{name}:apply` or a direct
config write, re-renders the affected endpoints from the *current* declared config and upserts
them, preserving placement) and **invalidate on the node** (the worklist reply carries a per-node
**`config_generation`**, the max `updated_at` across the endpoints the node polls; an advance drops
the node's endpoint cache and re-fetches this tick, a steady value serves from cache: a real change
lands within one tick, no restart). The generation moves at **operator-config pace, not telemetry
pace**: the sample-write path never touches `endpoint.updated_at` and a no-op re-apply does not
advance it, so nodes are never woken for nothing.
:::

## The node-server protocol

The edge is **outbound-only**: a node sits behind NAT, so the server never dials it. A node is a **NATS client over the WAN**, one authenticated outbound connection (an nkey/JWT credential bound to `node.name`); everything server-to-node arrives on subjects the node is permitted to consume. Three flows share the connection:

Telemetry up is built as a JetStream publish the stream acks, under a `Nats-Msg-Id` it
deduplicates ([below](#delivery-a-batch-lands-once)); the subject it lands on is the node's own
`og.v1.telemetry.<node>`, the stream the ingest consumer reads.

:::design[The full node NATS contract, per ADR-0036; the admission split is #430]
- **Telemetry up**: the node **publishes** `TelemetryBatch` batches (`{samples, labels}` plus the
  `(task, ts)` envelope, [below](#shipping-samples)) to a **JetStream raw ingress subject**; the
  admission consumer preserves each batch's `Nats-Msg-Id` when republishing to the trusted stream.
- **Control down**: a **durable JetStream consumer** on the node's **command queue**, plus
  **worklist-change signals** (the config-generation bump); subjects scoped by placement.
- **Control up**: heartbeat (feeding the node-down sweep), command-execution results (the
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
self-logs) goes through one **spool** (`internal/node/spool.go`): the batch is minted a `Nats-Msg-Id`
(a UUIDv7) when it is built and queued, and counted sent only once the stream acks it. A send only
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

### On-demand runs: asked, answered once

The server can ask a node to **run work now** on the node's own run subject (`og.v1.run.<node>`), a
request the node answers once, the simulator operation's shape rather than the command queue's
([ADR-1163](/architecture/decisions/#adr-1163-a-poll-runs-once-on-demand-through-its-node-as-a-preview-that-stores-nothing-or-a-run-now-that-lands)). A run is wanted now or not at all, so nothing is queued: a node that is
not connected is an answer (the API's 503), and one that does not answer within the run's budget
another (504). A **preview** runs on the request's own goroutine, touches no held session and
publishes nothing, its excerpt scrubbed of the credential before it is cut. A **run now** waits for
the run loop, which owns every held session and gate, so the poll borrows the endpoint's session and
honors its gate exactly as a scheduled run does, and its samples and faults publish as a scheduled
run's would, through the same change memory (an event or a log line that has not changed is not
landed twice); the schedule is untouched. A run now the loop could not take within its caller's
budget is dropped unrun, since its caller was already answered. The node's grant
covers its own run subject only, and its one reply stays permitted for the run's whole budget.

### Buffering and retention are cascade settings

When the server is unreachable the node **buffers in memory**, bounded, **not durable at the edge**:
the spool above, its caps (count, bytes, age) fixed in code and a drop told in the node's self-logs.

:::design[Target design: the buffer knobs, tracked in #430; retention is #417]
Both the **buffer** (size, shed policy) and **retention** are **cascade-resolved**
([cascade](/architecture/cascade/)) with an install-wide `platform` binding, overridable down the
tree. A full buffer **sheds oldest metrics first and surfaces it** as a
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

The built endpoint types, their per-task params, and the fixed samples each emits are the collection **type catalog** ([endpoint types and their config](/architecture/collection/#endpoint-types-and-their-config)); this page covers how the node *executes* them: reachability gating, sessions, the task queue, tick scheduling.

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
dials no more: a poll over it fails fast and waits its interval (or runs at the first tick its
session is up again, so a fixed credential lands data within a tick), a command fails with the reason, and
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
first unclaimed line), then unclaimed (counted, and a `collection-failed` per tick when the spec
says so). This reverses the design's order below, which tried an outstanding poll's matcher first
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
silently. Today it is counted and logged once per tick, and lands as a log line only where the
spec asks (`unmatched: log`).
:::

## The component task queue

Built: a standing listen task shares its endpoint's held session with the endpoint's polls and
commands, demuxed by one reader (#817); the per-component lanes and the smart-wait gate below remain
design.

::::design[Target design: the component task queue, tracked in #1249; commands ride the per-node pull of ADR-0136]
The node's work is the **component task queue** (distinct from the central **rule engine**;
[workers](/architecture/workers/)): **poll tasks** (produce samples) and **command tasks** (from
`run` actions, producing a caused `event` + `action`-row status), split by shape:

- **discrete tasks** (pollers, commands): request/response, **serialized into per-component lanes**.
  Component, not host, is the contention key: a server with two IPs is one component, and a reboot
  takes out both endpoints. A shared poller runs once on its parent and fans out at binding.
- **standing receivers** (listen tasks): always-on, event-driven, **not lane-serialized**, sharing a
  held session with pollers (demuxed) or owning their connection.

**Smart-wait gate.** After a disruptive command the lane blocks until reachability reports the host
back up, read from the node's **local** copy, not the sample store; a fixed timeout backstops.

Today the node runs its whole worklist serially, one task after another, then its commands; the
lanes above are what make distinct components run concurrently.

:::caution[Open question]
Whether to add intra-endpoint concurrency, given that connection and order semantics differ per
protocol.
:::

The node-side queue is **not** durable: durability lives **server-side** (the JetStream command
queue, the cascade-configurable telemetry buffer). On reconnect the node re-pulls its worklist,
resumes its durable consumer on the command queue, and replays unacked telemetry publishes
(each landed once by its batch receipt).
::::

## Implicit reachability

Any endpoint with a host address gets reachability for free: the node pings the host and checks the declared port(s), continuously and out of band; a smart default, **bypassable per endpoint** (for endpoints that drop ICMP or have no port).

:::design[Target design: the layered availability gate's per-host ping, concurrent pre-pass and verdict-gated polls, tracked in #1249; the ping's rtt and loss are #1063]
The results come back as `reachable` / `port_open` **samples** usable in rules and dashboards, and
feed the smart-wait gate from the node's local copy: the connection detector and the dashboard
signal are one always-on probe.

**The layered availability gate** is an **OSI-layered** set of cheap checks run as a **concurrent
pre-pass** (high concurrency, short timeouts) before a connection-endpoint's poll tasks. All
applicable checks run, each shipping a built-in sample, instanced (the ping by host, the rest by
endpoint) and owned by the queried component; the endpoint's **`endpoint-reachable`** verdict is
their AND.

| Layer | Check | Sample | Notes |
|---|---|---|---|
| L3 network | ICMP ping, **batched once per host** per probe run | `icmp-reachable` / `icmp-rtt-avg` | **informational** (see verdict below); shared by every endpoint on the host |
| L4 transport | TCP connect (tcp-family) | `tcp-open`/`tcp-connect-time` | udp has no presence check: an open udp port cannot be detected, so its transport rung is the driver's request answered (built, [ADR-0163](/architecture/decisions/#adr-0163-a-udp-endpoint-binds-its-own-socket-and-its-rung-is-a-request-answered)), and the host's port-unreachable report reads `refused` |
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

**Poller** tasks run only if the verdict is up; **listener** (`mode=listen`) tasks run ungated,
never pinged; **inline probes** (`icmp`/`tcp` with the host on the task) *are* the check, ungated. A
down endpoint's gate samples ship in **one** batched call. L5 (socket) and further L7 handshakes
extend the stack as rungs a driver's ladder declares, not as toggles.
:::

## Shipping samples

The node ships a native `TelemetryBatch`: `{ samples, labels }` plus an envelope (`task`, batch `ts`), **published to the JetStream raw ingress subject** (protobuf-encoded, the proto surviving as the NATS message schema).

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

## Tick scheduling, concurrency, and self-observability

::::design[Target design: the tick scheduler, `node.self`, and the node-down sweep, tracked in #430 (per-task driver poll schedules are built, #822); the seeded node rules are the `event_rule` (ADR-0050)]
A tick groups the worklist **by endpoint** and runs three phases: the L3 ping pre-pass (batched per
host), the gate-verdict pre-pass, the poll phase. The gate pre-passes run at a **high fixed
concurrency** (`gateConcurrency`), the poll phase across the **bounded poll pool** (default 16,
`--workers`), so the cheap gate is never throttled by a small `--workers` and a node facing many
dead targets is bounded by concurrency, not the serial sum of probe timeouts (a dead SNMP get costs
`timeout * (retries+1)`, via `--snmp-timeout` / `--snmp-retries`, default 3s x2). Each poll task gets
a per-task deadline (`--task-deadline`, default 30s).

:::note[Settled for driver polls (#822) and probes (#1273)]
A driver poll runs on its declared `schedule.every`, at heartbeat granularity: the node keeps each
poll's next due time and runs it on the first tick that reaches it, within half a heartbeat (a task
new to the worklist runs at once, a restart polls at its first tick, and a run whose fetch failed
retries on a backoff from the heartbeat, doubling, capped at the interval). The half heartbeat is
there because a tick is judged after its own work, which varies by milliseconds: without it, an
interval equal to the heartbeat would skip a tick now and then. A reachability probe runs on the
node's probe interval (`--probe-every`) the same way, and on every tick when the interval is unset. A
probe new to the worklist runs at once, and so does one whose endpoint was edited in place or retried
(its connection fingerprint moved), so an operator's fix reads its verdict on the next tick. The
interval lets a node check in often, so a change reaches it within seconds, while probing less often,
since each probe writes its observations. Keep it under the console's 150s staleness window: an
endpoint only a probe watches reads stale once its last probe is older than that (#1304). A listener's
pushes are never gated
([ADR-0152](/architecture/decisions/#adr-0152-a-driver-poll-runs-on-its-declared-schedule-at-heartbeat-granularity),
[ADR-1273](/architecture/decisions/#adr-1273-a-reachability-probe-runs-on-the-nodes-probe-interval-not-every-heartbeat)).
:::

The loop is **overrun-aware**: it reschedules relative to each tick's finish, not a fixed ticker; a
tick exceeding its interval is flagged and the next fires immediately, so falling behind
**surfaces** rather than silently dropping ticks.

Each tick the node publishes a `node.self` envelope: tick duration, task
attempted/ran/skipped/failed counts, endpoint probed/up/down counts, the `node.overrun` state. Not
special-cased: `node.self` is node-owned samples (the seeded `node.*` types) riding the **same
raw-ingress -> admission -> trusted** path; the self shape is **built into the binary** (no
operator-authored template), the admission consumer binds it to the **reporting node**
(`owner_kind = node`, the `node_id` arm of the exclusive arc), and the rule engine's batching and
amortized rule refresh apply for free. Self-telemetry is best-effort (a failed report is logged,
never fatal).

A node that goes dark reports nothing, so a **node-liveness sweep** runs server-side: a node whose
last heartbeat (or registration, if it never checked in) predates `OMNIGLASS_NODE_DOWN_AFTER`
(default 90s) gets a node-owned **`node-down` alarm**, auto-resolved on the next heartbeat. The
sweep raises it directly (no event_rule: a dead node emits no sample), keyed by `(node-down,
node owner)` for idempotency across sweeps; this is why the node owner arc reaches `event` and
`alarm`, not just samples.

A degraded-but-alive node alarms through the ordinary **event_rule** path: a rule on a `node.*` key
opens a node-owned alarm. Two are seeded, `node-overrun` (while `node.overrun` is true) and
`node-tasks-failing` (while `node.tasks.failed > 0`), both resolving implicitly on the next clean
tick, because `Evaluate` is owner-general (component, system, location, or node), which also unlocks
system- and location-owned alarms.
::::
