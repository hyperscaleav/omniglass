---
title: "The ingest footprint is counted, and a stored row's heap bytes are a count"
date: 2026-10-27
status: "Accepted"
summary: "A fourth performance instrument, the ingest footprint: a deterministic standard workload delivered through the real telemetry handler over the dev scenario's fleet, holding statements per batch, rows per observation on each lane, metric rows per heap page and the metric index definitions exactly, and index bytes per row (after REINDEX) under a bound; the live index size, receipt bytes, WAL bytes and wall-clock time are reported, never held. Extends ADR-0094: a byte count that depends only on the schema and the Postgres major is a count, not a timing, so it may gate"
refs: [1272, 1265, 651]
---

- **Date:** 2026-10-27 | **Status:** Accepted | **Pages:** [test-driven](/contributing/test-driven/)
- **Decision:** (#1272, extending [ADR-0094](#adr-0094-benchmarks-are-the-second-performance-instrument-and-they-gate-nothing).)
  1. **What one node batch costs the database is counted.** `TestIngestFootprint`
     (`internal/bus/footprint_test.go`) seeds the dev scenario's fleet and delivers a standard
     workload through `handleTelemetry`, the real ingest handler, with only the JetStream
     delivery doubled. It runs in `make test`.
  2. **Four quantities are held exactly:** statements per batch (from the query tracer, the
     seam `querycount` already uses), rows written per observation on each lane, metric rows per
     full heap page after `VACUUM` (density, so a slightly larger fleet moves nothing unless a
     row does), and the metric table's index definitions. **One is bounded:** index bytes per
     stored metric row, measured after `REINDEX`, which is the same on every run. **The rest are
     reported only:** the live index size, whose page splits follow the component uuids the dev
     seed mints at random (it varies by about a fifth between runs); receipt bytes per batch;
     WAL bytes per observation, which moves with checkpoint timing; and wall-clock time,
     measured by `BenchmarkIngestFootprint` in `make bench` as ADR-0094 prescribes.
  3. **The workload is declared by shape.** `internal/footprint` builds one node batch per task
     per check, deterministically from the task and the tick; its `StandardMix` is calibrated
     to a measured lab run and is re-derived when the simulated fleet changes, rather than
     waiting for every simulator profile to exist.
- **Context:** The storage work of #1265 (batched writes, narrower rows, retention, trends)
  needs a before and an after that a reviewer can trust. ADR-0094 refused a timing gate and a
  stored baseline because a duration depends on the machine and the day. A statement count
  does not, and neither does the heap the window's rows occupy: they land in order, so how many
  fill a page is the same on every machine for one Postgres major (pinned `postgres:18` in the
  test harness), and so is the size of an index rebuilt from them. That is why bytes join the
  counted family, while WAL, the live index and time stay diagnostic. The first measurement
  (2026-10-27, `main` at `50d4ae8d`), for a batch of five observations:

  | Statements | Count |
  |---|---|
  | Task owner resolution | 1 |
  | Full catalog reads in the consumer (metric, property and event types) | 3 |
  | `begin` and `commit` | 2 |
  | Receipt insert | 1 |
  | Event-rule lock (`for share`) | 1 |
  | The same scoped component read | 6 |
  | Key-writer admission | 3 |
  | Series transition lock | 1 |
  | `select now()` | 3 |
  | Commandable property names (a `command_type` join) | 1 |
  | Event-rule reads | 2 |
  | Guarded property insert, writing nothing while the value holds | 1 |
  | Calc-rule lookup | 1 |
  | Metric inserts, one each | 4 |
  | Endpoint seen stamp | 1 |
  | **Total** | **31** |

  The bytes: 65 metric rows a heap page (about 126 bytes a row), about 91 index bytes a row
  rebuilt and about 100 to 135 live, about 205 receipt bytes per batch, and about 460 WAL bytes
  per observation. The time: about 3.3 ms and 135 KB allocated per batch, flat from 13 to 130
  tasks.
- **Not chosen:**
  - **Driving the full lab per test.** Docker orchestration, cgroup sampling and real node
    cadence make a slower, noisier instrument; it stays a diagnostic follow-up.
  - **Bounding the live index size.** The bound would have to sit above a run-to-run spread of
    about a fifth, wider than the change it exists to catch: an added index passed on some runs
    and failed on others in the review. Rebuilding first removes the spread; pinning the
    definitions catches an added index by name.
  - **Pinning a heap page count.** It holds only while the window's row count happens to fill
    whole pages, so a dev fleet that gains a few endpoints would read as a row-width
    regression. Rows per full page do not move with the count.
  - **Gating WAL.** It is the cost a replica or a WAL archive pays and is reported for that
    reason, but a checkpoint landing inside the window changes it by a large fraction.
