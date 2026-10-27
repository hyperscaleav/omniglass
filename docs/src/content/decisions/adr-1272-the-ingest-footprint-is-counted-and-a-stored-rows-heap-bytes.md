---
title: "The ingest footprint is counted, and a stored row's heap bytes are a count"
date: 2026-10-27
status: "Accepted"
summary: "A fourth performance instrument, the ingest footprint: a deterministic standard workload delivered through the real telemetry handler over the dev scenario's fleet, holding statements per batch, rows per observation on each lane and heap bytes per stored metric row exactly, and index bytes per row under a bound; WAL bytes and wall-clock time are reported, never held. Extends ADR-0094: a byte count that depends only on the schema and the Postgres major is a count, not a timing, so it may gate"
refs: [1272, 1265, 651]
---

- **Date:** 2026-10-27 | **Status:** Accepted | **Pages:** [test-driven](/contributing/test-driven/)
- **Decision:** (#1272, extending [ADR-0094](#adr-0094-benchmarks-are-the-second-performance-instrument-and-they-gate-nothing).)
  1. **What one node batch costs the database is counted.** `TestIngestFootprint`
     (`internal/bus/footprint_test.go`) seeds the dev scenario's fleet and delivers a standard
     workload through `handleTelemetry`, the real ingest handler, with only the JetStream
     delivery doubled. It runs in `make test`.
  2. **Three quantities are held exactly:** statements per batch (from the query tracer, the
     seam `querycount` already uses), rows written per observation on each lane, and heap bytes
     per stored metric row after `VACUUM`. **One is bounded:** index bytes per stored metric row,
     because the indexes lead with component uuids the dev seed mints at random and page splits
     vary between runs. **Two are reported only:** WAL bytes per observation, which moves with
     checkpoint timing, and wall-clock time, measured by `BenchmarkIngestFootprint` in
     `make bench` as ADR-0094 prescribes.
  3. **The workload is declared by shape.** `internal/footprint` builds one node batch per task
     per check, deterministically from the task and the tick; its `StandardMix` is calibrated
     to a measured lab run and is re-derived when the simulated fleet changes, rather than
     waiting for every simulator profile to exist.
- **Context:** The storage work of #1265 (batched writes, narrower rows, retention, trends)
  needs a before and an after that a reviewer can trust. ADR-0094 refused a timing gate and a
  stored baseline because a duration depends on the machine and the day. A statement count
  does not, and neither does the heap a fixed-width row occupies: with rows that land in order,
  the page count is the same on every machine for one Postgres major (pinned `postgres:18` in
  the test harness). That is why bytes join the counted family, while WAL and time stay
  diagnostic. The first measurement (2026-10-27, `main` at `50d4ae8d`): 31 statements for a
  batch of five observations (the owner resolution, three full catalog reads, the receipt, the
  rule and series locks, the same component read five times, one `INSERT` per metric, the
  seen stamp); 126 heap bytes and about 100 to 130 index bytes per metric row; about 460 WAL
  bytes per observation; about 3.3 ms and 135 KB allocated per batch, flat from 13 to 130 tasks.
- **Not chosen:**
  - **Driving the full lab per test.** Docker orchestration, cgroup sampling and real node
    cadence make a slower, noisier instrument; it stays a diagnostic follow-up.
  - **Pinning index bytes exactly.** The fixture's random uuids would make the pin flap; seeding
    fixed uuids would measure a fleet no install has.
  - **Gating WAL.** It is the cost a replica or a WAL archive pays and is reported for that
    reason, but a checkpoint landing inside the window changes it by a large fraction.
