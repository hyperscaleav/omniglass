---
title: "The ingest registry is kept between batches (#1279)"
date: 2026-10-28
refs: [1279, 1265, 1272]
---

The footprint harness (#1272) showed the telemetry consumer reading all three catalogs in full on
every node batch: the metric, property and event types, each with its calc-rule writer. The
catalogs change only when an operator edits a type, so nearly every one of those reads returned
what the previous batch had read.

The consumer now keeps the registry between batches. Before each batch it reads one fingerprint
(`storage.CatalogFingerprint`), a digest of every row version of the four tables the registry is
built from. It rebuilds the registry only when that moves, so a type created, renamed or retired is
honored by the very next batch
([ADR-1279](/architecture/decisions/#adr-1279-the-ingest-registry-is-kept-until-the-catalogs-fingerprint-moves)).
A drift guard fails if the catalog list queries ever read a table the fingerprint does not cover.

Measured on the standard workload:

| Per node batch | Before | After |
|---|---|---|
| Statements | 31 | 29 |
| Allocations | about 1,267 | about 812 |
| Bytes allocated | about 135 KB | about 74 KB |

Wall-clock time moved within the host's noise. `TestRegistryFollowsTheCatalogBetweenBatches` proves the
cache follows the catalog. A metric type created between two batches lands on the second. A calc
rule that starts writing that metric makes the next node sample of it drop. A cache that never
rebuilds fails it.
