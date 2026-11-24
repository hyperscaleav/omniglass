---
title: Outposts
description: "How an outpost works: it pulls its worklist, runs each task on its own interval through its endpoint's lane, holds sessions, gates on reachability, and ships what it collects once."
sidebar:
  order: 32
  badge:
    text: Partial
    variant: note
---

An **outpost** is a collection process placed near the fleet that runs tasks and commands over
endpoints and reports back. It is the same `omniglass` binary in its `outpost run` mode. It pulls its
**worklist** from the server, runs the work on the spot, parses what it reads at the edge, and ships
the results over the bus. To operate outposts, see [the operator collection guide](/operator/collection/); what
an outpost runs is described by [collection](/architecture/collection/), and what it reports of its
own health by [self-monitoring](/architecture/self-monitoring/).

## The outpost

An outpost follows the identity triad. Its **`id`** is the immutable key every reference stores
(`endpoint.outpost_id`). Its **`name`** is its fleet address: the token in every bus subject it uses
and its enrollment identity, so it has no `:rename` (moving it would move a live subject) and the
`PATCH /outposts/{name}` body carries no name. Its **`label`** is the friendly string.

Its optional **location** is descriptive, not a scope: an outpost serves the whole fleet, and the
location clears if that location is deleted. The create and update still resolve the location within
the caller's own `location:read` scope, so a location outside it is refused as absent
([ADR-0089](/architecture/decisions/#adr-0089-a-uuid-is-the-address-a-dotted-path-is-a-positional-lookup)).

An outpost is a **taggable owner**: [tags](/architecture/tags/) whose `applies_to` includes `outpost`
bind to it directly, with no cascade. Creating an outpost (`outpost:create`) does not enroll it;
enrolling is a separate act (`outpost:enroll`, [below](#enrollment)). Decommissioning
(`DELETE /outposts/{name}`, `outpost:delete`) deletes the outpost and cascades its endpoints, their
derived tasks, its tags, its self-logs, its [platform component](/architecture/self-monitoring/) and
its credential; the telemetry it collected from components is untouched.

An outpost acts only for itself. Its bus credential is permitted only its own subjects, and the
ingest consumer confines what it lands to the tasks placed on it: a batch group for another outpost's
task is dropped, warned about once per outpost per minute and counted on the outpost's
`orphaned_batches` and `last_orphaned_at`.

### Serving its own simulated components

`omniglass outpost run --simulate` runs the [simulator](/architecture/simulation/#in-the-outposts-own-process)
in the outpost's own process: every component placed on the outpost whose product a profile simulates
is served on its own `127.60.0.x` loopback address, and the outpost collects from it as it would from
hardware. The simulator holds the outpost's identity, so there is no second enrollment
([ADR-0142](/architecture/decisions/#adr-0142-the-simulator-is-a-run-mode-beside-its-node-holding-that-nodes-identity)).
`--impairments`, `--port-offset` and `--poll` belong to the simulator and are refused without
`--simulate`; `--impairments` needs `NET_ADMIN`. The [lab](/developer/lab/) runs its outpost this way.

## Getting its instructions

The **worklist** is everything placed on the outpost, resolved by the server: each task with its
driver function baked (its request, schedule and emits, each emit's lane fixed at attach), its
driver's `session` block and ladder, its inputs, and the secrets that cascade onto its component
([ADR-0135](/architecture/decisions/#adr-0135-a-drivers-spec-is-data-one-engine-interprets-it)). The
outpost never sees a driver spec, and keeps no endpoint config apart from its last worklist.

A reply travels **grouped by endpoint**: an endpoint's fields (params, ladder, learned pin, last
rejection, connection fingerprint) ride once however many tasks run over it, and the task specs,
session blocks and credentials tasks share ride once in dictionaries a task names by index
([ADR-1583](/architecture/decisions/#adr-1583-a-nodes-worklist-travels-grouped-by-endpoint)). The
reply also carries a `config_generation`, the newest `updated_at` among the outpost's endpoints, which
moves at operator pace, never at telemetry pace.

:::design[A cadence an operator overrides, tracked in #1373]
Today a poll's interval is its driver's `schedule.every`. The target resolves every collection cadence
from a platform default, the driver's declaration and an operator's override down the cascade, and
the worklist carries the resolved interval.
:::

### Pulling the worklist

The pull runs on its own goroutine and interval, apart from the heartbeat (`--worklist-every`, 30 s by
default, each wait jittered within 10%), over request-reply on `og.v1.worklist.<outpost>`. A pull
that fails never stops the outpost
([ADR-1577](/architecture/decisions/#adr-1577-a-nodes-worklist-is-compressed-served-by-a-bounded-responder-and-retried-without-exiting)):

- **Retried, never fatal.** Every pull, the first included, retries on a jittered backoff from 1 s to
  a 30 s cap, each failure logged at WARN. After the first success a failed pull keeps the last good
  worklist, every task and session of it. Before it, the outpost still heartbeats and reports itself,
  so an outpost refused from the start reads as up with its pulls refused
  ([ADR-1882](/architecture/decisions/#adr-1882-a-node-heartbeats-and-counts-its-pulls-before-its-first-worklist)).
- **Counted, so a stale worklist reads as loss.** Keeping the last worklist hides the failure, so the
  outpost reports `worklist-pull-loss-ratio` every report window, and the seeded `worklist-stale` rule
  warns when it holds at 1 ([self-monitoring](/architecture/self-monitoring/#self-monitoring-every-outposts-account-of-itself)).
- **Failed whole by the server.** A worklist the server cannot read in full, or one still over the
  bus's `max_payload` once compressed, is answered with an `Omniglass-Error` header and no body. A
  database blip never reaches the outpost as a short worklist, which would tear down the sessions of
  the tasks it dropped.
- **Compressed past 64 KB.** A larger reply is gzipped and marked `Content-Encoding: gzip`.
- **Served from a bounded pool.** The outpost states its wait in an `Omniglass-Timeout` header. The
  server stamps each pull's arrival, queues it (up to 256) and answers it from a pool of workers (8 by
  default, held under the database pool's size). A pull past its deadline is dropped unread, one whose
  deadline passes mid-build is abandoned, and one that meets a full queue is refused at once as busy,
  so the outpost backs off rather than waiting out its budget.

The worklist grows only when an operator changes what the outpost runs or the outpost learns
something: a key pinned on first use or an endpoint's last rejection, each riding once on its
endpoint's entry.

:::design[The worklist syncs endpoint by endpoint against a manifest, tracked in #2606]
One reply still carries an outpost's whole worklist, so `max_payload` (1 MB) caps how much one outpost
can run: on the 3,000-system corporate seed, headquarters is about 463 KB gzipped. The target pulls a
**manifest** first (the generation, a set digest, and a content digest per endpoint), answered
`unchanged` with no body in the steady state; then asks by id for only the endpoints that changed, in
batches under the limit; and applies the new set once it holds every endpoint listed, running its last
worklist until then. An endpoint too large for any batch fails alone, by id
([ADR-1586](/architecture/decisions/#adr-1586-a-node-syncs-its-worklist-endpoint-by-endpoint-against-a-manifest)).
:::

### The change nudge

A scheduled pull alone leaves an edit waiting up to one interval. Instead, the commit of any write
that changes an outpost's worklist **nudges** that outpost, and it pulls at once
([ADR-1945](/architecture/decisions/#adr-1945-a-worklist-change-nudges-its-node-through-a-postgres-notify-on-commit)):

- **The write announces, in its transaction.** It issues a Postgres `NOTIFY` naming each outpost it
  touched, which Postgres delivers only on commit, once per outpost. The writers are an endpoint's
  create, edit, `:retry` or delete (an edit that moves an endpoint names both outposts); a driver
  change to its spec; a secret's create, delete or field edit (the outposts of every endpoint whose
  inputs name it and whose component it covers); a path dependency added or removed; a component
  rename or move; a location moved to a new parent; and a change to `collection.report_seconds`, which
  every worklist carries. A label-only edit nudges nothing.
- **The server forwards it.** The bus holds one listening database connection, outside the pool, and
  publishes an empty message on `og.v1.worklist-changed.<outpost>`. A dropped listener reconnects on a
  backoff; a change committed while it is down reaches its outpost on the next scheduled pull.
- **The outpost coalesces.** It subscribes before its first pull, folds a burst into one more pull, and
  pulls at most once every 200 ms. A backing-off pull is not cut short.

The nudge carries nothing, so a lost one costs only the wait for the next scheduled pull:
`--worklist-every` is how stale an outpost can be at worst, and the nudge is how fresh it is when
nothing is lost. Once a worklist lands, the outpost reconciles its held sessions and hands the new
tasks to its dispatcher at once, so a declared change is running within a second, with no restart and
nothing to apply. On the server, an endpoint edit or a driver spec change re-derives the affected tasks
in the same transaction, and a session whose
[connection fingerprint](/architecture/collection/#the-connection-fingerprint-and-the-layered-gate)
moved is recycled on the pull that carries it.

## The outpost-server protocol

The edge is **outbound-only**: an outpost often sits behind NAT, so the server never dials it. An
outpost is one authenticated client of the server's in-process NATS server, and every flow rides a
subject carrying its own name, which its credential alone is permitted:

| Subject | Shape | What |
|---|---|---|
| `og.v1.worklist.<outpost>` | request-reply | the worklist pull |
| `og.v1.worklist-changed.<outpost>` | server to outpost | the change nudge |
| `og.v1.heartbeat.<outpost>` | outpost to server | liveness; the server stamps `last_heartbeat_at` |
| `og.v1.telemetry.<outpost>` | JetStream publish | telemetry batches, [below](#delivery-a-batch-lands-once) |
| `og.v1.command.<outpost>` | request-reply | the command queue pull |
| `og.v1.commandstatus.<outpost>` | outpost to server | a command's outcome |
| `og.v1.run.<outpost>` | server to outpost, request-reply | an on-demand run, [below](#on-demand-runs-asked-answered-once) |

The credential is a shared secret: the enrollment token is exchanged for the outpost's bus password.
The decentralized nkey and JWT model is deferred
([ADR-0036](/architecture/decisions/#adr-0036-a-node-is-a-kindnode-principal-with-an-interim-bearer-credential-and-static-per-connection-nats-subject-permissions)).

### Commands: a durable server queue, a stateless edge

A command is issued server-side and queued in the database; the edge holds nothing durable. The outpost
pulls its pending commands, rendered for it over the driver's binding, on its heartbeat, actuates them
one at a time through each endpoint's lane, and reports each outcome. The pull itself marks a command
dispatched, and one never reported is redelivered or expires, so a restarted outpost loses nothing
([ADR-0136](/architecture/decisions/#adr-0136-actuation-is-a-per-node-pull-rendered-at-dispatch),
[queue health](/architecture/commands/#queue-health-each-outposts-command-queue-measured)).

:::design[A command runs the moment it is issued, tracked in #1635]
The server wakes the outpost when a command is issued, with the pull as the fallback, and the command
takes a bounded command pool through its endpoint's lane, so no command waits on the heartbeat, and the
heartbeat publishes liveness and nothing else.
:::

### Delivery: a batch lands once

The outpost publishes **at-least-once** and the platform lands each batch **once**
([ADR-0453](/architecture/decisions/#adr-0453-telemetry-is-published-through-jetstream-with-an-ack-a-minted-nats-msg-id-and-a-bounded-spool),
[ADR-0311](/architecture/decisions/#adr-0311-a-telemetry-batch-lands-once-in-one-transaction-under-its-receipt)).

- **One spool.** Every telemetry publish (a task's samples, a collection failure, a session
  transition, the self-logs) goes through one spool. A task's result joins as a **group**, and the
  spool coalesces pending groups into one batch: it flushes after a second, or sooner when the next
  group would pass the batch's 512 KiB budget. A session's pushed samples flush at once.
- **Fixed at the flush.** The flush marshals the batch once and mints its `Nats-Msg-Id` (a UUIDv7), so
  every retry ships the same bytes under the same id. A batch counts as sent only once the stream
  acks it.
- **In order, never blocking.** One shipper keeps up to 32 batches in flight with the stream, in
  order, so no caller waits on the stream. A batch not acked stays at the spool's head and ships on the
  next tick under the same id. A duplicate inside the stream's five-minute window is stored once, and
  past it the server's batch **receipt** keeps it from landing twice
  ([messaging](/architecture/messaging/#delivery-a-batch-lands-once)). The connection buffers nothing
  across a reconnect and reconnects without limit, so order holds, which the property lane's
  transition guard depends on.
- **Bounded, and told.** The spool is memory, capped at 10,000 batches, 16 MiB or 12 hours. Past a cap
  it drops the oldest batch not in flight, says so once when it starts and once with the count when it
  drains, and makes the next tick resend current state (each held session's state and auth result,
  each listener's faults), so the server never keeps showing a session up that went down meanwhile.
  The 12-hour cap sits inside the receipt's 24-hour horizon. A batch the bus can never take (over its
  payload limit) is dropped and told rather than blocking every batch behind it.

Every batch carries a **delivery ledger**: the spool's boot id, a dense `seq`, the items of each kind
it carries and the items minted before it, and the highest seq below which nothing is still queued. A
batch that never lands leaves a hole the server counts by kind. The ledger also carries the outpost's
own losses: a drop at a cap or a bus refusal as a run of seqs with its cause, and items lost before
any batch carried them (self-log lines the bounded buffer dropped, a group that failed to encode) as
running totals. So the read can tell the outpost hop from the ingest hop
([the loss ratio](/architecture/messaging/#accounting-every-item-the-loss-ratio)).

:::design[A killed boot's unshipped tail is counted, tracked in #1733]
A restart mints a new boot, so batches still spooled when an outpost is killed can never settle. The
read names that boot unshipped and reads it as far as it landed, but cannot count what it held.
:::

### On-demand runs: asked, answered once

The server can ask an outpost to run work now on `og.v1.run.<outpost>`, a request the outpost answers
once ([ADR-1163](/architecture/decisions/#adr-1163-a-poll-runs-once-on-demand-through-its-node-as-a-preview-that-stores-nothing-or-a-run-now-that-lands)).
Nothing is queued: an outpost not connected is an answer (the API's 503), one that does not answer in
the run's budget another (504), and a run past the operator queue's cap is refused at once (429).

- A **preview** stores nothing and touches no held session; its excerpt is scrubbed of the credential.
  It takes the lane of the endpoint its transport and target name, or runs off-lane when it names none.
- A **run now** takes its endpoint's lane ahead of the scheduled work, borrows the endpoint's session,
  honors its gate, and publishes exactly as a scheduled run would; the schedule is untouched.

### Credentials at the edge

The server resolves an endpoint's secrets down the cascade into the worklist, so device secrets
travel to the outpost, confined to the components placed on it. The outpost holds them in memory only
and never writes them; a rotated secret moves the connection fingerprint and recycles the session
([ADR-0158](/architecture/decisions/#adr-0158-one-connection-fingerprint-gates-every-connection-on-a-ladder-the-driver-declares)).

:::design[Secrets ride a TLS bus and are decrypted on use, tracked in #1250]
The secrets travel over TLS and stay sealed until the moment a connection needs them, never held in
plaintext longer than that use.
:::

### Enrollment

An outpost is created server-side first. Pressing **Enroll** mints a per-outpost enrollment token
(`POST /outposts/{name}:enroll`), and the outpost claims its identity on first connect
(`POST /outposts:claim`), exchanging the token for its bus credential.

:::design[Fleet auto-enrollment, tracked in #1025]
A shared enrollment token plus a discovery rule enrolls a rollout of outposts with no token per
outpost: each derives its name from its own stable facts, editable server-side after deploy.
:::

## Placement

A listener runs where its transport puts it
([ADR-1024](/architecture/decisions/#adr-1024-omniglass-receives-webhooks-on-the-server-and-a-webhook-token-is-a-sealed-secret)).
A **webhook** is received by the **server**, which a cloud sender reaches over public HTTPS and an
outpost behind NAT cannot, so a webhook endpoint takes no outpost and its URL is the server's
(`/api/v1/webhooks/<token>`). The pushes that should not cross a WAN (syslog, SNMP traps, tcp and udp
on a shared port) are received by an **outpost** beside the gear.

:::design[The cross pairings, tracked in #1157 and #1158]
An outpost hosts a webhook listener for senders on its LAN (#1157), and the server hosts syslog, trap
and shared-port listeners for a sender with no outpost near it (#1158), one listener primitive serving
both placements.
:::

## Load distribution across a site's outposts

Today an endpoint runs on the outpost its `outpost_id` names, set by hand. A site with two outposts
splits its components only as an operator splits them, and a lost outpost's endpoints wait for someone
to re-point them.

:::design[A site's components spread across its outposts, tracked in #1687, #1688 and #1689]
The design is [ADR-1638](/architecture/decisions/#adr-1638-a-sites-components-spread-across-its-nodes-by-expected-cost-guarded-by-measured-headroom):

- **The unit is the component** (#1687): every outpost-run endpoint of one component runs on one
  outpost, since they share a device, a gate and its exclusion. A server-hosted push endpoint stays
  outside the unit.
- **The pool is the location**: a component's candidates are the claimed outposts at its location, or
  at its nearest ancestor that has any. An endpoint's explicit `outpost_id` stays a **pin** for the
  whole component. Every outpost in a pool must reach every device in it; the operator builds the pool
  so it holds. Pushes over a shared port land on any outpost in the pool, claimed by source address.
- **Expected cost balances, measured headroom guards** (#1688): a component costs the sum over its
  tasks of runs a second (a listen task at a flagged weight), and an outpost whose schedule loss or
  `worker-busy-fraction` is over a ceiling takes no new component. A server reconcile applies a pure
  `assign`: pins win, assignments are sticky, new components go to the least-loaded outpost,
  dependency neighbours pull together, and rebalancing moves past a hysteresis, capped per pass.
- **Loss and join** (#1689): a lost outpost's components spread over its unsaturated peers after the
  silent window plus a grace; a joining outpost fills gradually. A move between live outposts is
  two-phase, confirmed by the generation the old outpost reports running, so two live outposts never
  hold one single-session device.
:::

## Running tasks

A task runs in one of two modes, **poll** (on its schedule) or **listen** (claiming what its endpoint's
held session or socket delivers); a held connection is a property of the endpoint's transport, not a
third mode ([collection](/architecture/collection/)). Either way the outpost locates and transforms
each emit at the edge and lands a `collection-failed` event for anything that does not parse.

Each kind of work runs on its own timer and its own bounded pool, so a slow device holds only its own
endpoint's lane, never the heartbeat or another endpoint's work
([ADR-1637](/architecture/decisions/#adr-1637-a-node-runs-each-kind-of-work-on-its-own-timer-and-pool-one-request-per-endpoint-at-a-time)):

| Kind | When it runs | Where it runs |
|---|---|---|
| heartbeat | `--heartbeat`, 30 s by default | publishes liveness, pulls commands, ships the spool, takes the dispatcher's window |
| worklist pull | `--worklist-every`, or at once on a nudge | the pull goroutine |
| polls and probes | each task's own interval, from the dispatcher | the lane pool, through the endpoint's lane |
| commands | pulled on the heartbeat | one at a time, each through its endpoint's lane |
| held sessions | while the worklist holds them, reconciled the moment it lands | one supervised goroutine each |
| housekeeping | the shorter of the report window and the heartbeat | unclaimed lines, push counts, the collection report |

:::design[Cascaded labels at the edge and raw bytes on failure, tracked in #1253 and #1251]
The outpost stamps each sample with its endpoint's cascaded labels (#1253), and a `collection-failed`
event carries the raw wire bytes that did not parse (#1251).
:::

## Sessions

An endpoint whose worklist carries a standing listen task, or a poll over a transport or driver that
needs a held connection, gets a **session**: one connection held across ticks, keyed by the endpoint,
that its polls and commands borrow instead of dialing per run. Its machine (establish, authenticate,
operate, recover, teardown), its states and its backoff are on
[collection](/architecture/collection/#sessions); auth and rejection, and the gate a refusal closes,
are there too ([auth and rejection](/architecture/collection/#auth-and-rejection)). There is no session
table: each transition lands as the `session-state` property on the endpoint's component, with
`session-reason` and an outpost self-log
([ADR-0150](/architecture/decisions/#adr-0150-a-held-sessions-state-is-a-property-series-and-node-logs-not-a-session-table)).

How the outpost runs its sessions
([ADR-1636](/architecture/decisions/#adr-1636-held-sessions-reconcile-on-arrival-are-supervised-and-dial-through-node-wide-slots)):

- **Reconcile on arrival.** A worklist reconciles the held sessions and datagram sockets the moment it
  lands: added, removed, re-addressed or re-keyed endpoints are started, torn down or recycled then. A
  timed reconcile at the heartbeat interval is the safety net. Every reconcile runs on the outpost's one
  run-loop goroutine, the session maps' one writer.
- **Supervised.** Every session goroutine runs under a recover. A panic is logged with its stack,
  counted, and becomes that session's ordinary failure (`recovering`, reason `panic: <value>`), so a
  panic on one device's bytes costs that one session, never the outpost.
- **Bounded establish.** First dials and handshakes share outpost-wide slots (`--session-dials`, 16 by
  default), so an outpost restarting with hundreds of held endpoints opens them a few at a time. A
  redial takes no slot, so endpoints that never answer cannot hold up a healthy session's recovery.
- **Housekeeping on its own clock.** Unclaimed lines and push counts are taken on a grid at the report
  window, so a late pass never stretches the window that follows.

A **tls** endpoint keeps one public key pin, shared by its session and its probe and carried across
recycles: trusted on first use and published as `tls-certificate`, replaced by a pin the worklist
delivers, forgotten when the worklist withdraws it (the ssh host key likewise)
([tls](/architecture/transports/#tls)).

:::design[A session's pool knobs and the held transports still to come, tracked in #1254, #1036, #1039, #1040 and #1041]
A held session's reconnect, backoff and keepalive are fixed today; its pool knobs (idle timeout,
maximum lifetime, more than one connection per endpoint) are declared per driver (#1254). The held
transports beyond the line protocol, the http stream and the websocket are each their own issue: mqtt
(#1036), modbus/tcp (#1039), bacnet/ip (#1040) and knxnet/ip (#1041)
([ADR-0153](/architecture/decisions/#adr-0153-the-held-session-proof-is-the-line-protocol-over-tcp-other-held-transports-are-their-own-issues)).
:::

## Inbound handling on a shared connection

One reader per held connection routes every inbound message: **listeners first**, then the exchange in
flight by its declared reply shape (or, with none declared, the first unclaimed message), then
unclaimed, counted once a report window as the endpoint's `push-unclaimed-count`
([ADR-0151](/architecture/decisions/#adr-0151-a-held-line-session-routes-every-inbound-line-listeners-first-answers-by-reply-shape),
[listeners over a held session](/architecture/collection/#listeners-over-a-held-session)). A websocket
exchange is matched by an id the outpost mints into it instead.

:::design[A driver's declared prompt delimits a held answer, tracked in #1057]
Where a device frames its answers with a prompt, the driver declares it and the outpost reads an answer
up to that prompt, rather than taking one line.
:::

## The endpoint lane

Every discrete request an outpost makes rides **its endpoint's lane** on one bounded worker pool
([ADR-1249](/architecture/decisions/#adr-1249-the-endpoint-lane-runs-one-request-at-a-time-operator-work-first)).
A lane is the unit of exclusion: one request in flight per endpoint, while two endpoints, of one
component or of fifty, run side by side.

- **Discrete work** rides the lane: scheduled polls and probes, and an operator's commands, run nows
  and previews. Operator work waits ahead of the scheduled work on the same lane; nothing is
  preempted.
- **Standing receivers** (listen tasks) never take a lane: a listen task is what holds the session the
  lane's polls borrow.
- **Each job asks the gate as it is taken.** A job consults the endpoint's
  [gate](#implicit-reachability) first, so work queued behind a request that closed it sends nothing: a
  command fails naming the reason, a run now or preview answers 409, and a scheduled poll is held and
  counted. Where several reasons hold, the most causal answers: `upstream`, then `disrupted`, then
  `refused`, then `unreachable`.
- **Overload is counted, never queued without bound.** A due whose last run is still in flight is
  skipped and counted. Operator work across every lane is capped at 64: past it a run now or preview
  answers 429, and the outpost counts offers against refusals in its window.

The outpost's queues are not durable: on reconnect it re-pulls its worklist and commands and ships its
spool, each batch landing once by its receipt.

:::design[Component-wide exclusion and a session's pool size, tracked in #1649 and #1254]
A driver whose device cannot take two endpoints' requests at once keys its component's lanes on the
component (#1649). More than one request in flight to one endpoint comes only from a held session's
declared pool size (#1254).
:::

### Sizing the pool

With `--lane-workers auto`, the default, an outpost sizes its pool from the CPU and memory it may use
([ADR-1869](/architecture/decisions/#adr-1869-a-node-sizes-its-lane-pool-from-its-cpu-and-memory)).
The CPU count alone is the wrong measure, because a run spends most of its time waiting on its device.
The outpost reads its limits from its cgroup (`cpu.max`, `memory.max`, the tightest up the tree) and its
host (`GOMAXPROCS`, `MemTotal`), takes the tighter of each, and then the tighter of two bounds:

- **CPU**: half the CPUs times a run's whole time over its CPU time; at 4.4 ms of CPU waiting 200 ms,
  about 23 lanes a CPU.
- **Memory**: a quarter of the memory, at 1 MiB a lane.

The result is clamped to 4 to 256; a number sets the pool outright (`--lane-workers`, or
`OMNIGLASS_LANE_WORKERS`). The outpost logs the count and the limits it read at boot, and reports it as
`lane-worker-count` beside `worker-busy-fraction`. A cgroup v1 memory limit is not read. The
[lab](/developer/lab/) caps each outpost container at one CPU, so a scale run's parallelism is one
known figure rather than the host's.

## Implicit reachability

Every endpoint with a host address gets reachability for free: the outpost's **probe** runs the
endpoint's ladder on its own interval (`--probe-every`, 30 s by default; keep it under the console's
150 s staleness window). Its rungs and their samples are on
[collection](/architecture/collection/#the-connection-fingerprint-and-the-layered-gate): the ping is
**informational** and shared once per host per probe slot, the transport rung (a connect, a handshake,
or for udp and snmp a request answered) is the gate, and the session rung is the login. The endpoint's
`endpoint-reachable` is their AND.

The probe feeds the endpoint's **gate**, which holds a **set of reasons** and is open only when the
set is empty
([ADR-1650](/architecture/decisions/#adr-1650-an-endpoint-gate-holds-a-set-of-reasons)):

| Reason | Added by | Released by |
|---|---|---|
| `refused` | a terminal refusal on a rung | a fingerprint change, an operator's `:retry`, or the rung passing |
| `unreachable` | two consecutive down probe readings | the next up reading |
| `disrupted` | a disruptive command, before it is sent, on every endpoint of its hold set | after the quiet period, each endpoint's first up reading from a probe begun after it; the hold backstop releases the rest |
| `upstream` | a closed gate on a component the endpoint is reached through | the upstream opening, then the endpoint's own first up reading |

- **Unreachable.** One lost probe never stops collection, and a rung down only because a refusal
  holds it does not count. While it holds, a due poll is **held**: nothing sent, no failure and no
  backoff booked, counted as `poll-held-count`, since a poll the outpost chose not to send is not a
  lost sample. Held sessions and listeners are not gated by it; their own reconnect handles a gone
  device.
- **Disrupted.** A [disruptive command](/architecture/commands/#the-command_type-catalog) arrives with
  its [hold set](/architecture/commands/#the-hold-set-follows-dependency-edges), and the outpost closes
  each of those gates before it sends. For the type's quiet period nothing runs against them; then each
  reopens on its own first up reading, and the type's `hold_seconds` backstop reopens the rest, counted
  as `hold-expiry-count`, so a device that never came back is seen
  ([ADR-1651](/architecture/decisions/#adr-1651-a-disruptive-command-closes-its-endpoints-gates)).
- **Upstream.** A component reached through another (a `path`
  [dependency edge](/architecture/core-entities/#dependency-edges-what-a-component-needs-apart-from-where-it-sits))
  is held while any endpoint of its upstream has a closed gate, so a gateway going down raises one
  root cause, not a storm behind it. The reason names the root of the closure, derived by one pure
  function whenever any reason set moves, and a path cycle never holds itself up. An out-of-band
  endpoint is never held
  ([ADR-1652](/architecture/decisions/#adr-1652-a-path-upstream-holds-its-dependents-on-the-node)).

The gate is published as `endpoint-rejection` and carried back on the worklist, so a restarted outpost
adopts a refusal and an upstream hold rather than retrying against an unchanged fingerprint; it does
not adopt `unreachable` or `disrupted`, which the probe re-earns.

:::design[Gates across outposts and a session's upstream wait, tracked in #1655 and #1703]
An upstream, a hold set or a preview on another outpost is not followed today (#1655), and a held
session's own redial does not yet wait out its path upstream's hold (#1703).
:::

:::design[The ping's rtt and loss as observations, tracked in #1063]
Each echo lands as one observation and a lost echo as one with outcome `lost`, and rtt, variation and
packet loss derive from them. Today the shared ping reports `icmp-reachable` and an averaged
`icmp-rtt-avg`, and no loss.
:::

## Shipping samples

The outpost ships a `TelemetryBatch` (generated from `proto/og/v1/telemetry.proto`) on its own
telemetry subject: a **task group** per task it ran (the task, its observed time, and its metric,
property, event and log lanes), the outpost's own self-log lines, and its
[outpost-self block](/architecture/self-monitoring/#self-monitoring-every-outposts-account-of-itself).
The outpost stamps no component identity: the server binds each group's owner from its task's endpoint
and lands it under the outpost's confinement. Batches are held and retried until the stream acks them
([delivery](#delivery-a-batch-lands-once)).

## Scheduling, concurrency, and self-observability

Each poll and probe runs on **its own interval**, from a dispatcher with a timer of its own, not on the
heartbeat. Every task sits once in a due-time heap; the dispatcher sleeps until the earliest due, hands
it to its endpoint's lane, and books its next due
([ADR-1634](/architecture/decisions/#adr-1634-each-poll-and-probe-runs-on-its-own-interval-from-a-dispatcher-of-its-own)). A driver
poll's interval is its `schedule.every` (the heartbeat interval when it declares none); a probe's is
`--probe-every`.

- **A stable phase.** A task's dues fall on a grid of its own, offset by a hash of its id, so a
  restarted outpost lands each task on the same slots and a fleet of tasks with one interval spreads
  across it rather than firing together. A task new to the worklist, or whose connection fingerprint
  moved, runs at once.
- **Skip, not burst.** A task still in flight at its next due is skipped and counted, never queued
  behind itself, and a late pass runs a task once and books the next slot ahead of now.
- **Outcomes book on the grid.** A failed fetch retries on a backoff that doubles from a second up to
  the interval (a device's `Retry-After` is a floor); a terminal refusal waits its interval unless its
  session comes back up; a poll its gate holds is counted held and runs the moment the gate opens.

What the outpost reports of its schedule: per endpoint, `poll-due-count` and `poll-skipped-count` each
report window (skipped over due is the endpoint's schedule loss); and of itself, the longest pass, the
dues that met their run in flight, the worst lateness and gap excess, and the pool's busy share, as
platform metrics ([self-monitoring](/architecture/self-monitoring/#self-monitoring-every-outposts-account-of-itself)).
The window's high-water marks (`worker-busy-count`, `dispatch-queue-depth`) are said in a
`dispatch window` self-log line, only for a window that skipped a due or refused an operator's request,
so an outpost keeping its schedule logs nothing.

:::design[Probes apart from the pool and a per-task deadline, tracked in #1624]
Probes run at a high fixed concurrency with short timeouts, so the cheap gate is never throttled by a
small lane pool, and each poll task gets a deadline of its own.
:::
