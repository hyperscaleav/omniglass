---
title: "The `event` log-kind sink"
date: 2026-07-20
status: "Accepted; superseded in part by [ADR-0066](#adr-0066-logs-are-a-raw-ingest-lane-not-events)"
summary: "A **log**-kind observation is no longer dropped at ingest: it lands in a new **`event`** table, the log-kind sink (a past occurrence) beside `metric_datapoint` / `state_datapoint` (a sampled present value). `event` carries the same datapoint owner exclusive-arc and provenance, plus `message` + `attributes`, and the reserved `event_id` FK stubs on the two datapoint tables are closed (`on delete set null`). Scope excludes the `datapoint`->`sample` rename (a later cleanup) and `property_value` / the current-value store (the fold-fields slice). P1 follow-up of the estate-model roadmap"
---

- **Date:** 2026-07-20 | **Status:** Accepted; superseded in part by [ADR-0066](#adr-0066-logs-are-a-raw-ingest-lane-not-events) (the log-to-event ingest promotion and the seeded `log.line` type were removed) | **Pages:** [core entities](/architecture/core-entities/), [datapoints](/architecture/properties/), [data collection](/architecture/collection/), [API](/architecture/api/), [Nodes and reachability guide](/guides/operator/collection/)
- **Decision:** A collected **log**-kind observation now has a durable home. A new **`event`** table is the
  **log-kind sink** of the collection pipeline, the counterpart of `metric_datapoint` / `state_datapoint`: where
  a datapoint records a **sampled present value**, an `event` records a **past occurrence** (a device log line, a
  structured frame). It carries the **same datapoint owner exclusive-arc** (`owner_kind` plus
  `component_id` / `system_id` / `location_id` / `node_id`, one-set CHECK) and the **same provenance** vocabulary
  (`observed` / `calculated` / `intended` / `declared`, default `observed`) as the datapoint sinks, plus a
  **`message`** (text) and structured **`attributes`** (jsonb). The ingest consumer's `deriveDatapoints` now
  returns metrics, states, **and** events, and the persistence path calls **`InsertEvents`**: a **log**-kind
  datapoint that used to be **dropped** at ingest (it had no sink) is routed to `event`, riding `string_value`
  (its message) or `json_value` (its attributes), under the **same** owner-confinement and reject-not-project
  gates as the metric and state sinks. A boot-seed property **`log.line`** (kind `log`) is the canonical
  log-kind starter. The reserved **`event_id`** columns on `metric_datapoint` and `state_datapoint` are closed
  into **real foreign keys** to `event(id)` (`on delete set null`), so an **intended**-provenance datapoint
  references the `event` that produced it. Storage adds `InsertEvents` (batch, in-tx, provenance `observed`) and
  `ListComponentEvents(name, since, limit)` (newest first); the read route **`GET /components/{name}/events`**
  (operationId `list-component-events`, gated `component:read`, non-disclosing 404 out of scope) returns the last
  24 hours, capped at 200, and is the log-kind mirror of the reachability read. The console component detail page
  gains an **Events** panel over it.
- **Context:** The estate model is shifting toward property / event / command on the signal side; property
  landed in [ADR-0043](#adr-0043-the-property-catalog). Log was already a first-class datapoint **kind** in the
  registry, but the ingest consumer had **no sink** for it: a log-kind datapoint was silently dropped after the
  metric/state route split ([ADR-0038](#adr-0038-the-reachability-verdict-is-a-built-in-state)), a checkpoint gap
  rather than a design choice. This is the **P1 follow-up** of the estate-model roadmap: give the log kind a
  durable home so the third sink flows like the other two, and close the `event_id` stubs the datapoint tables
  reserved for exactly this. Reusing the datapoint owner-arc and provenance (not a bespoke shape) keeps a log
  occurrence owned, addressed, and traced identically to the values beside it, the primitive-first move.
- **Deferred:** the **`datapoint` -> `sample`** rename (a naming cleanup that lands in a later slice, so the
  datapoint tables keep their current names here); **`property_value`** and the materialized current-value store
  (the [fold-fields slice](/architecture/properties/#reads-current-value-is-a-view)); the normalized **event_type**
  registry and the **promotion** of a raw `event` occurrence into a registered event ([events](/architecture/events/));
  a scope-wide `event` read (this ships the per-component read only); and any `calculated` / `intended` / `declared`
  event producer (the write path is `observed` collection only).
- **Supersedes:** the checkpoint behavior where a **log**-kind datapoint had no sink and was **dropped** at ingest
  (recorded in [ADR-0038](#adr-0038-the-reachability-verdict-is-a-built-in-state)); the log kind now persists to
  `event`. **Divergence from [datapoints](/architecture/properties/):** that page's present-tense design routes the
  log kind to a **`log_datapoint`** table and treats `event` as a strictly normalized, `event_type`-registered
  occurrence promoted from a raw line. The built log sink is the **`event`** table directly (the raw occurrence
  lands there, not in a separate `log_datapoint` table), and the `log_datapoint` table plus the promotion ladder
  stay `Design`; the pages carry an inline note pointing here until the two models are reconciled in the
  fold-fields / rename cleanup.
