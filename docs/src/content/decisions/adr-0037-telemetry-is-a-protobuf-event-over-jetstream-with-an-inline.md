---
title: "Telemetry is a protobuf Event over JetStream with an inline owner-confining consumer"
date: 2026-07-07
status: "Accepted"
summary: "Telemetry is a protobuf `Event` over a JetStream durable consumer; the consumer binds the owner from the task's interface and confines a node to its own tasks inline (no separate raw-telemetry table or Postgres queue); raw persistence + replay and label-based multi-owner routing deferred"
---

- **Date:** 2026-07-07 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [datapoints](/architecture/properties/)
- **Decision:** A node ships each collected batch as a protobuf `Event` (proto3, `proto/og/v1/event.proto`, since renamed to `TelemetryBatch` in `proto/og/v1/telemetry.proto`, #424,
  `Event` + `Datapoint` messages only, no gRPC service) published to `og.v1.telemetry.<node>`. This is
  omniglass's first protobuf; the wire is generated with `protoc` + `protoc-gen-go` via a `gen-proto` stage on
  `make gen`, and the generated `event.pb.go` is committed. The server hosts a JetStream stream
  (`OG_TELEMETRY` over `og.v1.telemetry.*`) and a **single durable consumer** (`og-telemetry-worker`,
  AckExplicit) whose handler, per Event, **derives and writes inline**: it decodes the batch, resolves the
  owner as the task's interface component, **confines** the node to its own tasks, applies reject-not-project
  against the `datapoint_type` registry, writes the surviving typed rows through the checkpoint-1
  `InsertMetricDatapoints` path (`owner_kind=component`, `provenance=observed`), and acks. A permanent
  condition (an undecodable payload, or an orphan the confinement fence drops) is terminated/acked so it is not
  redelivered; only a transient failure (a DB error) is left unacked so JetStream redelivers. **The node stamps
  no component identity**: its only assertion is the publishing subject (its own name) plus the `task_id`; the
  server binds and confines.
- **Context:** The prior (v2) design split telemetry into a hot path that persisted a raw event to a
  `telemetry` table and an async Postgres queue worker that derived from it. Checkpoint 3 deliberately
  **collapses that split**: the JetStream durable consumer **is** the at-least-once worklist, so there is no
  raw-telemetry table and no Postgres queue in this checkpoint; the handler derives, confines, writes, and acks
  in one place. This keeps the reachability slice small while keeping the two invariants **real and negatively
  tested**: a node cannot land a datapoint for a component it holds no task for (an Event carrying another
  node's `task_id` is orphan-dropped, no row written), and an unregistered datapoint name is dropped, not
  projected. Owner binding is the **interface-prebind path only** (task -> interface -> component); there is no
  separately-authored `transform_rule` (omniglass has none), so label-based multi-owner routing, discovery
  rules, and node-self binding are a later checkpoint.
- **Closes the gap:** raw-`Event` persistence (backfill/replay) and the raw -> admission -> trusted two-lane
  topology, plus label-based multi-owner resolution, are tracked with a later collection checkpoint.
