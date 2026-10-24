---
title: "The event_type family lands: events get their own registry ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables), second feature slice)"
date: 2026-07-28
refs: []
form: bullet
---

- **The event_type family lands: events get their own registry ([ADR-0063](/architecture/decisions/#adr-0063-the-telemetry-model-is-typed-registries-over-bare-noun-data-tables), second feature slice).**
  Events move out of `property_type` into their own **`event_type`** registry, the occurrence keyspace and
  the twin of `property_type`. A guarded migration creates `event_type`, repoints `event` off
  `property_type` (kind=log) onto `event_type`, adds the **`origin`** column (caught/caused/derived/
  scheduled) plus the `caused_by_event_id` and `correlation_id` causation columns, and drops the `log` kind
  from `property_type` (now `{metric, state}`). Event rows are ephemeral telemetry logs, so the repoint
  clears the occurrence log rather than backfilling a type. The registry ships full CRUD (`/event-types`,
  gated `event_type:*`, seeded official `call.started`, official rows read-only), a
  generated CLI and typed client, and a console **Event Types** catalog page mirroring Properties.
  The **native caught path** is built: a component that publishes an event (an xAPI event, a trap) has its
  registered `event_type` name route it to the `event` sink with `origin=caught`, under the same
  owner-confinement and reject-not-project as a metric or state, and the events panel shows the origin. Raw
  log lines are a **separate ingest lane**, not events ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)):
  the `log_line` table and the derivation rules that turn a log line into an event are a later slice. Only
  the caught path has a producer today; `caused` is the command pillar (#396) and `derived`/`scheduled` need
  the rule engine and the clock (the action layer).
  [events](/architecture/events/) advances `Design` to **`Partial`**; [datapoints](/architecture/properties/)
  notes the `log` kind's graduation. Part of
  [#395](https://github.com/hyperscaleav/omniglass/issues/395).
