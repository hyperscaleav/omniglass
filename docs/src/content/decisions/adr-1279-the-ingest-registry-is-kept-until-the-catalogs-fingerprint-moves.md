---
title: "The ingest registry is kept until the catalogs' fingerprint moves"
date: 2026-10-28
status: "Accepted"
summary: "The telemetry consumer keeps its sample registry (the metric, property and event catalogs, with each type's calc-rule writer) between batches and rebuilds it only when a fingerprint of the tables it is built from changes: one statement digesting (SHA-256) every row version (xmin) of metric_type, property_type, event_type and calc_rule. A batch costs one short read instead of three catalog reads; a catalog change is honored by the next batch; a drift guard fails when a list query starts reading a table the fingerprint does not cover"
refs: [1279, 1265, 1272]
---

- **Date:** 2026-10-28 | **Status:** Accepted | **Pages:** [samples](/architecture/properties/)
- **Decision:** (#1279.)
  1. **The consumer keeps the registry between batches.** `registryCache` in `internal/bus` holds the registry `collection.NewRegistry` builds and the fingerprint it was built under, and every batch first reads the fingerprint (`storage.CatalogFingerprint`). A match reuses the registry; a change rebuilds it from the three catalog lists.
  2. **The fingerprint is the tables' row versions.** One statement folds every row's `xmin`, in id order, of `metric_type`, `property_type`, `event_type` and `calc_rule` into a SHA-256 digest (`sha256()`, not `md5()`, which a FIPS-mode Postgres refuses). An insert or an update writes a row version with its own transaction id, and a delete removes one, so any change moves it. A `VACUUM FREEZE` keeps `xmin` (verified on Postgres 18), so nothing else does.
  3. **The fingerprint is read before the catalogs,** so a registry stored under a fingerprint is never older than it: a change landing between the two reads only makes the next batch rebuild.
  4. **A drift guard holds the covered tables to the list queries.** `TestCatalogFingerprintCoversThePlannedRelations` plans each of the three list statements with `EXPLAIN (FORMAT JSON)` and fails when any relation the plan reads, subplans included, is missing from `registryTables`; `TestCatalogFingerprintCoversTheRegistryReads` is its fast, text-scanning sibling for `make test-short`. `calc_rule` is there because a type's writer class follows the calc rule that writes it (ADR-0161).
  5. **The collision warning is unchanged:** still evaluated on every batch, from the cached registry. Moving it to rebuilds was left open on #1279 and is #1298. The fourth catalog-shaped read a batch makes (the commandable property names, inside `LandBatch`) is #1299.
     **Amended (#1298):** the warning moved into the cache. It is logged when a registry is built with a collision and again once an hour while the collision persists, never per batch (about 65 lines a second on the dev fleet at the lab's cadence before); a rebuild without one logs nothing.
- **Context:** The footprint harness (ADR-1272) showed the consumer reading all three catalogs in full on every batch: 3 of the 31 statements a node batch cost, and most of its allocations. The catalogs change only when an operator edits a type. Measured after (`TestIngestFootprint`, `BenchmarkIngestFootprint`, 2026-10-28): 29 statements per batch, allocations per batch from about 1,267 to about 812, bytes from about 135 KB to about 74 KB; wall-clock moved within the host's noise.
- **Not chosen:**
  - **Invalidating from the catalog write paths in process.** Correct only while one process both writes the catalogs and runs ingest, and every catalog write path (create, update, delete, fork, restore, the boot seed, a calc rule's output change) would have to remember to call it.
  - **A time-to-live.** A type created by an operator would be refused for up to the TTL, the failure mode #1279 names.
  - **Fingerprinting the list queries' results.** Exact by construction, but Postgres would still evaluate the wide reads (each type's writer subselect) on every batch; the row versions answer the same question for a fraction of the work, and the drift guard keeps them in step.
  - **A trigger and a generation counter.** A schema change and a trigger on four tables for what a read of row versions already answers.
