---
title: "One tick of a node lands as one batch of task groups (#1582)"
date: 2026-11-08
refs: [1582, 1570, 1556, 311, 161]
---

A node used to publish one batch per task per tick, and the server landed each in its own transaction of
about twenty statements, so the receipt table grew as fast as the metric table (about one receipt per
sample, measured on the worker in #1570 and #1556). Now the node's spool coalesces the groups of a tick
(a `TaskGroup` per task, with its own observed time and lanes) into one `TelemetryBatch`, flushed after
a second, at its byte budget, or at once when a session pushes a change, with the id and the bytes fixed at the flush so a retry ships the same
pair. The consumer resolves every group's task in one query, binds a writer per group, and lands the
batch in one transaction under one receipt whose statement count does not grow with its groups: the
arcs are preloaded in one query, every group is admitted in Go, the property series are locked in one
sorted statement, and each lane is a set insert per writer.

A group the gateway refuses is skipped and its siblings land, with the receipt written once and the
message acked, where a refused row used to fail the whole batch. A value only Postgres refuses reruns
the batch in a fresh transaction with a savepoint per group to learn which groups it refuses, rolls
that pass back, and lands the batch once more without them, so a refused group leaves no calc mark
on its owner. The identity bindings of every conduit in a tick resolve in one read. A series a batch carries twice still
lands each changed value in order, so the property lane's transition guard holds. A node-subject batch
that carries top-level task samples is terminated: the node lane moved wholesale to groups, with no
compatibility path.

ADR-0311 and ADR-0161 are amended in place: one batch per tick, a refused group dropping only itself,
and the writer per group. The receipt horizon stays 24 hours, past the spool's 12 hour age cap.
`omniglass lab measure` gains batches landed, samples landed, samples per batch and batches a second,
from the database's receipt and row counters.

The figures this is held to are targets until the slice's pull request records a measured run: at
1,000 systems the busiest node ships 40 or more samples per batch, all nodes together land 18 or fewer
batches a second, statements per sample fall at least fivefold from about 18, receipts stay under
0.5 GiB a day, ingest lag p99 stays under 5 s, and a ten minute backlog drains.
