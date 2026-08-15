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
`internal/bus/consumer.go`), and the raw self-log lane (ADR-0066: `internal/node/logs.go`,
`GET /nodes/{name}/logs`, the console Self-logs panel). Still `Design`: commands and the durable
command queue, sessions and inbound demux, the layered `_check` reachability gate,
config-generation-driven cache invalidation, node self-telemetry, the node-down sweep, the JetStream
publish-ack delivery contract (telemetry publish is fire-and-forget today, #430), tick
scheduling and concurrency knobs, and config apply (the `:apply` flow). The credential is a shared
secret (the enrollment token doubles as the NATS password); the decentralized nkey/JWT model is
deferred. See [implementation status](/architecture/status/) and
[decision log](/architecture/decisions/) (ADR-0036).
:::

## The node

The node is the edge process (`omniglass node run`), one per site, or the **server itself** when no site-local edge exists (see *Placement*). Identity is **bound to `node.name`** (a compromised node cannot impersonate another, [identity-access](/architecture/identity-access/)); it holds no config, and its writes are confined to its **placement-derived `visible_set`** (the owners of its assigned tasks): **node mode**, not all-visibility system mode.

A node carries the identity triad with one exception. Its **`id`** is `principal_id`, the immutable primary key every reference stores (a node is the detail row of a principal, and `interface.node_name` holds that uuid whatever its column name suggests). Its **`name`** is the operator-typed identifier and estate address, the NATS subject token and the enrollment identity, and it is the one name with no `:rename` custom method: moving it would move a live subject, so the PATCH body carries no name at all. Its **`label`** is the operator label (console falls back to the name). Its optional `location` is **descriptive**, **not** a scope: a node stays estate-wide, `location` clearing if that location is deleted (`ON DELETE SET NULL`). Descriptive does not mean unguarded: the create and the update resolve that reference within the caller's own `location:read` scope, the seam every other placement bind uses, and a location outside it is refused as **absent** rather than as forbidden ([ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup), amended by [#705](https://github.com/hyperscaleav/omniglass/issues/705)). The console blade is read-edit-save via `PATCH /nodes/{name}` (Edit primary, gated `node:update`, editing label, description, location; name read-only; enrolling secondary).

A node is a **taggable owner**: governed [tags](/architecture/tags/) whose `applies_to` includes `node` bind to it (estate-wide, all-scope, `node:update`); effective tags are the `platform` layer plus direct bindings, no cascade (a node is not a scope tree); the blade carries a **Tags** panel, the list a Tags column and per-key facet. **Decommissioning** (`DELETE /nodes/{name}`, `node:delete`) hard-deletes, cascading its interfaces, their derived tasks, its node-owned tags and self-telemetry, and its enrollment credential; collected component telemetry is untouched.

## Getting its instructions

The node pulls a **worklist** (the tasks and commands resolved for the components **placed on it**) over a NATS request-reply config pull, and **heartbeats** separately on its own subject ([the protocol](#the-node-server-protocol)), so liveness tracks independently of the pull.

:::design[Target design: the cascade-resolved worklist, tracked in #489]
The server, not the template, decides placement (next) and resolves the cascade (config / `$var:`
values, effective `interval`, credentials) before handing the node concrete work. The node never
sees a template, only materialized, resolved task and command instances.
:::

### Config propagation (declared change to running node)

:::design[Target design: config propagation to a running node (`:apply` and the generation-driven cache), tracked in #489]
An interface's connection config (endpoint, snmp community, http auth header) is a **projection** of
the component's declared config through its template. The node re-pulls the worklist every tick but
**caches interface config for its process lifetime**, so a changed input must propagate:
**reconcile on the server** (a changed declared input, via `/components/{name}:apply` or a direct
config write, re-renders the affected interfaces from the *current* declared config and upserts
them, preserving placement) and **invalidate on the node** (the worklist reply carries a per-node
**`config_generation`**, the max `updated_at` across the interfaces the node polls; an advance drops
the node's interface cache and re-fetches this tick, a steady value serves from cache: a real change
lands within one tick, no restart). The generation moves at **operator-config pace, not telemetry
pace**: the sample-write path never touches `interface.updated_at` and a no-op re-apply does not
advance it, so nodes are never woken for nothing.
:::

## The node-server protocol

The edge is **outbound-only**: a node sits behind NAT, so the server never dials it. A node is a **NATS client over the WAN**, one authenticated outbound connection (an nkey/JWT credential bound to `node.name`); everything server-to-node arrives on subjects the node is permitted to consume. Three flows share the connection:

:::design[The full node NATS contract, per ADR-0036; delivery guarantees are #430]
- **Telemetry up**: the node **publishes** `TelemetryBatch` batches (`{samples, labels}` plus the
  `(task, ts)` envelope, [below](#shipping-samples)) to a **JetStream raw ingress subject**;
  JetStream acks each publish (at-least-once), a `Nats-Msg-Id` deduping replays (the admission
  consumer preserves it when republishing to the trusted stream).
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

### Delivery: at-least-once, idempotent by nature

The node publishes **at-least-once**, resuming unacked publishes on reconnect (JetStream ack plus
`Nats-Msg-Id` dedup); replay is safe **without a separate idempotency layer** because everything
the edge ships is idempotent by its own key: **samples** dedup on **`(series, ts)`** (an idempotent
upsert; the edge stamps `ts`, so the server is **ts-authoritative**, reorders out-of-order arrivals,
and needs **no strict ordering** on the wire), and **command results** are an idempotent status
update on a known `action` row by id. **Events are not shipped from the edge**: an event is
**derived server-side** (an `event_rule` over samples, or from a `log_line`,
[events](/architecture/events/)), so nothing needs dedup; not re-raising the same event
next poll is the **alarm** model's job (one stateful open alarm, fire and clear).
:::

### Buffering and retention are cascade settings

:::design[Target design: the edge buffer, tracked in #430; retention is #417]
When the server is unreachable the node **buffers in memory**, bounded, **not durable
at the edge**. Both the **buffer** (size, shed policy) and **retention** are **cascade-resolved**
([cascade](/architecture/cascade/)) with an install-wide `platform` binding, overridable down the
tree. A full buffer **sheds oldest metrics first and surfaces it** as a
`node.buffer` sample (depth, drops): visible, never silent.
:::

### Credentials at the edge

:::design[Target design: credentials at the edge, tracked in #489]
Worklist materialization resolves credentials server-side, so **device secrets travel to the
node** (over TLS), held **decrypt-on-use**: in memory, or encrypted at rest in a scratch dir with
the key from the [`SecretProvider`](/architecture/variables/), **never persisted in plaintext**,
scoped to the node's placement, re-fetched on the config-generation bump.
:::

### Enrollment

A node is **created server-side first** (its `node.name` and properties), the UI mints a **per-node enrollment token**, and the node **claims its identity** on first connect, exchanging the token for its **NATS credential** (a per-node JWT signed for its nkey, scoped to the subjects its placement allows, [identity and access](/architecture/identity-access/)).

:::design[Fleet auto-enrollment over a `discovery_rule`, tracked in #489]
Later, a **shared enrollment token** plus a **`discovery_rule`** auto-enrolls a fleet: the node's
own properties (stable facts, selected ENV) derive its name, editable server-side after deploy; a
rollout mints no per-node token.
:::

## Placement (ETL, cascaded)

:::design[Target design: cascaded placement, tracked in #489]
Collection follows **ETL**: extract **and transform** (including the extractor's Expr transform)
default to the **edge**; the shaped samples **load** to the server, where resolve / bind / calc /
evaluate default to **central**. Placement is a **cascaded property**
([cascade](/architecture/cascade/)): `placement: central` makes the **server itself the node
target** (cloud APIs, SaaS pollers, inbound webhooks). A listener endpoint lives where placement
puts it (the on-site node for LAN devices, the server for cloud sources), so a registered
callback URL resolves to the placed listener's address, never a hardcode.
:::

## Running tasks

:::design[Target design: edge normalization (locate + Expr) and the listen mode, tracked in #489]
Per task the node runs the protocol over the interface's connection, then **normalizes at the
edge**: the locate + Expr extraction ([collection](/architecture/collection/)) produces samples and
stamps labels (cascading union + override), keeping the wire bytes as `raw` only on a parse or
validation failure (for `collection.failed`) or under dev raw-mode. A task runs in one of the two
[collection](/architecture/collection/) modes, **poll** (on the resolved `interval`) or **listen**;
a held-open connection is a **stateful interface transport**, not a third task type. Both modes
assemble the same telemetry payload (below).
:::

The built interface types, their per-task params, and the fixed samples each emits are the collection **type catalog** ([interface types and their config](/architecture/collection/#interface-types-and-their-config)); this page covers how the node *executes* them: reachability gating, sessions, the task queue, tick scheduling.

## Sessions

::::design[Target design: sessions and the session pool, tracked in #489]
A stateful interface (`ssh`, `mqtt`, anything held open) becomes a **session** at runtime: one
connection keyed by `(node, interface)`, shared by every task and command under it, handshake and
auth paid once. A pool holds it open across ticks (reconnect, backoff, keepalive); a listener
runtime wakes on its inbound. The socket is ephemeral on the node, which **reports lifecycle
transitions as `session_log` rows**; the `session` entity is a server-side projection (a
view over `session_log`; [storage](/architecture/storage/)).

:::caution[Open question]
The exact `session` lifecycle state enum and pooling parameters (idle timeout, max lifetime, pool
size per interface, a shared versus dedicated session for a stream).
:::

Lifecycle: **establish** (connect, authenticate, subscribe if a stream rides the session),
**operate** (pollers and stream events over the held connection, demuxed, next), **recover**
(graceful retry with backoff, surfaced as a `session_log` error, never hammering: a rejected
credential risks lockout; a subscription is session-scoped, so a reconnect **re-subscribes**),
**teardown** (exit cleanly, set up again).

**Where failures land.** `session_log` owns **connection health** (cannot connect, auth rejected,
dropped, timeout); the **data event owns parse health**: a parse failure emits a `collection.failed`
event carrying the `raw` (plus the `action` row for commands), surfacing as an alertable
collection-health sample. A command timeout can touch both.
::::

## Inbound handling on a shared connection

:::design[Target design: the ordered matcher set, tracked in #489]
One connection carrying heterogeneous inbound frames cannot assume an arriving frame answers the
last command. Frames route through an **ordered matcher set**: every task
contributes a matcher (a poller's awaited-response shape, a listener/stream's `match:` predicate),
each frame tested **in order**, first match winning. While a poll is **outstanding** its response
matcher is tried first, then the standing matchers in declared order, so an event arriving mid-poll
falls through to its stream instead of being mis-eaten as the response; where the protocol itself
frames responses vs events (xAPI `*r` vs `*e`, a request id), framing drives routing and the regex
only extracts within the matched frame. An **unmatched** frame lands as `raw` (orphan, logged): a
missing matcher surfaces, never fails silently.
:::

## The component task queue

::::design[Target design: the component task queue, tracked in #489; the durable command queue is ADR-0036]
The node's work is the **component task queue** (distinct from the central **rule engine**;
[workers](/architecture/workers/)): **poll tasks** (produce samples) and **command tasks** (from
`run` actions, producing a caused `event` + `action`-row status), split by shape:

- **discrete tasks** (pollers, commands): request/response, **serialized into per-component lanes**.
  Component, not host, is the contention key: a server with two IPs is one component, and a reboot
  takes out both interfaces. A shared poller runs once on its parent and fans out at binding.
- **standing receivers** (listen tasks): always-on, event-driven, **not lane-serialized**, sharing a
  held session with pollers (demuxed) or owning their connection.

**Smart-wait gate.** After a disruptive command the lane blocks until reachability reports the host
back up, read from the node's **local** copy, not the sample store; a fixed timeout backstops.

Tasks within one interface run serially (one probe, then its tasks in order); only distinct
interfaces run concurrently.

:::caution[Open question]
Whether to add intra-interface concurrency, given that connection and order semantics differ per
protocol.
:::

The node-side queue is **not** durable: durability lives **server-side** (the JetStream command
queue, the cascade-configurable telemetry buffer). On reconnect the node re-pulls its worklist,
resumes its durable consumer on the command queue, and replays unacked telemetry publishes
(idempotent on `(series, ts)`).
::::

## Implicit reachability

Any interface with a host address gets reachability for free: the node pings the host and checks the declared port(s), continuously and out of band; a smart default, **bypassable per interface** (for endpoints that drop ICMP or have no port).

:::design[Target design: the layered availability gate, tracked in #489]
The results come back as `reachable` / `port_open` **samples** usable in rules and dashboards, and
feed the smart-wait gate from the node's local copy: the connection detector and the dashboard
signal are one always-on probe.

**The layered availability gate** is an **OSI-layered** set of cheap checks run as a **concurrent
pre-pass** (high concurrency, short timeouts) before a connection-interface's poll tasks. All
applicable checks run, each shipping a built-in sample, instanced (the ping by host, the rest by
interface) and owned by the queried component; the interface's **`interface-reachable`** verdict is
their AND.

| Layer | Check | Sample | Notes |
|---|---|---|---|
| L3 network | ICMP ping, **batched once per host** per tick | `icmp-reachable` / `icmp-rtt-avg` | **informational** (see verdict below); shared by every interface on the host |
| L4 transport | TCP connect (tcp-family) **or** UDP presence (snmp/UDP) | `tcp-open`/`tcp-connect-time` · `udp.open` | a closed UDP port answers ICMP port-unreachable, so absence of that is "present"; this is why SNMP's transport check is L4, not its auth-dependent get |
| L7 app | protocol handshake: SNMP `sysUpTime` get (**`snmp.reachable`**, default-on) · SSH handshake+auth · telnet login chain | (verdict) | the SNMP get is the **primary, default** SNMP liveness (ICMP-independent); SSH/telnet are **opt-in** (`ssh_check`/`telnet_check="on"`) because their liveness credential can differ from the device's |

**The verdict respects each layer's definitiveness.** A TCP connect and any L7 handshake are
**definitive**, so the **ping is informational** (an ICMP-filtered host still reads up); a UDP
"present" is a read timeout (open|filtered), ambiguous, and only the ping disambiguates it, so a
failed ping fails the verdict ONLY for an SNMP interface opted out of the L7 get (`snmp_check=off`),
the UDP probe its only signal (`pingGates`). A definitively down layer (TCP refused, UDP
ICMP-unreachable, an L7 auth/no-answer) fails the verdict regardless; an inconclusive probe (a
setup/resolve error, a missing credential) does not gate.

**Off gates.** Every check toggles via `params.<name>_check = "on" | "off"`; `params.liveness =
"off"` disables the whole gate. Defaults split by **auth dependence**: auth-independent checks ON,
opt-out (`ping_check`, `port_check`, `tls_check` when TLS lands); **`snmp_check` ON**, the one
auth-dependent exception (the get reuses the poll's community, so a failure means genuinely
unpollable, and it is the only ICMP-independent SNMP signal); **`ssh_check` / `telnet_check` OFF**,
opt-in (a differing liveness credential must not read as down).

The honest limit: a v2c wrong community is a **silent drop**, so a get failure alone cannot separate
down from wrong-community. Cross-referencing does: host pings + UDP not refused + get silent =
"reachable, SNMP not answering this community"; with ICMP fully blocked the inference is lost,
reading "host down or fully filtered." SSH verifies auth; telnet completes the `login:`/`Password:`
chain (service-up, not a verified shell). `params.liveness_oid` overrides the probe OID.

**Poller** tasks run only if the verdict is up; **listener** (`mode=listen`) tasks run ungated,
never pinged; **inline probes** (`icmp`/`tcp` with the host on the task) *are* the check, ungated. A
down interface's gate samples ship in **one** batched call. L5 (socket), L6 (TLS), and further L7
handshakes extend the stack: one `append` in `ifaceChecks`, gated by its own `_check` param.
:::

## Shipping samples

The node ships a native `TelemetryBatch`: `{ samples, labels }` plus an envelope (`task`, batch `ts`), **published to the JetStream raw ingress subject** (protobuf-encoded, the proto surviving as the NATS message schema).

:::design[Target design: the edge retry buffer, raw on failure, and the OTLP adapter, tracked in #430]
The publish is **buffered with retry/backoff**. On a parse or validation failure the node also ships
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

::::design[Target design: the tick scheduler, `node.self`, and the node-down sweep, tracked in #489 and #430; the seeded node rules are the `event_rule` (ADR-0050)]
A tick groups the worklist **by interface** and runs three phases: the L3 ping pre-pass (batched per
host), the gate-verdict pre-pass, the poll phase. The gate pre-passes run at a **high fixed
concurrency** (`gateConcurrency`), the poll phase across the **bounded poll pool** (default 16,
`--workers`), so the cheap gate is never throttled by a small `--workers` and a node facing many
dead targets is bounded by concurrency, not the serial sum of probe timeouts (a dead SNMP get costs
`timeout * (retries+1)`, via `--snmp-timeout` / `--snmp-retries`, default 3s x2). Each poll task gets
a per-task deadline (`--task-deadline`, default 30s).

:::caution[Open question]
Per-task schedule dispatch: the resolved `interval` exists, but honoring distinct per-task cadences
within one node tick is unsettled.
:::

The loop is **overrun-aware**: it reschedules relative to each tick's finish, not a fixed ticker; a
tick exceeding its interval is flagged and the next fires immediately, so falling behind
**surfaces** rather than silently dropping ticks.

Each tick the node publishes a `node.self` envelope: tick duration, task
attempted/ran/skipped/failed counts, interface probed/up/down counts, the `node.overrun` state. Not
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
