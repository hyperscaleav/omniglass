---
title: "Fleet entities are created in batches, one collection per call"
date: 2026-11-23
status: "Accepted"
summary: "Locations, systems and components each take an AIP-233 batch create (POST /locations:batchCreate, /systems:batchCreate, /components:batchCreate): a list of the single create's own bodies, run in order in one transaction, each row the ordinary create in the caller's scope. validate_only rolls it back; a write stamps one batch id into every audit row as from_batch; a refused row refuses the batch and is named requests[i]; a batch over 1000 rows is a 413. Each route is gated as its single create is."
refs: [2368, 827, 2369]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [the CLI](/developer/cli/#creating-in-batches)
- **Decision:** (#2368)
  1. **A batch create per collection.** `POST /locations:batchCreate`, `POST /systems:batchCreate`
     and `POST /components:batchCreate` follow [AIP-233](https://google.aip.dev/233). The body is
     `{"requests": [...], "validate_only": bool}`, each request exactly the body the collection's
     single create takes, and the answer is `{"locations": [...], "batch_id": "...",
     "validate_only": bool}` (`systems`, `components`), each row exactly what the single create
     answers, in request order. There is no new row shape to learn: a client that can create one
     can create a thousand.
  2. **Atomic per call.** The rows run in order in one transaction, each through the single
     create's own transactional half, so a refused row refuses the whole batch and nothing lands.
     It is mapped as the single create maps it (the same status and message), with `requests[3]: `
     leading the detail and `body.requests[3]` as the error's location. Because the rows run in
     order, a row names a row an earlier request of the same batch created (a floor's `parent`, a
     system's parent) by name, exactly as it would once committed; rows of an earlier batch are
     committed and resolve the same way.
  3. **A dry run.** `validate_only: true` runs the same transaction, every check and every name
     rule, and rolls it back: a dry run refuses exactly what the write would. It answers 200 with
     the rows and the names the write would mint, their ids empty since the rows they named are
     gone; a write answers 201. The spec declares both.
  4. **Provenance in the audit detail.** A write mints a uuidv7 batch id, returns it as `batch_id`,
     and `storage.WithBatch` stamps it into the new image of every audit row the batch wrote, under
     `from_batch`, beside `from_template` (ADR-0071 keeps provenance in the audit detail only). A
     batch is not an entity: the id is a handle for `new->>'from_batch'` and nothing else stores it.
  5. **Bounded.** At most 1000 requests per call; more is a 413 naming the limit, so a client
     splits a bigger import (by building). The body stays under the server's 1 MiB cap.
  6. **Gated as the single create.** Each route takes its collection's create permission
     (`location:create`, `system:create`, `component:create`), and each row needs what its single
     create needs: a component naming a system needs `system:update` over it. There is no import
     permission. A create gate is what the MCP classifier reads as a write, so each route is an MCP
     tool confirmed as one.
- **Context:** Importing a fleet that already exists elsewhere (a spreadsheet, another system's
  records) one create at a time is thousands of calls and no rollback. CSV parsing and the agent
  recipe are #2369.
- **Consequences:** An import tool orders its calls: standards (the existing from-template route),
  then locations with parents before their children, then systems, then components, then slot
  assignments and declared values, linking each by name. A dry run of a later batch cannot see rows
  an earlier batch has not written yet, so an import dry-runs its first batch, writes it, and dry-runs
  the next against what landed.
- **Not chosen:**
  - **One plan object applied whole.** The first draft took one body carrying standards, locations,
    systems and components together and applied it in one call. Plan is the word for change
    management, a plan computed, applied and reconciled, and spending it on a bulk create would take
    it from that.
    One object is also a big bang: one shape that grows with every kind of row, where a batch per
    collection reuses the shapes the API already has and adds one kind at a time.
  - **POST, PATCH and DELETE on one `:batch` path.** The CLI generator names a command after the
    custom verb whatever the method, so the three would collide on one command; and a DELETE with a
    body is poorly supported by some clients and proxies.
  - **A body that is either an object or an array.** An MCP tool's input schema must be an object,
    and a union muddies the generated CLI flags and the typed web client.
  - **Deferred:** a batch of slot assignments, a batch of declared values, `batchUpdate` and
    `batchDelete`. Each is its own call on its own collection when it is built.
