---
title: "The command-targeted property names ride the kept registry"
date: 2026-10-28
status: "Accepted"
summary: "The catalog fingerprint covers command_type, and the ingest consumer keeps the property type names some command type targets beside its registry, rebuilt on the same fingerprint, and hands them to LandBatch on each batch; the property lane answers settlement's question from the batch instead of reading command_type in the batch's transaction. A node batch drops from 19 to 18 statements, with no new read. A node batch and an API push both carry the set; calc outputs and InsertPropertySamples read it in their own transaction as before"
refs: [1299, 1279, 1274, 1265]
---

- **Date:** 2026-10-28 | **Status:** Accepted | **Pages:** [samples](/architecture/properties/)
- **Decision:** (#1299.)
  1. **The fingerprint covers `command_type`.** `registryTables` gains it, so creating, retargeting or deleting a command type moves the catalog fingerprint (ADR-1279), and the next batch rebuilds. The statement that reads the targeted names (`commandTargetedSQL`) joins the registry list statements, so both drift guards hold it to the covered tables: dropping `command_type` from the fingerprint fails them by name.
  2. **The consumer keeps the set beside the registry.** On a rebuild, `registryCache` also reads `CommandTargetedProperties`, and every batch carries the kept set as `IngestBatch.CommandTargeted`. `LandBatch` seeds its `batchTx` with it, and `commandTargetedProperties` answers from the batch, the same pattern ADR-1274 set for the owner and the clock. The set is read only, so one set is shared across batches.
  3. **Both ingest lanes carry it; the other property writers read it themselves.** A node's batch and an API push both land through the consumer (`Server.land`), so both carry the kept set. A batch with no set (nil) reads it in its transaction, as before, and the property writers outside the consumer keep that path: calc outputs (in their rule's transaction) and `InsertPropertySamples` (the dev seed and tests).
- **Context:** #1274 left 19 statements on a node batch. One was settlement's `select distinct pt.name from command_type ... join property_type ...`, read in every batch carrying a property sample, though it changes only when a command type does. The fingerprint was already read once per batch, so covering one more table costs nothing per batch. Measured (`TestIngestFootprint`, exact; `BenchmarkIngestFootprint`, 300 batches, 3 runs each, back to back against #1298's branch on one host): **18 statements per batch**, allocations 596 to 581; wall time is within run-to-run noise on a local Postgres (about 2.0 to 2.4 ms either side), since the saved statement is one local round trip. Against a remote Postgres each round trip is a network hop.
  - The staleness bound is the registry's: the fingerprint is read before the catalogs, so the kept set is never older than the fingerprint it is stored under, and a command type created between two batches is honored by the second (`TestACommandTypeCreatedBetweenBatchesSettlesOnTheNext`).
- **Not chosen:**
  - **A cache inside the storage layer keyed by its own fingerprint.** Reading that fingerprint would cost the statement it saves.
  - **Folding the set into the registry type.** The registry is the collection package's name resolution; settlement's set is a storage concern the consumer only carries.
