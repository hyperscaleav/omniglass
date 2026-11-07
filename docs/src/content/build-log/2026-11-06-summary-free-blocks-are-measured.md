---
title: "Summary-free blocks are measured at the span rule, the week minimum and trend cost (#1522)"
date: 2026-11-06
refs: [1522, 1265]
---

The draft compacted-tier ADR on #1265 drops the per-hour summaries from the block and leaves
hourly reads to `metric_trend`. `make compress-bench -plain` measures that design and closes
its open questions 3, 4 and 6. The block is F2 with nothing else: `metric_block (series_id,
span_start, span, n, offset_ms int4[], value float8[])`. Each series-day picks its span from
its own count:

- 170 samples an hour or more on average (at least 4080 in the day) makes hour blocks.
- 170 or more in the day makes one day block.
- Anything sparser waits for its ISO week's block.

The datasets are the #1518 ones (30 days, with duplicate instants under ADR-0061), started on
a Monday. No product schema, ingest, retention or `metric_trend` change, and it does not choose
a format. Every figure is the median of 3 runs. Every cell round-tripped exactly: samples,
hourly buckets, the trend's buckets, and every series-day's span against the rule.

**Question 4: bytes a sample at the rule span.** F2p is the summary-free block. The F2 column
is the same span carrying #1518's hourly summaries, rerun here:

| cadence | span | F2p B/sample | F2 B/sample | draft | F2p rows compressed |
|---|---|--:|--:|--:|--:|
| 10 s | hour | 7.71 | 7.78 | 7.7 | 80% |
| 30 s | day | 7.18 | 7.59 | 7.2 | 80% |
| 60 s | day | 7.25 | 8.16 | 7.4 | 80% |
| 5 m | day | 7.84 | 10.73 | 8.2 | 80% |
| 1 h | week | 8.58 | 26.26 | 10.4 | 64% |
| 24 h | week | 35.41 | | | 0% |

The draft's figures hold at every cadence, and at 1 h F2p beats the draft's 10.4 B. Without
summaries, the cost of a sample no longer climbs as cadence slows: it stays between 7.2 and
8.6 B from 10 s to 1 h. At 1 h only 64% of rows compress. The 30 days end on a two-day partial
week whose 48-sample rows stay under the threshold, on top of the generator's noisy fifth that
never compresses.

**Question 3: the week minimum is 2 samples a week.** For this question every block was a
whole ISO week: 3000 series over 4 weeks, against raw rows:

| samples a week | raw B/sample | week block B/sample | block / raw |
|--:|--:|--:|--:|
| 1 | 126.54 | 151.98 | 1.20 |
| 2 | 114.07 | 83.96 | 0.74 |
| 3 | 109.70 | 58.64 | 0.53 |
| 4 | 106.97 | 47.97 | 0.45 |
| 7 | 105.49 | 32.30 | 0.31 |
| 14 | 100.54 | 22.60 | 0.22 |
| 24 | 112.20 | 18.57 | 0.17 |
| 48 | 116.97 | 16.12 | 0.14 |
| 168 | 121.95 | 8.09 | 0.07 |

One sample a week is the only count at which a block loses to the row it replaces. This
overturns #1518's raw cutoff at 24 h. #1518 saw a 24 h block cost 476 B a sample, but that was
a day block carrying summaries. Without summaries, a 24 h series in week blocks costs a third
of raw (35.41 against 103.48 B over 30 days, 32.30 against 105.49 over whole weeks).

**Question 6: the trend pays for itself at fast cadence and not at slow.** The table shows what
`metric_trend` adds per stored sample, and the 30 d hourly bucketed read of one series from
each source:

| cadence | trend B/sample | F2p read ms | trend read ms |
|---|--:|--:|--:|
| 10 s | 0.38 | 109.85 | 0.66 |
| 30 s | 1.06 | 28.36 | 0.80 |
| 60 s | 2.07 | 16.81 | 0.58 |
| 5 m | 10.21 | 4.25 | 0.55 |
| 1 h | 122.15 | 0.74 | 0.58 |
| 24 h | 122.12 | 0.23 | 0.16 |

The trend costs one row, 120 to 140 B, for every hour that holds a sample. At 10 s that is under 0.4 B a
sample, and it turns a 110 ms unnest into a 0.7 ms read: the summaries' job, done more cheaply
than #1518's per-block summaries. At 5 m it already costs more than the block itself. At 1 h
and 24 h each hour holds one sample, so the trend is a second copy of the data, 14 times the
block at 1 h and 3.4 times at 24 h. It saves 0.16 ms and 0.07 ms on a read the week blocks
already serve in under a millisecond. Hourly trend rows for slow series buy nothing a block
read does not.

**Mixed cadence keeps every series-day on its own span.** Over one ISO week, the week minimum
held at 2:

- Series 1 polls 60 s, then turns to 30 s at Wednesday noon. It stays on day blocks every
  day: 1459 samples Monday, 2212 Wednesday, 2938 Sunday. A 30 s day (2880 samples) is below
  the 4080 an hour block needs.
- Series 2 polls 1 h, then turns to 30 s at Thursday noon. Monday to Wednesday (24 to 26
  samples a day) wait for the week block; Thursday (1485) and the days after go to day blocks.
- Series 3 reports three times a week. All three samples share one week block.

Every sample came back once and only once. Each block's `n` matched both array lengths, each
offset fell inside its span, and the read gave back exactly the generated series.

Caveats, all deliberate:

- The containers run with `fsync` off, as in #1518, so times measure CPU and not the disk.
- F2p's compression time (2.3 ms per 1000 samples at 10 s against F2's 0.8) is a staging query
  that sorts each series-day in SQL. It says nothing about a product compactor.
- No late merge was measured for F2p. A summary-free block rebuilds the same way #1518's did,
  minus the summary arrays.
- The trend is refreshed from the staged samples in one pass. Its incremental refresh cost is
  not measured.
- Block and week boundaries are `date_trunc` in UTC; the week is the ISO Monday.
