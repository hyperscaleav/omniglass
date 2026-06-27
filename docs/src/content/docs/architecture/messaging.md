---
title: Messaging
description: "The internal and edge NATS subject contract, the sibling to the public API: JetStream streams and consumers, the two lanes, request-reply, KV, the live UI relay, and per-tenant subject isolation."
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
- **Subject permissions gate nodes and internal clients only.** A node may publish and subscribe only the
  subjects for the owners on its placement (its `visible_set`); the grant is **mechanically derived from
  placement**, a coarse transport gate, not a second copy of the ABAC model. Authorization stays
  authoritative in the [Storage Gateway](/architecture/storage/), which confines every node ingest to that
  same `visible_set`. **Operators never connect to the bus**, so there is no operator subject-permission
  model to keep in sync (see the live UI relay below).

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

## The live UI relay

The web UI gets real-time data by **subscribing to the server, not to the bus**, and never through a
polling loop on the API. **Operators do not connect to NATS** (the bus is internal-plus-nodes only), so the
live path introduces **no second authorization model**:

- **Server-side relay.** The server holds the internal JetStream subscription, runs every candidate
  message through the **same Storage Gateway scope** a read would use (the one authoritative permission and
  ABAC check, in-process), and streams only what passes down to the browser. Authorization executes in
  exactly one place; the live path **calls** it per message instead of re-encoding it as subject
  permissions.
- **Transport is SSE.** The browser opens a **Server-Sent Events** stream on the same authenticated,
  same-origin HTTP seam as the rest of the API (same cookie or bearer, same proxy, same TLS), and the
  server pushes. One-way fits a live read: subscribe is one request, data flows down, and mutations and
  commands keep their own paths (the API action row, the internal bus). Over HTTP/2 the stream
  multiplexes, so there is no connection-count ceiling. There is **no NATS-WebSocket path and no
  fallback**: SSE is the one live transport.
- **Seed then stream.** A [view](/architecture/views/) over HTTP paints current state; the SSE stream
  keeps it live with deltas. Bulk reads stay on the views BFF; live deltas come over the relay.
- **Where it shines:** a live fleet tile, the alarm console, and the **template-debug / dev-tap** surface,
  where an operator watches datapoints arrive in real time as a template runs (the learning-tool "render
  the real engine against live data" surface, [the learning tool](/contributing/learning-tool/)).

Related: [API](/architecture/api/) (the public HTTP contract), [scaling](/architecture/scaling/) (the
deployment topology and the diagram), [nodes](/architecture/nodes/) (the edge as a NATS client),
[workers](/architecture/workers/) (the JetStream consumers), and [storage](/architecture/storage/)
(Postgres as the system of record).
