---
title: "A batch lands its metric lane in one statement (#1274)"
date: 2026-10-28
refs: [1274, 1265, 1272, 1279]
---

After #1279, a node batch of five observations still cost 29 statements. Six were the same
component lookup, made once per sample and again for the lock pass. Three were `select now()`, one
per lane. Four were an `INSERT` for each metric.

An ingest batch now resolves each owner and reads its clock once, for every lane, through a wrapper
around the batch's transaction. The metric lane lands every sample of a series the batch carries
once in a single ordered statement. A series the batch carries twice keeps the old insert-then-judge
order, so a rule judging a sample never sees a later sample of the same series from the same batch
([ADR-1274](/architecture/decisions/#adr-1274-a-batch-lands-its-metric-lane-in-one-statement-except-a-series-it-carries-twice)).
A new test lands 31 then 20 in one batch and requires the rule to fire and then clear. Batching
every row instead derives nothing and fails it.

Measured on the standard workload, against #1279's branch, back to back on one host:

| Per node batch | Before | After |
|---|---|---|
| Statements | 29 | 19 |
| Time | 3.4 to 4.0 ms | 2.3 to 2.6 ms |
| Allocations | 811 | 596 |
| Bytes allocated | 67 to 80 KB | 47 to 57 KB |

Rows, heap density and index size are unchanged. Against `main` before either slice, a node batch
went from 31 statements and about 3.8 ms to 19 and about 2.35 ms.
