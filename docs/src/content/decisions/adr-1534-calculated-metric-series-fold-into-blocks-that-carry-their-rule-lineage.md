---
title: "Calculated metric series fold into blocks that carry their rule lineage"
date: 2026-11-07
status: Accepted
summary: "A calculated metric series folds into metric_block rows as an observed one does, each block carrying the one calc rule lineage (source_rule, source_rule_id, source_rule_version) its samples were written under, null for an observed block. A span splits at every lineage change and the count rule applies to each lineage alone. The block key becomes a UNIQUE NULLS NOT DISTINCT constraint over series, span start, span and the lineage, since a primary key cannot hold the nullable lineage. A late calculated sample merges into the block of its lineage that covers it or starts one. MetricBlockSamples reads a range back from the blocks with each sample's lineage. Calculated series stay out of metric_trend, for lineage alone. A dense calculated series costs 7.25 bytes a sample in blocks against 167.10 raw; an observed block's null lineage costs its row 8 bytes, which moves the 5 min figure to 8.70."
refs: [1534, 1528, 1529, 1276, 1530, 1531, 61]
---

- **Date:** 2026-11-07 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/)
- **Decision:** (#1534; amends [ADR-1528](#adr-1528-observed-metric-samples-fold-into-lossless-blocks-sized-by-their-sample-count)'s decision 1, under which only observed series blocked.)
  1. **Calculated series block, with their lineage.** `metric_block` gains the raw row's lineage
     columns, `source_rule text`, `source_rule_id uuid` and `source_rule_version bigint`, null for an
     observed series' block and the calc rule's for a calculated one's. One block holds one lineage:
     every sample in it was written by the same rule, under the same name, at the same version.
  2. **The key carries the lineage.** A span of a calculated series holds a block for each lineage
     that wrote in it, so the key is `(series_id, span_start, span, source_rule_id,
     source_rule_version, source_rule)`. A primary key cannot hold the nullable lineage, so the key is
     the unique constraint `metric_block_key` with `NULLS NOT DISTINCT`, under which an observed
     series' span still has exactly one block and a fold or merge lands on it by `on conflict on
     constraint`. The id tells apart two rules of one name (a rule deleted and created again). The
     version tells apart a rule before and after an edit, which bumps it. The name is in the key
     because a rename keeps the version (`RenameCalcRule`), so the raw rows on either side of a rename
     carry different lineage at one version, and a block keyed without the name would read one side
     back under the other's name.
  3. **The count rule applies to each lineage.** Each UTC day's rows of one lineage are counted and
     folded alone: 4,080 or more make hour blocks, 170 or more a day block, and fewer wait for the ISO
     week, whose block for that lineage gathers the lineage's rows of every day of the week where it
     has no hour or day block. A day a rule was edited in can so hold a day block for the old version
     and a share of the week block for the new one. An observed series has one lineage, none, and folds
     exactly as before.
  4. **A late calculated sample merges by its lineage.** A batch is written by one writer, so it
     carries one lineage, and the merge
     ([ADR-1529](#adr-1529-a-late-metric-sample-merges-into-the-block-that-covers-it)) looks for the
     covering block of that lineage only: the hour block, else the day block, else the week block,
     where a calculated sample of a version its day never folded starts the week's block of its own
     lineage. A calculated sample landing in an announced hour is now late as an observed one is, so
     a calc write takes the trend lock shared as an ingest batch does, in the same order. A calc
     tick dates its output now, so in production a calculated sample is never late; the path serves a
     recompute or backfill that writes past hours.
  5. **The read returns the lineage.** `MetricBlockSamples` unnests a series' blocks over a range,
     each sample with its block's lineage, in timestamp, then lineage, then landing order: exactly the
     raw rows' order within a lineage. `VerifyMetricBlocks` pairs each block with its own lineage's raw
     rows, so a sample read back under any lineage but its raw row's fails the check. A full join
     cannot match null lineage, so the check numbers the blocks for the pass and joins on that.
  6. **Calculated series stay out of the trends.** [ADR-1276](#adr-1276-hourly-trends-close-an-hour-a-day-after-it-ends-and-hold-back-the-prune)
     excluded them with no reason beyond lineage, and "sparse already", the storage page's gloss, does
     not hold for a dense calculated series. Lineage is the reason, and it is recorded here: a trend
     row is one per series per hour with no place for a rule version, so an hour spanning an edit would
     fold two rules' output into one minimum, maximum and sum, and trend nothing that says so. A calc
     series' late sample marks no trend hour. Trending calculated series, with or without lineage on
     the trend row, is for #1531's trend setting to decide.
- **Context:** #1528 folded observed series only. A calculated sample's raw row costs more than an
  observed one's, its lineage on every row, and a dense calc series (a rollup per component at 30 s)
  is as dense as what it reads. Measured by `TestCalculatedBlockFootprint` (`make footprint`): 30
  calculated series at 30 s for one ISO week, the rule edited mid-week so each series' edit day splits
  into two day blocks, 616,881 samples in 240 blocks:

  | | Bytes a sample |
  |---|--:|
  | blocks, with lineage | 7.25 |
  | raw rows, with lineage | 167.10 |

  The lineage costs a calculated block about 70 bytes with its key, a few hundredths of a byte a
  sample. An observed block's three null columns give its row a null bitmap, 8 bytes, which moves
  `TestBlockFootprint` by under 2% at every cadence but 5 min: its day blocks of 288 samples sit at
  the size four rows fill a heap page by, and 8 bytes more costs a page for every few blocks, 8.01 to
  8.70 bytes a sample. The test now holds this layout's figures and logs #1525's beside them.
- **Not chosen:**
  - **Lineage on the series.** A series outlives its rule's edits: the edit bumps the version and the
    series keeps writing, so lineage is a property of the samples, not the series.
  - **One block a span, lineage per sample.** Three more arrays (or a run-length lineage array) keep
    one block a span, but the read must then split every block to find a version, and the merge and
    the check gain a per-sample lineage comparison. Rule edits are rare, so a block a lineage costs
    an extra row a few times a rule's life.
  - **A surrogate key with the natural key unique.** An id column costs every block 8 bytes and its
    index for nothing a reader uses; the unique constraint is the key.
  - **Non-null lineage defaults for observed blocks** (an empty name, version 0, the nil uuid). They
    avoid the null bitmap but cost an observed block 25 bytes against its 8, and put a fake rule on
    every observed block.
