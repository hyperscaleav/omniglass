---
title: Messaging
description: "The internal and edge NATS subject contract, the sibling to the public API: JetStream streams and consumers, the two lanes, request-reply, KV, the live UI relay, and per-tenant subject isolation."
sidebar:
  badge:
    text: Partial
    variant: note
---

:::note[Implementation status]
Built today: the embedded nats-server with JetStream (`internal/bus/server.go`), per-node subject
isolation with the auth callback, one `OG_TELEMETRY` stream on the node subjects
(`og.v1.telemetry.*`) and the API push subject (`og.v1.api.telemetry`), the single durable
`og-telemetry-worker` consumer writing straight to Postgres, and the worklist and heartbeat
request-reply lanes. The two-lane split below is target design; today there is **one stream and one
serial consumer doing admission (owner confinement) and persistence inline**.
:::

This page is the **one home of the two-lane data plane**: the admission / trusted / persistence split, the CDC publisher, and the ingest-path enumeration live here, only named (with a link) everywhere else.

Omniglass has **two typed contracts**: the [public API](/architecture/api/) (HTTP and OpenAPI) and this sibling, the **internal and edge transport**, a **NATS subject contract** over JetStream carrying service-to-service traffic, the edge, and the live UI. **Postgres stays the system of record; NATS moves.** Deployment topology: [scaling](/architecture/scaling/).

:::design[Target design, tracked in #430]

## Two lanes, one bus

Internal traffic splits by what is moving:

- **Data lane (NATS-native): samples.** Untrusted publishers (a node, an external webhook sender)
  publish to a **raw ingress subject** (the wire unit is a `Sample` in a `TelemetryBatch`); an
  **admission consumer** owner-confines each sample and re-publishes only confined ones to the
  **trusted** samples stream. The confinement set is **per publisher class**: a node's payload owner
  is checked against its placement `visible_set`, a central webhook's against the interface's
  declared owner (the trusted server-set `interface` label). The republish copies the original
  `Nats-Msg-Id`, `correlation_id`, and `source_event_id` headers verbatim, so dedup survives the
  hop. **Trusted server-internal producers publish straight to the trusted stream**, no admission
  pass: calc output (owner from the validated `calc_rule` scope) and the action layer's intended
  write (owner from the command target). The rule engine consumes the trusted stream directly; a
  **persistence consumer** batch-writes it to the Postgres `metric`, `property`, `event`, and
  `log_line` tables as an async sink, idempotent on `(series, ts)`, and the sink never gates the
  rule engine: a slow persistence consumer holds up only the durable record, never the live signal.
  Confinement is at **consume time, ahead of evaluation**: a forged owner must be dropped before it
  can open an alarm, not just before it is persisted. The admission consumer runs in **system mode**
  (a dropped sample is logged as a discovery candidate,
  [identity and access](/architecture/identity-access/)). Samples do not go through CDC; they are
  already on the bus.
- **Record / state lane (Postgres-first, CDC-out): events, alarms, actions, operator mutations**
  (config, ack, snooze, settings, manual commands). Born in a Postgres transaction: a firing
  `event_rule` writes the event plus the alarm transition atomically (serialized per
  `(event_rule, owner)`), and the API writes config, ack, and settings the same way. A
  **leader-elected CDC publisher** (exactly one active, failing over on the NATS KV lock,
  [workers](/architecture/workers/)) reads committed changes from a replication slot by **logical
  decoding of the WAL** and publishes each to JetStream, where `action_rule`, reconcile, and
  projection consumers react. Delivery is at-least-once with an **idempotency key per change**, so
  outcomes stay exactly-once downstream. **No dual-write**, no row-lock single-fire worklist, no
  `LISTEN`/`NOTIFY` fan-out: the change commits once and the bridge fans it out. The
  replication **slot and publication are ensured in the idempotent boot phase** (created if absent,
  untouched if present), never a run-once dbmate migration, so a fresh database and an existing one
  converge.

:::

## The three ingest paths

Three ingest paths exist today, and this list is the enumeration's one home:

- **The node bus path.** A node publishes a `TelemetryBatch` on its own subject (`og.v1.telemetry.<node>`), and the server binds each sample's owner from the task's interface ([collection](/architecture/collection/)).
- **The API push path.** `POST /telemetry:push` is the first-party HTTP ingest write: a scoped caller declares the owner and the route's scope check is the fence. The API publishes the batch onto the bus (`og.v1.api.telemetry`, trusted by subject, below) rather than writing Postgres directly, so pushed records are visible to the same stream consumers and land in history the same way ([API](/architecture/api/)).
- **The raw log path.** A raw log line rides either transport in the same batch but lands on its own untyped lane, `log_line`: no property name, no registry gate ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)).

