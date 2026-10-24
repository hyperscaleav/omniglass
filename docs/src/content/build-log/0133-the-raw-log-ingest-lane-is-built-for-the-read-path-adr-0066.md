---
title: "The raw-log ingest lane is built for the read path ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events))"
date: 2026-07-28
refs: []
form: bullet
---

- **The raw-log ingest lane is built for the read path ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)).**
  `log_line` is the raw-arrival table: untyped device text owned through the exclusive arc, with
  `severity` and `facility` promoted to indexed columns for retention and routing, and freeform
  `attributes`/`labels`. A log line is not an event, so the write has no registry gate. The read is
  `GET /components/{name}/logs` (gated `component:read`, scope-injected), with a generated CLI and typed
  client, and a console **Logs** panel beside the events panel (severity badges, the structured
  `attributes` and `labels` columns on demand). The dev seed installs the lobby display's device logs (link, CEC, EDID, input, thermal) as log
  lines, their model-correct home. The generated ERD picks up `log_line` in the telemetry cluster. Still
  directional: the ingest producer, the derivation engine that turns log lines into events, and the
  lineage columns. Part of [#410](https://github.com/hyperscaleav/omniglass/issues/410).
