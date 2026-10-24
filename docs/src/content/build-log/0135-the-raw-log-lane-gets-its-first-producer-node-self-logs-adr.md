---
title: "The raw-log lane gets its first producer: node self-logs ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events))"
date: 2026-07-29
refs: []
form: bullet
---

- **The raw-log lane gets its first producer: node self-logs ([ADR-0066](/architecture/decisions/#adr-0066-logs-are-a-raw-ingest-lane-not-events)).**
  The telemetry `Event` gains a `LogLine` message and a `repeated logs` field: raw log lines are untyped
  arrival, not registry-resolved samples, so they need their own wire slot. The node now runs an slog handler
  that writes to its console and buffers every record, **installed as the process default so it is a node-wide
  emitter**: any node code ships a self-log with a plain `slog.Info(...)` (carrying a `facility`, and optionally
  a `source`, which the mapping lifts into the `log_line` columns), with no logger threaded down to it, and the
  buffer is bounded (oldest dropped, count reported) so a chatty stretch between ticks cannot grow the edge
  process without bound. The run loop drains it each tick and publishes the
  records as a logs-only Event, which the ingest consumer routes to `log_line` **owner-bound to the node**,
  ahead of the sample owner-confinement and independent of any task (a logs-only Event lands without a
  `task_id`). The node narrates its operational story back to the platform (connected to the bus, worklist
  pulled, a task skipped) instead of swallowing it, and a skipped task becomes a node self-log, not a
  false-down component state. The read is `GET /nodes/{name}/logs` (gated `node:read`, scope-injected), with
  a generated CLI (`node log list`) and typed client; the console reuses the `LogsPanel` primitive as a
  **Self-logs** panel on the node blade. That panel's disclosure is now named for the columns it reveals
  (`attributes`, `labels`, or `attributes + labels`, each block captioned once open) rather than the
  overloaded "fields", which collided with a component's property fields and said nothing about which of
  the two a line carries. The dev seed installs the edge node's example self-logs, and one seeded device
  log line is **classified with a label**, the ADR-0066 way an untyped line gets sorted into a class
  without a registry, which nothing in dev exercised before. The
  end-to-end producer loop is closed by a real node publishing a `worklist pulled` self-log that lands on
  its `log_line` lane. Still directional: a device-log source (a syslog listener or a poll) and the
  derivation engine that turns log lines into events. Part of
  [#410](https://github.com/hyperscaleav/omniglass/issues/410).
