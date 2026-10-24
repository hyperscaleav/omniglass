---
title: "The event lineage columns land ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events))"
date: 2026-07-28
refs: []
form: bullet
---

- **The event lineage columns land ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)).**
  `event.caused_by_event_id` is renamed to **`source_event_id`** (the source is the cause), and
  **`source_log_line_id`** (an FK to `log_line`, the raw line a rule derived the event from) and
  **`derived_by_rule_id`** (which rule) are added, all nullable, so a natively-caught event carries none.
  This is the schema the derivation engine writes into: a rule that turns a `log_line` into an event will
  stamp the two new columns. No producer fills them yet (the derivation engine is the next slice); the
  generated ERD carries the new shape and the `event -> log_line` edge. Part of
  [#410](https://github.com/hyperscaleav/omniglass/issues/410).
