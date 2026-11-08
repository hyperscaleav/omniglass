---
title: "A series read takes a folded day from its blocks and the rest from raw rows"
date: 2026-11-08
status: Accepted
summary: "Every reader of a metric series' samples (the bucketed read at every resolution, the raw read, the series API and the CLI) reads them through one Storage Gateway read over raw metric rows and unnested metric_block rows. A late sample lands raw and merges into its block, so a folded day holds it twice; the read takes each series-day of each lineage from one source: from its blocks when a block of its lineage covers the day (an hour or day block of the day, or its ISO week's block) and the day is intact (blockIntact, the merge's own test), else from its raw rows. Not chosen: stopping the raw insert of a merged sample. Samples come back by ts, then lineage, then landing order, which moves a bucket's last off the raw row id. Amends #1530's spec: ListMetricBuckets did not read unchanged, it read raw rows only."
refs: [1530, 1572, 1529, 1528, 1534, 1566, 1573, 1574]
---

- **Date:** 2026-11-08 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/)
- **Decision:** (#1572, the first slice of #1530.)
  1. **One series read.** `metricReadSQL` is a series' samples over a range: its raw `metric` rows
     unioned with its `metric_block` rows unnested, each sample with the lineage it was written under
     (`source_rule`, `source_rule_id`, `source_rule_version`, null for an observed sample).
     `MetricSeriesSamples` reads one series through it, `ListMetricBuckets` computes every bucket at
     every resolution over it, and `ListMetricSeries` (the API and CLI raw read) reads it newest first.
     It is a union of arms, so `metric_pending` (#1574) is one more arm, and a bucket of closed whole
     hours can later be served from `metric_trend` (#1566) beside it.
  2. **A day of a lineage is read from one source.** A late sample for a folded day lands raw and
     merges into its block ([ADR-1529](#adr-1529-a-late-metric-sample-merges-into-the-block-that-covers-it)),
     so the day holds it twice. The read takes a series-day of one lineage from its blocks when a block
     of that lineage covers the day, and from its raw rows otherwise. A block covers its own day, an
     hour or day block, or all seven days of its ISO week, a week block: the week fold takes the days
     of a lineage no hour or day block holds, and the merge puts a late sample of a folded day into a
     block of its lineage, starting one when it needs to. So every sample of a covered day is in its
     blocks, and every sample of a day no block covers is a raw row.
  3. **Only an intact day is read from its blocks.** The merge writes a late sample into its block
     only while the day is intact (`blockIntact`, its start inside the series' window). A day whose
     start has slipped past the window's cutoff took no late sample into its block, so its raw rows
     hold what the block does not, and the read takes it from them. `blockIntact` stays the one
     predicate: the merge, the trend recompute and the read agree about a sample.
  4. **Coverage is by the blocks that exist, not the marks.** A day the fold has passed may have no
     block of a lineage yet (a sparse day before its week folds, a calculated sample of a rule version
     the day never folded), and its raw rows are then the only copy. Reading the blocks that are there
     needs no mark and no lock, and is right in the middle of a fold, whose transaction writes a day's
     blocks with its mark.
  5. **Order.** Samples come back by `ts`, then lineage (version nulls first, rule id, rule), then
     landing order: the raw row's id, or the sample's place in its block, which holds one instant's
     samples of one lineage in their rows' id order. One instant's samples of one lineage are all raw
     rows or all in one block, so the order is the same before the fold and after. A reader of several
     series of one instance breaks a tie by series. A bucket's last (`ListMetricBuckets`) was the last
     landed row of the bucket's newest instant (`m.id desc`); it is now that instant's sample of the
     newest series, then the newest lineage, then the last landed. It differs only where an observed
     and a calculated series of one instance, or two lineages, share the newest instant.
  6. **The raw rows stay.** Nothing in this slice drops a raw row. The early drop and the block prune
     (#1574) rely on decision 2: a covered intact day's raw rows are then redundant. They must keep
     decision 3's day covered: once a covered day's raw rows are gone, a day past the cutoff has to be
     read from its block (and from the raw rows of the late samples that did not merge) rather than
     from raw rows alone.
- **Amendment.** #1530's spec said bucketed reads were unchanged by the block reads. That was wrong:
  `ListMetricBuckets` read raw `metric` rows only, so it would have lost every folded day once the raw
  rows went. Ruled on #1530 (option A): bucketed reads go through the series read at every resolution;
  trend-served buckets are #1566.
- **Context:** The raw arm drops a covered row with an anti-join against the set of covered (day,
  lineage) pairs, which the planner hashes once. The week's days are a list of seven: with
  `generate_series` the planner guessed a thousand rows a block and sorted every raw row in range for a
  merge anti-join, spilling to disk. Measured on one ten-day series at one sample a second (864,000
  samples, seven days folded): hourly buckets read in about 1.3 s, most of it the sort that picks each
  bucket's last, and the raw arm's anti-join costs about 0.1 s over the scan. The raw read now reads
  the window's samples and keeps the newest `limit`, where it read newest first and stopped; at the
  API's 24-hour default that is one day of samples.
- **Not chosen:**
  - **Stopping the raw insert of a merged sample.** A late sample of a day past the cutoff, of a week
    the week fold has not reached, or of a day the fold has not reached does not merge and has to land
    raw anyway, so the read would still need to know which days its blocks cover. The raw table would
    stop being a complete record while it is still the only copy of every unfolded day, and
    `VerifyMetricBlocks` would lose the raw rows it holds a block to.
  - **Coverage by the block marks.** See decision 4.
  - **Per-hour coverage.** A lineage's day is folded at one span, so the day is the unit the blocks
    cover, and a day key keeps the covered set small.
