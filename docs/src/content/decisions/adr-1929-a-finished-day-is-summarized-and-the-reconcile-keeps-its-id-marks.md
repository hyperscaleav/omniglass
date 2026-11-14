---
title: "A finished day is summarized and the reconcile keeps its id marks"
date: 2026-11-14
status: "Accepted"
summary: "The event rule reconcile reads only the days past its mark through summarized block range indexes, not through partition pruning: each day's own id and ts index summarizes a range once it fills, and each retention pass, after its drop, summarizes every finished day's last range. The marks stay the newest committed id, not the sequence's high water, and no time bound is added, so a sample in flight or stamped late is still judged"
refs: [1929, 1896, 928, 1527]
---

- **Date:** 2026-11-14 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [alarms and actions](/architecture/alarms-actions/)
- **Decision:** (#1929.)
  1. **The marks stay `max(id)` committed.** The issue's spec read the upper mark off the metric
     identity sequence's high water and bounded the candidates scan by a time lower bound, so the
     planner would prune to the live days. The sequence's high water counts ids a batch in flight
     holds and has not committed, and a time bound drops a sample stamped before it, so either
     could let a sample land past a pass without any pass judging it. The reads stay on `id`, and
     the scan reads the ids past the mark first, on their own, so each day is reached through its
     `id` block range index.
  2. **A finished day's ranges are summarized.** A block range no summary covers reads as a match,
     so the id reads were cheap only once every finished day was summarized. A partitioned index
     takes no storage option, so a migration sets `autosummarize` on each existing day's own index
     (a `SHARE UPDATE EXCLUSIVE` lock, no rewrite), and the partition roll makes each new day's
     indexes with it before the attach adopts them. `autosummarize` takes a range once it fills; a
     day's last range never fills, so the retention pass summarizes every finished day's ranges.
  3. **The summary is a retention step of its own.** It runs after the drop, so no day about to go is
     summarized, on a deadline of its own, so a slow roll does not starve it, and a day that fails
     (its lock timeout) is named in the pass's error while the rest are still summarized. The
     partition roll at boot does not summarize: the retention pass runs once at start.
- **Consequences:** Each pass's marks read and touched scan fall from a finished day's whole heap to
  a few index pages (316 and 536 of a test day's pages to a handful), and the guarantee for a batch in
  flight is unchanged. Every retention pass re-summarizes each finished day, which reads only the
  unsummarized ranges and so costs little once a day is done. A day whose summary keeps failing is
  read whole by the reconcile until a pass succeeds, slower but never wrong.
- **Context:** On the corporate 3,000 soak the reconcile read about 212k blocks for its marks and 214k
  for its touched scan every minute, 79 % of it in finished days whose ranges were never summarized,
  competing with ingest on the same database (#1896).
- **Not chosen:**
  - **The sequence's high water and a time bound (the issue's spec).** They prune by partition but
    trade the in-flight and late-sample guarantee of the reconcile (#928) for it, when summarized ranges give the same reads without the trade.
  - **Rebuilding the parent indexes with the option.** A partitioned index cannot take it with
    `ALTER INDEX`, and a drop and recreate rebuilds every day's index under a lock ingest waits on.
  - **Summarizing in the partition roll.** The roll runs at boot before ingest and shares the pass's
    deadline; a summary there delays the start and can starve the drop behind it.
