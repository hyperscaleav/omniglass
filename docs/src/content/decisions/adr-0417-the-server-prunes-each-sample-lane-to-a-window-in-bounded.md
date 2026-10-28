---
title: "The server prunes each sample lane to a window, in bounded chunks"
date: 2026-10-28
status: "Accepted"
summary: "The server prunes metric and property samples, and log lines by severity band, to a window each. The windows are install-wide settings in a platform-domain, client-visible retention namespace (metric 14 days, property 90, trend 730 for #1276; 1 to 36,500 days), not cascade-resolved. A pass runs at start in bus.Server beside the receipt prune, then hourly, or a minute later while a lane is behind. It deletes in chunks, each owner arc its own statement. Beyond ADR-0080's floor (never declared, never a series' latest) it keeps a command's lineage rows, and it treats a row as latest under both reader orders (ts then id, and id). The newer-row check is a limited scalar subquery, so it stays a per-row index search on every arc; that adds metric_arc_owner_idx. PruneSamples is rewritten from a whole-table ranking to cutoff-first. A metric or property type can keep its own window (retention_days), set by a setRetention custom method that works on official types"
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
  7. **Log lines by severity band (#417, the second PR).**
     - The same pass keeps `log_line` and `node_log` by four bands rather than eight levels (`logline.RetentionBands`):
       - debug, 3 days;
       - info and notice, 14 days, and a line with no severity is kept with info;
       - warning, 30 days;
       - error, critical, alert and emergency, 90 days.

       These are `retention.log_debug_days`, `log_info_days`, `log_warning_days` and `log_error_days`, shared by both tables.
     - A log line has no floor: it is not a series' value, and an event that named one as its source keeps itself and drops the link (`on delete set null`).
     - Each band's prune is searched, not walked:
       - a new `(severity, ts)` index per table, which on `log_line` replaces the severity-only one it covers;
       - a partial `(ts)` index for lines with no severity.

       Each statement orders by its index's columns. A `LIMIT` with no `ORDER BY` let the planner prefer a sequential scan once retention made severity and time correlate: 2.4M rows read for a chunk that found none (the 2026-10-28 review). So the BRIN limit above does not apply to logs. Every prune, sample or log, takes its chosen ids as an array (`id = any(array(...))`), so the outer delete probes the primary key rather than a small table's planner hash-joining an `IN` over a sequential scan. `TestPruneLogsReachesItsIndex` holds the index search; `TestPruneStatementsKeepTheirShape` holds the order and the array, which a plan test with sequential scans priced out cannot see.
     - `event.source_log_line_id` gets the index its `on delete set null` needs. The action runs once per deleted line, and without the index each one scanned the never-pruned event table: 27.7 s for a 2,000-line chunk over 200k events, 8.8 ms with it.
     - A one-time backfill mapped every stored severity onto RFC 5424's canonical set, trimming whitespace as Go's `strings.TrimSpace` does, with its alias list held to `internal/logline`'s by a test. `node_log` writes are now normalized at the gateway, as `log_line` writes have been since #1145. A band never meets a stray word.
  8. **A type can keep a window of its own (#417, the third PR, ruled 2026-10-28).**
     - A nullable `retention_days` on `metric_type` and `property_type` overrides the lane's window; null is the lane's.
     - It is set by a custom method, `POST /metric-types/{name}:setRetention` and the property-type twin, which works on an official type. `PATCH` and the official read-only rule are untouched, and the boot seed's upsert never writes the column (a re-seed keeps it).
     - The method is audited as `set_retention` and gated like the retention settings: `<type>:update`, plus `platform:update`, which the handler enforces. The seeded Operator role holds the first and not the second.
     - `days` is required, so only an explicit null returns a type to its lane's window, and an empty body is refused.
     - The lane's own prune skips every overridden type. The pass reads the overrides and gives each type a lane of its own (`metric/<type>`), pruned by the same statements with a type predicate, so the floor, chunking and per-row index probe are unchanged. `TestPruneSamplesReachesAnIndexOnEveryArc` holds both forms.
     - A typed prune carries the window the pass read and deletes nothing unless the type still has it, so an override cleared or changed mid-pass never prunes by the old one.
     - A typed prune finds its type's rows through an index rather than reading every old row of the lane. For a metric a system or a location owns, that is `metric_arc_owner_idx`, which leads with the type; every non-component prune states `component_id is null` (true under the exclusive owner arc), so the index's partial predicate is provable. Without it the review measured 4,756 buffers a call at 400k rows; with it, 4.
     - Not shown in the console. The CLI and API set it.
- **Context:** Nothing pruned the sample tables. At the lab's cadence the metric table grows by millions of rows a day, and a hosted instance has to stay bounded. `PruneSamples` shipped ahead of any caller (#591) to fix the floor first; this is its first caller.
- **Known limit:** finding the old rows walks every page the `ts` BRIN cannot rule out. Once vacuum frees pruned pages and ingest reuses them, a range holding both old and new rows matches every cutoff, and BRIN ranges widen and never narrow. Over time a pass's last, short chunk reads much of the table. Hence the hourly steady state; `ts` partitioning (#420) ends it by dropping whole partitions.
- **Not chosen:**
  - **A cascade-resolved window per location or group.** No ruling or use case needs it, and the settings engine's platform domain is the install-wide level.
  - **Waiting for #419's clock.** The server already runs five ticker loops; one more, beside the receipt prune, is the same pattern.
  - **A btree on `ts` for the candidates.** It would keep the candidate search narrow, but every metric row would pay for it on insert. Partitioning is the fix that costs ingest nothing.
