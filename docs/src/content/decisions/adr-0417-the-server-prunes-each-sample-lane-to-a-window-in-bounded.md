---
title: "The server prunes each sample lane to a window, in bounded chunks"
date: 2026-10-28
status: "Accepted"
summary: "The server prunes metric and property samples to a window per lane. The windows are install-wide settings in a platform-domain, client-visible retention namespace (metric 14 days, property 90, trend 730 for #1276; 1 to 36,500 days), not cascade-resolved. A pass runs at start in bus.Server beside the receipt prune, then hourly, or a minute later while a lane is behind. It deletes in chunks, each owner arc its own statement. Beyond ADR-0080's floor (never declared, never a series' latest) it keeps a command's lineage rows, and it treats a row as latest under both reader orders (ts then id, and id). The newer-row check is a limited scalar subquery, so it stays a per-row index search on every arc; that adds metric_arc_owner_idx. PruneSamples is rewritten from a whole-table ranking to cutoff-first"
refs: [417, 1265, 591, 419, 420, 1276, 801]
---

- **Date:** 2026-10-28 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [settings](/architecture/settings/), [admin guide](/guides/admin/)
- **Decision:** (#417, the first of its three PRs, ruled 2026-10-28.)
  1. **One window per sample lane, install-wide.**
     - A platform-domain `retention` settings namespace holds `metric_days` (14), `property_days` (90) and `trend_days` (730, read once hourly trends exist, #1276).
     - Each is whole days from 1 to 36,500. The ceiling matters: a cutoff a billion days back overflows pgx's timestamp encoding and reaches Postgres as a date in the far future, which every row is older than. The pass refuses a lane whose stored window is out of range, rather than pruning it.
     - They are global settings first, as ruled on #1265, not cascade-resolved as the storage page's design proposed: a window is a fact about the install's storage, not something a location or a group would scope.
     - The namespace is client-visible, since a window is no secret and a history chart can say how far back it reaches.
     - Retention is the first integer setting the console edits, so the Settings page now writes a number as a number (`wireValue`); it had sent the typed text, which the API refused.
  2. **A pass runs beside the receipt prune.**
     - `bus.Server` runs it at start, on a goroutine of its own, as it already runs the receipt prune, the settlement sweep, the rule reconcile and the calc tick. It decides nothing about #419's background-work spine that those loops have not already decided.
     - A pass that kept up runs again in an hour. One that left a lane behind, by its chunk budget, a failure or its own five-minute deadline, runs again in a minute. Each lane has a deadline of its own, so a slow lane never starts the next on an expired context.
     - The pass itself (`retentionPass`) holds no clock and no database, so it is tested with neither.
  3. **Bounded chunks.**
     - A `PruneSamples` call deletes at most a chunk of one lane, one owner arc per statement and each its own short transaction.
     - A pass deletes a lane chunk by chunk until a chunk comes back short, or until it has spent 40 chunks of 5,000 rows, and then names the lane as behind.
     - A first pass over a large backlog therefore never holds the database, and a backlog drains at up to 200,000 rows a lane a minute.
  4. **What a prune spares.** ADR-0080's floor holds (never a declared row, never the latest row of a series), with two refinements:
     - **Latest under both orders.** The latest-value reads order a series by ts then id, the health reads by id. After a clock step-back (#801) the two disagree, so a row is deleted only when some row of its series has both a later-or-equal ts and a higher id. Neither reader's latest row is ever removed.
     - **Command lineage stays.** A command's intended row (`command_id` set, one per settleable command) is kept. The command table is never pruned and reads its intended value back from that row, so pruning it would turn a settled command's history into a fire-and-forget one.
  5. **Cutoff first, then a per-row index search.**
     - The first `PruneSamples` ranked every row of each table with `row_number()` before applying the cutoff: a full scan and sort per pass, the 2026-10-27 storage review's finding.
     - The rewrite takes old rows by the cutoff, then checks each for a newer row of its series with a scalar subquery limited to one row, walking up from the candidate's ts. Walking down from the newest row would first pass every row a node's clock future-dated before it was corrected, since the id test is a filter rather than part of the index condition (measured: 2.5 s a chunk against 46 ms, with 2,016 skewed rows a series). An `EXISTS` is not used: Postgres flattens it into a semi join, which on PG18 at 2.5 million rows it ran as a hash over the whole table, 1.5 s a chunk against 61 ms for the per-row search (the 2026-10-28 review).
     - Every arc that has samples is searched through an index:
       - `metric_owner_idx` and `property_series_idx` for a component;
       - `property_system_owner_idx` and `property_location_owner_idx` for a system or a location;
       - a new partial `metric_arc_owner_idx` for a metric a system or a location owns (a calc rule's output).
     - `TestPruneSamplesReachesAnIndexOnEveryArc` holds each search with the accesspath instrument: the right index, with a condition bound to the candidate row. The new index is partial on `component_id is null`, so a component's sample (every node probe and driver poll) pays nothing for it.
  6. **The floor's amendment waits.** "The latest row within retention" waits for `ts` partitioning (#420), where dropping a partition could otherwise remove a quiet series' only row, and for declared rows moving to a table of their own (#1265).
- **Context:** Nothing pruned the sample tables. At the lab's cadence the metric table grows by millions of rows a day, and a hosted instance has to stay bounded. `PruneSamples` shipped ahead of any caller (#591) to fix the floor first; this is its first caller.
- **Known limit:** finding the old rows walks every page the `ts` BRIN cannot rule out. Once vacuum frees pruned pages and ingest reuses them, a range holding both old and new rows matches every cutoff, and BRIN ranges widen and never narrow. Over time a pass's last, short chunk reads much of the table. Hence the hourly steady state; `ts` partitioning (#420) ends it by dropping whole partitions.
- **Not chosen:**
  - **A cascade-resolved window per location or group.** No ruling or use case needs it, and the settings engine's platform domain is the install-wide level.
  - **Waiting for #419's clock.** The server already runs five ticker loops; one more, beside the receipt prune, is the same pattern.
  - **A btree on `ts` for the candidates.** It would keep the candidate search narrow, but every metric row would pay for it on insert. Partitioning is the fix that costs ingest nothing.
