---
title: "A server reports its request and query timing by route template and statement fingerprint"
date: 2026-11-09
status: Accepted
summary: "Accepted: each server boot ensures one `omniglass-server` platform component for its identity (`OMNIGLASS_SERVER_NAME`, else the hostname), named in a new `platform_server` table, and lands its request counts and durations, slow-query count and database pool figures there once a report window through a general window recorder in `internal/platformmetric`. A request's instance is its method, route template and status class, never its path; a slow query is logged by its statement's fingerprint, never its arguments. The pool's wait time is the exhausted-acquire wait, and the recorder's flush yields platform rows, not sample writes."
refs: [1627, 1094, 1625, 1628, 1629, 1293, 1342, 1672]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [collection](/architecture/collection/), [settings](/architecture/settings/)
- **Decision:**
  1. **An instance is the route template, never the path.** The request recorder reads the pattern
     chi matched after the router has run (`GET /api/v1/components/{name} 2xx`), so the series a
     server keeps are bounded by its route table and a scan of a million names costs one. A method
     outside HTTP's standard set reads `OTHER`, and every request no route matched records under the
     one instance `unmatched`, so neither invented verbs nor invented paths grow the series. The status
     is its class, not its code.
  2. **A slow query logs its fingerprint, never its arguments.** The pgx query tracer logs a WARN with
     the duration and the statement's text, whitespace collapsed and bounded at 200 runes, and counts
     it into `db-slow-query-count`. Arguments carry operator data and secrets, so no log line names
     them. The threshold is the `database.slow_query_ms` platform setting (500 ms default, 0 off), held
     atomically and re-read each report window. A failed fleet view now logs its cause the same way:
     the error goes to the server log and the response stays a bare 500.
  3. **A `platform_server` table maps an identity to its component.** A server has no row of its own
     elsewhere (a node has `node.platform_component_id`), so the new table is the server's identity:
     the name it boots under and the component it reports on, unique on both. The boot ensures it under
     the platform step's advisory lock, so concurrent boots under one identity make one component.
     Keying on the component's name instead would break when a stem collision suffixes it. The
     override of the hostname is the environment variable `OMNIGLASS_SERVER_NAME`, not the platform
     setting the spec named: a platform setting is one value every server reads, and an identity is
     one per process. The lab sets a fixed `OMNIGLASS_SERVER_NAME`, since a container's
     hostname is new each run; the chart's equivalent is #1672.
  4. **`db-pool-wait-duration` is the exhausted-acquire wait.** The spec named pgxpool's
     `AcquireDuration`, which sums every acquire's time, including the near-instant ones from an idle
     connection. The reading is the window's delta of `EmptyAcquireWaitTime`, the time spent only by
     acquires that found the pool empty, beside `db-pool-wait-count` from `EmptyAcquireCount`, so both
     read 0 on a healthy pool and grow only with contention.
  5. **The recorder flushes platform rows.** `Recorder.Flush(componentID)` returns
     `[]storage.PlatformMetric`, the rows `RecordPlatformMetrics` (ADR-1625 point 7) already writes,
     not the spec's `MetricSampleWrite`, so the server needs no second write path. A count reads its
     sum, then one zero the window after its last activity, then nothing; a mean, max or gauge with no
     observation reads nothing. #1628 adds its runtime sampler to the same reporter.
  6. **File names.** The server component lives in `internal/storage/platform.go` beside the node's,
     and the tracer in `internal/storage/slow_query.go`, not the spec's `platform_component.go`.
- **Context:** #1094 asks the platform to monitor itself. The node's account of itself shipped with
  #1625; the server had only its logs, so a slow route, a slow statement or a starved pool showed up
  first as an operator's complaint. The window recorder and the reporter are the primitive #1628 and
  #1629 consume; the settings reference that renders `database.slow_query_ms` is #1342.
