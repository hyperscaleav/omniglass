---
title: "Day blocks and span sizing are measured at 10 s to 1 h cadence (#1518)"
date: 2026-11-05
refs: [1518, 1265]
---

The #1516 shootout measured hour blocks at product cadence and found the cost jumps wherever a
block row stays under Postgres's 2 KB compression threshold. `make compress-bench` now asks the
follow-up: how long a block should be at each cadence, what the hourly summaries a longer block
carries cost, and what a late sample costs to merge into one. It runs 10 s, 30 s, 60 s, 5 m and
1 h, with 24 h as the reference, over 30 days of the same seeded series (duplicate instants,
ADR-0061). No product schema, ingest or retention change, and it does not choose a format.

- **F0 and F2** (the #1516 hour blocks, `timestamptz[]` or millisecond offsets beside
  `float8[]`) run at hour and day spans everywhere, and at week and month spans at 1 h. A block
  longer than an hour (`/day`, `/week`, `/month`) also carries per-hour summary arrays: count,
  min, max, sum, first and last for every hour of the span, so an hourly bucketed read never
  opens the values.
- **F5** (TimescaleDB on day chunks) runs everywhere as the extension reference, plus month
  chunks (`F5/month`) at 1 h. F1, F3 and F4 are dropped: the #1516 tuning moved nothing, and
  pg_deltax's fit answers stand.

Every figure is the median of 3 runs. Every cell round-tripped exactly, samples, hourly
buckets and every block's hourly summaries, before and after the late merge. The full tables,
with ranges, are what `make compress-bench` prints.

Bytes per sample after `VACUUM`, every table, index and TOAST relation counted:

| candidate | 10 s | 30 s | 60 s | 5 m | 1 h | 24 h |
|---|--:|--:|--:|--:|--:|--:|
| raw | 133.27 | 133.22 | 133.06 | 132.64 | 127.57 | 102.14 |
| F0 | 10.82 | 12.72 | 19.66 | 30.49 | 185.38 | 184.92 |
| F0/day | 8.64 | 9.28 | 9.98 | 17.04 | 84.94 | 476.71 |
| F0/week | | | | | 29.32 | |
| F0/month | | | | | 26.32 | |
| F2 | 7.78 | 13.67 | 15.41 | 26.50 | 177.72 | 177.25 |
| F2/day | 7.21 | 7.59 | 8.16 | 10.73 | 72.78 | 476.53 |
| F2/week | | | | | 26.04 | |
| F2/month | | | | | 24.07 | |
| F5 | 10.08 | 10.04 | 10.03 | 11.83 | 25.24 | 434.34 |
| F5/month | | | | | 9.55 | |

The share of block bytes the hourly summaries take, and the share of block rows whose values
Postgres compressed:

| cadence | F2 summary share | F2/day summary share | F2 rows compressed | F2/day rows compressed |
|---|--:|--:|--:|--:|
| 10 s | 1.5% | 1.9% | 80% | 80% |
| 30 s | 2.6% | 5.3% | 0% | 80% |
| 60 s | 4.7% | 9.4% | 0% | 80% |
| 5 m | 13.6% | 23.6% | 0% | 80% |
| 1 h | 24.3% | 69.7% | 0% | 0% |

At 1 h, F2/week is 60.2% summaries with 70% of rows compressed, F2/month 53.9% with 80%. Rows
compressed tops out at 80% because the generator's noisy fifth (full-precision values) never
compresses; the other four fifths cross the threshold together.

**The span rule.** Per cadence, the shortest span at which at least half the block rows
compress; where none does, the span with the fewest bytes a sample; raw where no block beats the
narrow row:

| cadence | F0 span | F0 B/sample | F2 span | F2 B/sample | raw B/sample |
|---|---|--:|---|--:|--:|
| 10 s | hour | 10.82 | hour | 7.78 | 133.27 |
| 30 s | hour | 12.72 | day | 7.59 | 133.22 |
| 60 s | day | 9.98 | day | 8.16 | 133.06 |
| 5 m | day | 17.04 | day | 10.73 | 132.64 |
| 1 h | week | 29.32 | week | 26.04 | 127.57 |
| 24 h | raw | 102.14 | raw | 102.14 | 102.14 |

The raw cutoff is 24 h: a series at 24 h or slower stays raw, because a day of one sample is
one row, and any block of it costs more than the row. Between 1 h and 24 h nothing was measured,
so the cutoff is only known to sit in that range. F2's int4 offsets halve the timestamp column,
which keeps a 30 s hour row under the threshold where F0's crosses it, so F2 needs a day span one
cadence sooner.

**A longer span costs the merge, not the read.** A late sample rebuilds its whole block
(`insert ... on conflict do update`, after a checkpoint, so the first write after each
checkpoint pays full-page images):

| cadence | candidate | late merge ms | late merge WAL B |
|---|---|--:|--:|
| 10 s | raw | 0.55 | 20296 |
| 10 s | F2 | 1.25 | 53904 |
| 10 s | F2/day | 44.03 | 294336 |
| 60 s | F2/day | 3.62 | 87312 |
| 5 m | F2/day | 1.66 | 57624 |
| 1 h | F2/week | 4.31 | 81464 |
| 1 h | F2/month | 24.54 | 165392 |
| any | F5 | 0.53 to 1.15 | about 11400 |

A day block at 10 s merges 35 times slower than an hour block and writes 5.5 times the WAL, for
7% fewer bytes. The span rule already keeps 10 s on hour blocks. At 1 h, a month block is 8%
smaller than a week block and merges nearly 6 times slower. TimescaleDB's late insert is the cheapest
of all, because it lands in an uncompressed region of the chunk rather than rebuilding a segment.

Reads barely move with the span. Every block holds the 30 d hourly bucketed read of one series
under 1.5 ms at every cadence (raw takes 62.54 ms at 10 s and 8.54 ms at 60 s), and the 24 h
raw read at a median under 5 ms. Compression time per 1000 samples roughly doubles from hour to day blocks
(0.8 to 1.7 ms at 10 s) and reaches 3.7 ms for a 1 h month.

**The summaries are the 1 h cost.** At 1 h an hour holds one sample, so each summary entry
restates it six ways, and they are 54 to 70% of the bytes from day to month spans. Subtracting
them, an F2/week block would hold about 10 B a sample. At 30 s and faster they cost under 6%.
Summaries coarser than an hour at slow cadences is a design question for the choice, not part of
this measurement.

**Month spans need int8 offsets.** An int4 millisecond offset from the block start overflows at
24.8 days, so `F2/month` stores `int8[]` offsets; every shorter span keeps `int4[]`.

**Parquet, recorded only.** Parquet row groups are meant to be 128 MB to 1 GB, so one row group
holds many series-days. A day block maps onto a contiguous run of rows (or a few pages) inside a
row group, not onto a row group of its own, and the hourly summaries correspond only loosely to
page statistics (min and max per page, no count, sum, first or last). A cold tier would rebuild
summaries in its own layout rather than carry the blocks over. Nothing was built.

Caveats, all deliberate:

- The containers run with `fsync` off, so compression and merge times measure CPU and WAL
  volume, not the disk.
- Each late merge follows a checkpoint, so its WAL includes full-page images: the worst case
  for one late sample, and an overstatement for a burst of them into one block.
- Block boundaries are `date_trunc` in UTC.
- Series counts scale with cadence (10 at 10 s up to 3000 at 1 h) so every cadence from 10 s to
  1 h holds 2.2 to 2.6 million samples; 24 h keeps 3000 series and about 92 thousand samples.
