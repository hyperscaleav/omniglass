---
title: "Collection slice (checkpoints 1-2): the storage tier and the node runtime"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoints 1-2): the storage tier and the node runtime.** The first cut of the
  [collection](/architecture/collection/) engine. Checkpoint 1 landed the storage tier: the `node`,
  `interface_type`, `interface`, `task`, `datapoint_type`, and `metric_datapoint` tables (an idempotent
  dbmate migration), the reachability `datapoint_type` canon and the `icmp`/`tcp` `interface_type`s seeded at
  boot, a scope-safe `metric_datapoint` write path, and the pure reject-not-project registry. Checkpoint 2
  adds the [node](/architecture/nodes/) runtime: `omniglass server` hosts an in-process `nats-server`
  (JetStream enabled) and `omniglass node` enrolls (create, `POST /nodes/{name}:enroll` mints the token,
  `POST /nodes:claim` exchanges it for the NATS credential), then over NATS pulls its worklist
  (`og.v1.worklist.<node>` request-reply, its enabled tasks plus a `config_generation`) and heartbeats
  (`og.v1.heartbeat.<node>`, the server stamps `last_heartbeat_at`). **Per-node subject isolation is real**:
  an in-process auth callback scopes each node's NATS credential to its own subjects, proven by a negative
  integration test (node A cannot publish or pull as node B) against a real embedded server on an ephemeral
  port, plus a fake-seam unit test and the full enroll -> pull -> heartbeat round trip. A node is a
  first-class `principal` of `kind='node'` with a 1:1 `node` detail table (name is the estate address the
  collection FKs reference) and a bearer `credential` row, reusing the human/service identity machinery.
  Deliberate thin cuts ([ADR-0017](/architecture/decisions/)): the credential is an interim shared secret
  stored as a bearer credential (the enrollment token doubles as the NATS password; nkey/JWT and the
  single-use bootstrap deferred), and the control plane is JSON over core NATS. Deferred to later
  checkpoints of this slice: the probes (tcp, then icmp), the protobuf telemetry `Event` over JetStream, the
  ingest consumer, and the API/CLI/UI surfaces (interface and task CRUD, the Nodes inventory and reachability
  panels).
