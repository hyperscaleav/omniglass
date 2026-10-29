---
title: "The metric lane is narrow rows on a series dimension, partitioned by month"
date: 2026-10-29
status: Accepted
summary: "A metric sample is (id, series_id, ts, value) in metric, range partitioned by month from birth and rolled by the server; what it samples is a metric_series row (type, owner arc, instance, provenance, source, collecting node); a command's intended rows live in metric_declared, never pruned. The metric floor becomes the latest row within retention (amending ADR-0080); property keeps its floor and is not partitioned. A partition-routing miss is retried, never refused."
refs: [1275, 420, 1265, 1080, 1274, 417, 1417]
---

- **Date:** 2026-10-29 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [samples](/architecture/properties/), [admin guide](/guides/admin/), [testing](/contributing/test-driven/)
- **Decision:** (#1275, with #420's partitioning half, under the approved #1265.)
  1. **A series dimension.** `metric_series` holds one row per series, keyed by a bigint identity:
     the type, the owner arc (and `owner_id`, its one filled column), the instance, the provenance,
     the source, and `collector_node_id`, the node whose task collected it. Its unique key
     `(owner_id, metric_type_id, instance, provenance, source, collector_node_id)`, nulls not
     distinct, leads with the owner, so it is also the logical key's index; every reader asks for an
     owner's series of a type and merges the ones it finds. It is named for its lane: `property` is
     not narrowed, so the dimension is the metric lane's. The collecting node is a record, not a
     foreign key, so a deleted node leaves its series' history.
  2. **A narrow hot tier.** `metric` is `(id, series_id, ts, value)` plus a calculated sample's rule
     lineage in nullable columns, which cost nothing when empty. It is range partitioned by UTC
     month on `ts` from birth, has no primary key and no unique `(series_id, ts)` (ADR-0061), one
     btree `(series_id, ts desc, id desc)` (descending id, the order every latest read takes, where
     #1275 wrote `id`), and block range indexes on `id` and `ts`. It has no foreign key to the
     series: deleting an owner removes its series in the delete's transaction, and its samples,
     invisible to every reader, leave with their month or the next retention pass, rather than
     one statement deleting a quarter of the table.
  3. **The provenance split.** Observed and calculated samples are the hot tier; a command's intended
     rows are `metric_declared`, a plain table with the command and event lineage, never pruned.
     The lineage CHECK becomes structural: provenance is the series', and the hot tier has no
     command or event column. An event rule reads the hot tier only, so a command's intended row is
     no longer taken for a series' newest sample by the rule's newest check, its preview or its
     reconcile: it never was an observation (the one reader whose result changes, and only where a
     command targets a metric an event rule watches).
  4. **One create-copy-swap.** Three migrations: the narrow tables are created beside the wide one
     (renamed `metric_wide`), with a partition for every month it held a sample in and the next two;
     a one-time backfill copies every row, keeping its id, and refuses unless the counts agree; the
     wide table is dropped. Each has a full down.
  5. **The roll.** The server keeps this month and the next two partitioned, at boot before ingest
     starts (refusing to start when it cannot) and on every retention pass, creating a partition
     detached and attaching it under a five-second lock timeout. A partition-routing miss (SQLSTATE
     23514 naming no constraint) is not a refused write: the batch's months are made and it lands
     once more, and a miss after that is redelivered. A month is made for a sample however far its
     time (a collected key keeps its producer's time), and a past month leaves again with the next
     pass. No `pg_partman`, no `pg_cron`, no default partition (it would forbid detaching
     concurrently).
  6. **Retention.** A month wholly past the metric window and every type's own window is detached
     `CONCURRENTLY` and dropped; the chunked prune keeps the window exact inside the months that
     remain, by TID, a month at a time. The metric floor becomes **the latest row within retention**
     (amending [ADR-0080](#adr-0080-retention-is-provenance-aware-never-declared-never-the-latest-row-per-series)):
     a quantity stores every observation, so a series still measured always has a recent row, and
     one with none inside its window reads as no reading within retention; the task's liveness
     (#1264) will say when it was last measured. A calc rule writes an unchanged metric output
     again once its newest row is twelve hours old (under the shortest window a type may keep), so
     an output a rule still produces keeps a row within retention; one whose rule is not drained
     for longer than the window (every input transition-only and unchanged) reads as no reading.
  7. **Property is not partitioned** (#1265 open decision 2): its rows are transitions, sparse by
     construction, and its floor is unchanged. Recorded here for the architect's veto.
- **Context:** the epic's measurements put a wide metric row at 126 bytes of heap and 91 of index
  (rebuilt), most of it the series repeated on every observation; #1265's design review ruled out
  every ingest-side filter, so the row itself had to shrink. Measured by `make footprint` on the same
  host and build before and after (five runs each, every held figure identical across runs): heap
  126.0 to 60.2 bytes a row, index after `REINDEX` 90.6 to 39.4, live index 133.9 to 137.8 before and
  59.1 after, WAL 458.5 to 460.7 bytes an observation before and 304.4 to 315.4 after; statements per
  batch unchanged at 18. Partitioning from birth folds #420's swap into this one (two swaps on one
  table being one too many); `event`, `log_line` and `node_log` are #1417.
