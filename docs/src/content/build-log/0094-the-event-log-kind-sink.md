---
title: "The `event` log-kind sink"
date: 2026-07-21
refs: []
form: bullet
---

- **The `event` log-kind sink.** The collection pipeline gains its **third sink**. A new **`event`** table is
  the **log-kind sink** (a past occurrence) beside `metric_datapoint` / `state_datapoint` (a sampled present
  value), carrying the **same datapoint owner exclusive-arc** (`owner_kind` plus `component_id` / `system_id` /
  `location_id` / `node_id`, one-set CHECK) and the **same provenance** (`observed` / `calculated` / `intended` /
  `declared`, default `observed`), plus a **`message`** (text) and structured **`attributes`** (jsonb). A
  **log**-kind datapoint that the ingest consumer used to **drop** (it had no sink) now routes to `event`:
  `deriveDatapoints` returns metrics, states, **and** events, and the consumer calls **`InsertEvents`**, so a log
  rides `string_value` (its message) or `json_value` (its attributes) under the **same** owner-confinement and
  reject-not-project gates as the other two sinks. A boot-seed property **`log.line`** (kind `log`) is the
  canonical starter. The reserved **`event_id`** columns on `metric_datapoint` and `state_datapoint` are closed
  into real foreign keys to `event(id)` (`on delete set null`), so an **intended**-provenance datapoint
  references the `event` that produced it. Storage adds `InsertEvents` (batch, in-tx) and `ListComponentEvents`
  (newest first); the read route **`GET /components/{name}/events`** (`list-component-events`, gated
  `component:read`, non-disclosing 404 out of scope) returns the last 24 hours capped at 200, regenerated into
  the OpenAPI document, the cobra CLI, and the typed SPA client, and the component detail page gains an **Events**
  panel over it. Proven by the consumer unit tests (a log routes to the event slice, a metric/state still land in
  their sinks), the `InsertEvents` / `ListComponentEvents` storage integration test, and the
  `/components/{name}/events` HTTP e2e (the newest-first window and the out-of-scope 404). With metric, state,
  **and** log all flowing, this is the **P1 follow-up** of the estate-model roadmap
  ([ADR-0046](/architecture/decisions/#adr-0046-the-event-log-kind-sink)); the
  [datapoints](/architecture/properties/) and [data collection](/architecture/collection/) pages stay `Partial`
  (the `log_datapoint` table, the `event_type` registry, and log-to-event promotion are still `Design`).
