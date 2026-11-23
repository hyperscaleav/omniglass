---
title: "Locations, systems and components each take a batch create, with a dry run (#2368)"
date: 2026-11-23
refs: [2368, 827]
---

`POST /locations:batchCreate`, `/systems:batchCreate` and `/components:batchCreate` (`omniglass
location batchCreate` and its siblings, each an MCP tool) take a list of the single create's own
bodies and run them in order in one transaction, each row the ordinary create in the caller's scope
with its own audit row. A floor names the building an earlier request of the same batch creates as
its parent, by name. `validate_only` runs the same transaction and rolls it back, answering 200 with
the rows and the names a write would mint. A write answers 201 and stamps one batch id into every
audit row as `from_batch` (the new `storage.WithBatch`, beside `WithTemplate`). A refused row
refuses the batch, mapped as its single create maps it and named `requests[3]`, and a batch over
1000 rows is a 413 naming the limit. The decision is
[ADR-2368](/architecture/decisions/#adr-2368-fleet-entities-are-created-in-batches-one-collection-per-call).
