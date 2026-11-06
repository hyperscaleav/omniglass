---
title: "The metric lane is partitioned by day (#1527)"
date: 2026-11-06
refs: [1527, 1275, 1276]
---

`metric` is now partitioned by UTC day instead of by month, so retention drops raw samples whole a
day after they pass the window rather than deleting up to a month of them chunk by chunk. One
migration copies every month-partitioned row into the day partitions, keeping its id, and refuses
unless the counts agree; its down swaps back to months.

The server keeps today and the next three days partitioned, and the boot now reads the partitions
back and refuses to start ingest unless every instant through the third day ahead is covered,
naming the first one that is not. A batch that misses a day still makes it and lands. The drop
takes a day once it is past the lane's window and the trend's compacted mark, and keeps a day that
holds a sample of a type kept longer than the lane, where the months were all held back to the
longest type window installed.

`make footprint`, same host and build, before (month) and after (day):

| Figure | Month | Day |
| --- | --- | --- |
| Heap rows a page | 136 (60.2 bytes a row) | 136 (60.2 bytes a row) |
| Index bytes a row, after `REINDEX` | 40.5 | 38.1 |
| Index bytes a row, live | 61.9 | 61.9 |
| WAL bytes an observation | 309.5 | 310.2 |
| Statements a batch | 18.00 | 18.00 |
| Receipt bytes a batch | 161.9 | 161.9 |
