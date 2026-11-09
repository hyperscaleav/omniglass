---
title: "The scale lab measures WAL a sample, lag p99 and the ingest lane's busy fraction (#1517)"
date: 2026-11-09
refs: [1517, 1585, 1357]
---

`omniglass lab measure` now prints three more rows that size the single-Postgres ingest ceiling: WAL
bytes a sample (the window's WAL delta over the samples it landed), the ingest lag at p99 beside p50
and p95, and the ingest consumer lane's busy fraction with its service time a batch. The telemetry
stream is a work queue with one durable consumer and one message in flight, so the consumer is one lane
and the handler's share of the wall clock is the lane's utilisation. The bus server tallies every
batch's handler time (`Server.LaneTally`) and logs the cumulative count and busy milliseconds every 30 s
as `msg="ingest lane"`, only when a batch landed; the harness reads the first and last tally inside the
window and divides busy time by the span between them, so the fraction is exact over that span. Fewer
than two tallies, or a tally that went backwards across a restart, reads `not measured` with the reason
under the table, and every other figure still prints. The JSON carries `wal_bytes_per_sample`, `p99`
and `consumer_lane`. This is the measurement that decides whether partitioning the consumer (#1585) can
move the lag at all: a lag at the node's one-second spool flush with the lane mostly idle says the lane
is not the bottleneck.
