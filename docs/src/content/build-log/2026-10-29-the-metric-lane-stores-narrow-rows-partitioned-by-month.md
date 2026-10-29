---
title: "The metric lane stores narrow rows, partitioned by month (#1275, #420)"
date: 2026-10-29
refs: [1275, 420, 1265, 1417]
---

A metric sample is now `(id, series_id, ts, value)`. What it is a sample of (the type, the owner,
the instance, the provenance, the source, and the node that collected it) is one row of a new
`metric_series` table, shared by every sample of the series; a command's intended rows moved to
`metric_declared`, which no retention pass touches. The `make footprint` harness, five runs on the
same host before and after, put the row at 60 bytes of heap and 39 of index (after `REINDEX`), down
from 126 and 91, and WAL at about 310 bytes an observation, down from about 459.

`metric` is partitioned by month from birth. The server keeps this month and the next two, checking
at boot before ingest starts and on every retention pass, and a batch whose month has no partition
makes it and lands rather than being dropped as refused, the blocker #420's review found. Retention
drops a month wholly past every window, detaching it concurrently, and keeps the window exact inside
the rest. The metric floor is now the latest row within retention; property keeps its floor and is
not partitioned. The upgrade copies every wide row in one migration and refuses unless the counts
agree. `event`, `log_line` and `node_log` partitioning is #1417.