The two transports meet at one `land()` write path, so neither can drift from the other's semantics; the mechanics of `land()` (reject-not-project, the transition-only state guard, the current-value derive) belong to [samples](/architecture/properties/).

## Streams and consumers

As built today there is **one stream**, `OG_TELEMETRY`, bound to the node subjects (`og.v1.telemetry.*`) and the API push subject (`og.v1.api.telemetry`), consumed by the single durable `og-telemetry-worker`; `og.v1.telemetry.<node>` is the sample firehose itself, carrying each node's samples and its raw self-logs. The set below is the target topology:

:::design[Target design, tracked in #430]
- **samples** (data lane): raw ingress, the **admission consumer** (owner-confines per publisher
  class), then the **trusted** samples stream the rule engine, calc, and the persistence consumer
  read; trusted server producers publish directly. A **work-queue consumer group** scales
  horizontally (each message to exactly one consumer), no leader.
- **records** (events, alarms, actions): published by the CDC publisher from Postgres commits;
  consumed by `action_rule`, reconcile, and projection consumers.
- **commands**: a durable, per-node **command queue** the edge holds a consumer on
  ([nodes](/architecture/nodes/)).

Durable consumers track their own position; delivery is at-least-once with `Nats-Msg-Id` dedup plus
double ack, which with the idempotent sinks (a sample on `(series, ts)`, an action transition on
`(alarm, action, transition)`, the CDC idempotency key) gives exactly-once **outcomes**. This triple
(`Nats-Msg-Id` dedup, double ack, idempotent sink) is the canonical exactly-once mechanism the other
pages refer to.
:::

The edge stamps `ts`, so the system is ts-authoritative and needs no strict ordering on the wire. Today's delivery contract is weaker than the target: node publishes are fire-and-forget core NATS (no publish ack, no `Nats-Msg-Id`) and the consumer acks once after multi-transaction writes, so delivery is at-least-once with a known duplicate risk until #430 lands (see #430 and #311).

:::design[Target design, tracked in #430]

## The pipeline, end to end

```d2
direction: down
classes: {
  node: { style.border-radius: 8 }
  key: { style: { border-radius: 8; bold: true } }
  group: { style.border-radius: 8 }
}
edge: "Edge (node)" {
  class: group
  task: "task\npoll · listen\nstateless / stateful" { class: node }
  fn: "function\nextract → key → normalize" { class: node }
  task -> fn
}
raw: "raw ingress\nnode · webhook (untrusted)" { class: node }
admit: "admission consumer\nowner-confine per class\n(system mode)" { class: node }
ds: "JetStream\ntrusted samples stream" { class: node; shape: queue }
failed: "collection.failed\n(carries raw)" { class: node }
calc: "calc_rule consumer\ncross-key · system-level" { class: node }
erule: "event_rule consumer\nfire_criteria (+ optional clear_criteria)" { class: node }
persist: "persistence consumer\nbatch sink (async)" { class: node }
tables: "metric · state · log\nsample tables" { class: node; shape: cylinder }
sched: "schedule + timer\n(leader-elected clock)" { class: node }
pg: "event · alarm\n(PG)" { class: node; shape: cylinder }
alarm: "alarm\none incident · new row per open\n(event_rule, owner)" { class: node }
cdc: "JetStream\nrecord/state lane" { class: node; shape: queue }
actions: "action_rule consumer\nnotify · command\nremediate-verify-escalate" { class: node }
itsm: "ITSM (action target)" { class: node }
operator: operator { class: node }
config: "config\ndeclared (spec)" { class: node }
audit: audit_log { class: key }
divergence: divergence { class: node; shape: hexagon }
edge.fn -> raw: "observed · lineage on row\n(source_rule)"
edge.fn -> failed: "parse / validation fail" { style.stroke-dash: 4 }
raw -> admit
admit -> ds: "confined"
ds -> calc
calc -> ds: "calculated · trusted producer\n(direct, no admission)"
ds -> erule
ds -> persist
persist -> tables: "durable copy"
sched -> erule: "origin=scheduled"
erule -> pg: "PG-first: event + alarm in one tx"
pg -> alarm: "alarm transition"
pg -> cdc: "CDC (logical decoding)\nleader-elected publisher" { style.stroke-width: 3 }
cdc -> actions
actions -> ds: "command's effect · provenance=intended\n(trusted, direct)" { style.stroke-dash: 4 }
actions -> itsm: "ITSM: open->ticket · update->comment · resolve->close" { style.stroke-dash: 4 }
actions -> edge.task: "command + adaptive poll" { style.stroke-dash: 4 }
operator -> config: "declares (PG-first)"
config -- tables: "links · drift" { style.stroke-dash: 4 }
operator -> audit: "audit" { style.stroke-dash: 4 }
cdc -- divergence: "disagree(A,B): drift / conflict" { style.stroke-dash: 4 }
```

The two lanes, drawn end to end. On the data lane, the edge parses payloads into observed samples
and publishes them to raw ingress; the admission consumer republishes confined points to the trusted
stream; the `event_rule` consumer evaluates live off the trusted stream while the persistence
consumer sinks the durable copy; calc output and a command's intended write enter the trusted stream
directly. On the record lane, an `event_rule` fire writes the event and alarm transition to PG in
one transaction and the CDC publisher fans the commit onto JetStream, where `action_rule` consumers
react; a command's intended sample then re-enters the data lane on the device round trip. The teal
node is `audit_log`, the ground-truth record of operator writes (including config changes); observed
and calculated samples carry `source_rule` on the row, and intended names its command via
`command_id`. The raw payload is not stored: a parse or validation failure rides a
`collection.failed` event. [config](/architecture/variables/) holds declared intent (PG-first),
keyed to a property sample as its observed side.

:::

## Subjects, accounts, and scope

Subjects are hierarchical and **scope is expressed in them**, not bolted on:

:::design[Per-tenant NATS accounts, tracked in #529]
- **Tenant = one NATS account.** Per-account isolation (messaging) is the same boundary as
  per-database isolation (storage): no shared subjects, no shared rows
  ([identity and access](/architecture/identity-access/)).
:::

- **The API telemetry lane (`og.v1.api.telemetry`) is trusted by subject.** A first-party push (`POST /telemetry:push`) is authorized at the route, so the API publishes as a **trusted server producer** with no admission pass, and the ingest consumer believes the owner the batch carries **because of the subject it arrived on**, never because the field is populated. A batch on `og.v1.telemetry.*` that asserts an owner is dropped. Only the server's own credential can reach this subject: a node's grant is an explicit allow-list of its own three subjects, and the lane sits **outside** the single-token `og.v1.telemetry.*` wildcard.
- **Subject permissions gate the subject string.** A node may publish and subscribe only the subjects for its placement; the grant is **mechanically derived from placement**, a coarse transport gate, not a second copy of the ABAC model. **Operators never connect to the bus** (see the live UI relay below).

:::design[Target design, tracked in #430]
A sample's owner lives in the **payload** (a multi-owner function resolves owner from labels), which
subject permissions cannot see, so the **admission consumer** is the authoritative owner fence, and
authorization stays authoritative in the [Storage Gateway](/architecture/storage/).
:::

### The control-plane subject grammar

Every control-plane subject is `og.v1.<verb>.<node>`
([ADR-0081](/architecture/decisions/#adr-0081-the-control-plane-wire-is-one-subject-grammar-node-anchored-and-batch-granular)):
the node name is the **last token, exactly one token**, which is what lets the server subscribe
per-verb single-token wildcards (`og.v1.worklist.*`) and lets a node's credential be an explicit
allow-list of its own subjects plus its private reply namespace (`_INBOX.<node>`). The verb family
today is `worklist` (request-reply), `heartbeat`, and `telemetry` (the JetStream firehose);
`worklist-changed` is reserved for the server's re-pull nudge, and `og.v1.command.<node>` is the
committed future per-node command queue. The trusted push lane `og.v1.api.telemetry` deliberately
sits in its **own segment** rather than as a reserved node name under `og.v1.telemetry.*` (above):
structural impossibility beats a naming convention.

Addressing is **node-anchored and batch-granular**: a record has no subject of its own, and its
name is payload, not topic. Per-record subjects (the MQTT-style
`og.v1.telemetry.<node>.<component>.<metric>` tree) were rejected: the consumer needs the whole
batch anyway, a subject per record explodes the permission grammar without adding a fence (the
admission consumer owns the owner fence), and the one-token name rule was never justified by names
as topic tokens. One consequence is that the core-NATS verbs' server-side consumers (worklist,
heartbeat) are **singletons by construction**; the HA fork for them (queue groups versus worklist
reassignment) is named and deferred ([scaling](/architecture/scaling/)). Telemetry does not face
that fork: its ingest is a named durable JetStream consumer, and a second server attaching the
same durable joins it and splits deliveries.

## Request-reply: service to service

Synchronous internal calls use **NATS request-reply**: an in-process call in single-binary mode, a request over the bus when modes split across pods. The public API never uses request-reply (it is HTTP); request-reply is the east-west wire only.

:::design[The JetStream KV and object store, tracked in #529]

## KV and object store

- **KV** holds config, **distributed locks and leader-election** (the CDC publisher and the clock
  are leader-elected singletons), and the principal and permission cache (replacing Postgres
  `LISTEN/NOTIFY` invalidation, [identity and access](/architecture/identity-access/)).
- **Object store** holds internal artifacts (a compiled per-node runtime unit); user files stay on
  the content-addressed [blob store](/architecture/files/).

:::

:::design[The live UI relay, tracked in #523]

## The live UI relay

The web UI gets real-time data by **subscribing to the server, not to the bus**, never a polling
loop on the API. **Operators do not connect to NATS**, so the live path introduces **no second
authorization model**:

- **Server-side relay.** The SSE subscribe is a normal route, capability-checked before it opens;
  the server holds the internal JetStream subscription and runs every candidate message through the
  **same Storage Gateway scope** a read would use (the one authoritative ABAC filter, in-process),
  streaming only what passes: the scope filter executes in one place, **called** per message rather
  than re-encoded as subject permissions.
- **Transport is SSE**, on the same authenticated, same-origin HTTP seam as the rest of the API
  (same cookie or bearer, proxy, TLS); over HTTP/2 the stream multiplexes, no connection-count
  ceiling. **No NATS-WebSocket path and no fallback**.
- **Seed then stream.** A [view](/architecture/views/) over HTTP paints current state; the SSE
  stream keeps it live with deltas. Bulk reads stay on the views BFF.
- **Where it shines:** a live fleet tile, the alarm console, and the **template-debug / dev-tap**
  surface, an operator watching samples arrive live as a template runs
  ([the learning tool](/contributing/learning-tool/)).

:::

Related: [API](/architecture/api/), [scaling](/architecture/scaling/), [nodes](/architecture/nodes/) (the edge as a NATS client), [workers](/architecture/workers/) (the JetStream consumers), and [storage](/architecture/storage/).
