---
title: "The ingest footprint is counted, so storage work has a before and an after (#1272)"
date: 2026-10-27
refs: [1272, 1265]
---

The storage efficiency epic (#1265) is about to change how telemetry is written and kept, and every
change in it needs a measurement a reviewer can trust. `TestIngestFootprint` is that measurement.
It seeds the dev scenario's fleet and delivers a standard workload through the real telemetry
handler, one node batch per simulated endpoint per check, exactly as JetStream would deliver it.
Only the transport is a double.

It holds what one batch costs the database, all deterministic. Statements per batch, rows per
observation on each lane, metric rows per full heap page and the metric index definitions are
exact. Index bytes per row are bounded after a `REINDEX`, because a live index's page splits follow
the fleet's random uuids and vary by a fifth from run to run. WAL bytes, the live index size and
wall-clock time are reported but never held
([ADR-1272](/architecture/decisions/#adr-1272-the-ingest-footprint-is-counted-and-a-stored-rows-heap-bytes-are-a-count)).
The workload is built deterministically from each task and tick (`internal/footprint`). Its mix is
calibrated to a measured lab run, so it is re-derived when the simulated fleet grows rather than
waiting for it.

The first reading, on `main` at `50d4ae8d`, for a batch of five observations:

| Measure | Value |
|---|---|
| Statements per batch | 31: among them four catalog reads, the same component read six times, and one `INSERT` per metric (the ADR lists all 31) |
| Metric rows per heap page | 65, about 126 bytes a row |
| Index per metric row | about 91 bytes rebuilt, about 100 to 135 live |
| WAL per observation | about 460 bytes |
| Time and memory per batch | about 3.3 ms and 135 KB allocated, flat from 13 tasks to 130 |

Breaking the handler on purpose with one extra catalog read fails the test at 32 statements a
batch, listing the statements issued. `make footprint` prints the figures, and `make bench` now
times the same path.
