---
title: Messaging
description: "The internal and edge NATS subject contract, the sibling to the public API: JetStream streams and consumers, the two lanes, request-reply, KV, live UI subscriptions, and per-tenant subject isolation."
sidebar:
  badge:
    text: Design
    variant: caution
---

Omniglass has **two typed contracts**. The [public API](/architecture/api/) is the north face (HTTP and
OpenAPI: operators, the SPA, the CLI, integrations, MCP). This is its sibling: the **internal and edge
transport**, a **NATS subject contract** over JetStream. Service-to-service traffic, the edge, and the
live UI ride it. **Postgres stays the system of record; NATS moves.** The deployment topology and the
inter-service diagram are on [scaling](/architecture/scaling/).

## Two lanes, one bus

Internal traffic splits by what is moving:

- **Data lane (NATS-native): datapoints.** Observed datapoints arrive on the bus (the edge and the
  central node publish them); calc consumers publish calculated ones back. The rule engine consumes them
  directly, and a **persistence consumer** batch-writes them to the Postgres datapoint tables as an async
  sink. Datapoints do not go through CDC, they are already on the bus, idempotent on `(series, ts)`.
- **Record / state lane (Postgres-first, CDC-out): events, alarms, actions, operator mutations.** Born in
  a Postgres transaction (a firing `event_rule` writes the event plus the alarm transition atomically; the
  API writes config, ack, settings). A **leader-elected CDC publisher** (logical decoding of the WAL)
  publishes those committed changes to JetStream, where `action_rule`, reconcile, and projection consumers
  react. No dual-write: born in the commit, the bridge fans it out.

## Streams and consumers

- **datapoints** (data lane): published by the edge and calc consumers; consumed by the rule engine and
  the persistence consumer. A **work-queue consumer group** scales horizontally (each message to exactly
  one consumer), so adding worker replicas adds throughput with no leader.
- **records** (events, alarms, actions): published by the CDC publisher from Postgres commits; consumed by
  `action_rule`, reconcile, and projection consumers.
- **commands**: a durable, per-node **command queue** the edge holds a consumer on ([nodes](/architecture/nodes/)).
- **telemetry**: the edge publishes `node.self`, `session_log`, and command results.

Durable consumers track their own position; delivery is at-least-once with `Nats-Msg-Id` dedup and double
ack, which with the idempotent sinks (`(series, ts)`, action id, the CDC idempotency key) gives
exactly-once **outcomes**. The edge stamps `ts`, so the system is ts-authoritative and needs no strict
ordering on the wire.

## Subjects, accounts, and scope

Subjects are hierarchical and **scope is expressed in them**, not bolted on:

- **Tenant = one NATS account.** Per-account isolation (messaging) is the same boundary as the
  per-database isolation (storage): no shared subjects, no shared rows ([identity and access](/architecture/identity-access/)).
- **Placement and ABAC scope = subject permissions.** A node may only publish and subscribe the subjects
  for the owners on its placement (its `visible_set`); a principal's live subscription is limited to the
  subjects its visible set permits. The bus enforces the same scope the [Storage Gateway](/architecture/storage/)
  enforces on reads, so there is no second authorization model to keep in sync.

## Request-reply: service to service

Synchronous internal calls use **NATS request-reply**: an in-process call in single-binary mode, a
request over the bus when modes are split across pods. The public API never uses request-reply (it is
HTTP); request-reply is the east-west wire only.

## KV and object store

- **KV** holds config, **distributed locks and leader-election** (the CDC publisher and the clock are
  leader-elected singletons), and the principal and permission cache (replacing Postgres `LISTEN/NOTIFY`
  invalidation, [identity and access](/architecture/identity-access/)).
- **Object store** holds internal artifacts (a compiled per-node runtime unit, for example). User files
  stay on the content-addressed [blob store](/architecture/files/), not here.

## Live subscriptions: the reactive UI

The web UI gets real-time data by **subscribing, not polling**, and **not** through a polling loop on the
API:

- The SPA opens a **NATS WebSocket** connection (the `nats.ws` TypeScript client) and **subscribes to the
  subjects its scope permits**; the bus **pushes** updates as data flows. The API is not in the live path.
- **Auth** is a **short-lived, scoped NATS credential (JWT)** the API mints for the logged-in principal at
  session establish; its subject permissions are exactly that principal's ABAC visible set. So a live
  subscription is scope-enforced by the bus, the same boundary as every read, and it expires on its own.
- **Seed then stream.** A [view](/architecture/views/) over HTTP seeds the current state; the subscription
  keeps it live. Bulk reads stay on the views BFF; live deltas come over the subscription.
- **Where it shines:** a live fleet tile, the alarm console, and the **template-debug / dev-tap** surface,
  where an operator watches datapoints arrive in real time as a template runs (the learning-tool "render
  the real engine against live data" surface, [the learning tool](/contributing/learning-tool/)).
- **SSE fallback.** Where a strict proxy or a simpler deployment rules out a browser-to-NATS WebSocket,
  the server can SSE-relay (the API subscribes to NATS and streams to the browser). The NATS WebSocket is
  the model; SSE is the fallback for the same scoped seam.

Related: [API](/architecture/api/) (the public HTTP contract), [scaling](/architecture/scaling/) (the
deployment topology and the diagram), [nodes](/architecture/nodes/) (the edge as a NATS client),
[workers](/architecture/workers/) (the JetStream consumers), and [storage](/architecture/storage/)
(Postgres as the system of record).
