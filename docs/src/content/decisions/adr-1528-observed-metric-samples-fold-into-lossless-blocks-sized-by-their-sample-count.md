---
title: "Observed metric samples fold into lossless blocks sized by their sample count"
date: 2026-11-07
status: Accepted
summary: "Each closed UTC day of an observed metric series folds into lossless metric_block rows of int4 millisecond offsets and float8 values in landing order: hour blocks at 4,080 samples in the day or more, one day block at 170 or more, and otherwise a share of its ISO week's block, which has no minimum. Blocks carry no hourly summaries (metric_trend holds them, ADR-1276), and timestamps are whole milliseconds. History stays retention.metric_days, whose default rises to 30; trends become a per-type setting. This slice writes blocks beside the raw rows; the late merge, the pending table, block reads and the early raw drop follow. Measured at 7.2 to 8.6 bytes a sample from 10 s to 1 h (#1525). TimescaleDB, pg_deltax, fixed hour blocks and month blocks are rejected on measurement (#1516, #1518)."
refs: [1528, 1265, 1516, 1518, 1522, 1525, 1276, 1275, 1527, 1532, 1529, 1530, 1531, 61, 79, 311, 417]
---

- **Date:** 2026-11-07 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/)
- **Decision:** (#1528, under the approved #1265: the compacted tier the architect ruled required on
  2026-11-05, its draft ADR Accepted with the 2026-11-05 amendments, recorded on #1265.)
  1. **A block is lossless and plain SQL.** `metric_block (series_id, span_start, span, n, offset_ms int4[], value float8[])`,
     keyed by `(series_id, span_start, span)` and removed with its series. `span` is `hour`, `day` or
     `week`; `span_start` is its UTC start, an ISO Monday for a week; `n` is its sample count, held
     equal to both arrays' lengths by a check; `offset_ms[i]` is sample i's milliseconds after
     `span_start`. A week is 604.8M ms, so `int4` never overflows, and no span is longer than a week.
     Samples keep their landing order (`ts`, then `id`) inside a block, so duplicate instants
     ([ADR-0061](#adr-0061-a-calculated-series-is-current-at-its-highest-id-not-its-newest-timestamp)) round-trip. Observed
     rows carry no lineage (the narrow lane keeps `event_id` and `command_id` on `metric_declared`,
     [ADR-1275](#adr-1275-the-metric-lane-is-narrow-rows-on-a-series-dimension-partitioned-by-month)),
     so a block drops nothing. Only observed metric series block: calculated series (as in ADR-1276),
     properties and declared rows do not.
  2. **The span follows the samples, not the configured cadence.** Once a UTC day has closed past the
     late-data horizon, each observed series' samples that day decide its span:
     - **4,080 or more** (170 an hour on average) cut it into **hour blocks**;
     - otherwise **170 or more** make it **one day block**. 170 offsets and values are the 2 KB at which
       Postgres compresses a row;
     - otherwise the day waits for its **ISO week**, and the week's waiting days fold into **one week
       block** when the week closes. The week block has **no minimum** and nothing stays raw for good: a
       series of one sample a week costs 1.20 times its raw row, which is negligible volume, and a
       minimum would need a third, permanent raw path (#1525).

     A cadence change, an outage, a spool replay or a bursty metric needs no special case: each day is
     decided by what it holds, so one series can carry hour blocks one week and day or week blocks the
     next. Every sample is in exactly one block. A week block holds only its week's days that earned no
     hour or day block.
  3. **The smallest span that qualifies wins.** Once a block passes the threshold, a longer span buys
     little. It costs three things: the raw rows wait longer for it, every late sample rewrites all of
     it, and a short read decompresses all of it (#1518).
  4. **Blocks carry no hourly summaries.** `metric_trend`
     ([ADR-1276](#adr-1276-hourly-trends-close-an-hour-a-day-after-it-ends-and-hold-back-the-prune))
     already holds count, minimum, maximum, sum, first and last per series per hour. The 24 h raw
     cutoff #1518 measured on summary-carrying day blocks is withdrawn: a daily poll in week blocks costs
     35.41 bytes a sample against raw's 103.48 (#1525).
  5. **Timestamps are whole milliseconds** (#1532). NTP-synced node clocks are accurate only to a few
     milliseconds; fine timing rides in a value or in the event and log lanes. A raw row and its block
     agree exactly, with no rounding rule between them.
  6. **History is one setting, and its default rises to 30 days.** Blocks are lossless, so they are the
     history, compressed. Operators keep the two windows they have: every observation for
     `retention.metric_days`, and the hourly trends for `retention.trend_days`. Whether a sample sits in
     a raw row or a block is internal, and how long raw rows live is not a setting. `metric_days`
     defaults to 30 (was 14), properties stay at 90 and trends at two years (#1531 moves the default).
  7. **Trends are declared on the metric type: on, off or auto** (#1531). **Auto**, the default, trends
     a series that averages at least one sample an hour. A sparser series gets none, and its blocks are
     kept for `retention.trend_days` instead: at 1 h and 24 h a trend adds 122 bytes a sample (14 and
     3.4 times the block) and saves under 0.2 ms on a 30-day read the week blocks serve in under 1 ms.
     The operator overrides it on the type or down the cascade, never a driver: whether a quantity is
     worth trending is the quantity's, and a product that needs otherwise defines its own local type.
     Retention windows stay operator-owned.
  8. **One pass, on the trend's marks.** The fold runs in the retention pass after the trend compaction.
     It folds a day only once the trend's compacted mark is past the day's end. By then every hour of
     the day has been announced and drained under ADR-1276's lock, so every sample that day has
     committed or has marked itself late. It folds up to two days a pass, oldest first. Each transaction
     takes up to 200 series of one day and writes their blocks. In the same transaction it advances
     `metric_block_mark`'s `blocked_through` and its series cursor, so a pass cancelled anywhere leaves
     no series-day half folded, and the next pass folds from the mark. The week fold has a mark of its
     own, `week_through`, which never passes `blocked_through`. Holding the mark row `FOR UPDATE`
     serializes the folds across servers. The first pass starts at the ISO Monday of the oldest day still
     inside the window.
  9. **A late sample marks its day dirty, and the day is rebuilt.** An observed sample landing in an
     announced hour already marks its trend hour dirty (ADR-1276). In the same insert it now marks its
     series' UTC day in `metric_block_dirty`, so ingest stays at 18 statements a batch. A pass rebuilds
     every dirtied day before `blocked_through` from its raw rows, up to 50 a transaction under the trend
     lock taken exclusive. The rebuild replaces the day's hour and day blocks and, once its week has
     folded, the week block. A day the late samples carry past 170 leaves the week block for a day
     block of its own.
  10. **Verification is one query.** `VerifyMetricBlocks` unnests every block whose span is still
      inside its window, pairs each sample with its raw row by landing order, compares the timestamp
      exactly and the value bit for bit (`float8send`), and names the blocks that differ.
      `BlockFold.Verify` runs it at the end of a pass.
- **Context:** #1265 rested on one figure, about 10.4 bytes a sample in hour blocks of SQL arrays,
  measured once at the 2 s lab cadence. #1516 measured hour blocks, pg_deltax and TimescaleDB on the
  same seeded series at product cadence. #1518 measured span sizing, and #1522 (PR #1525) the
  summary-free block at this rule. Bytes a sample after `VACUUM`, every relation counted:

  | cadence | span | #1525 | this slice, through the fold | raw |
  |---|---|--:|--:|--:|
  | 10 s | hour | 7.71 | 7.82 | 133.3 |
  | 30 s | day | 7.18 | 7.18 | 133.2 |
  | 60 s | day | 7.25 | 7.37 | 133.1 |
  | 5 min | day | 7.84 | 8.01 | 132.6 |
  | 1 h | week | 8.58 | 8.25 | 127.6 |

  This slice's column is `TestBlockFootprint`: one ISO week of #1525's generated series at its series
  counts, folded by the product pass. #1525 folded 30 days of them in a staging query. The 1 h figure
  is lower because this slice's week is whole, where #1525's 30 days ended on a partial week whose
  rows stayed under the threshold. At the fleet estimate on #1265 (about 2.6 billion samples a day),
  about 7.4 bytes a sample is about 570 GB for 30 days of history, against about 4.9 TB for 14 days of
  raw rows.
- **The build order, and what this slice leaves to the rest:**
  1. **This slice (#1528):** `metric_block`, the day and week folds, the dirty-day rebuild and the
     check. The blocks are written beside the raw rows, which stay authoritative. Nothing reads a block
     yet, and the raw rows still age out on the history window.
  2. **#1529:** a late sample merges into the block that covers it, so a day whose raw rows are gone
     can still take one. Until then, the rebuild reads raw rows.
  3. **#1530:** the gateway's series read over raw rows and blocks (raw reads, the current value, calc
     windows, settlement and backtest). Then sparse days move to `metric_pending` instead of holding
     their day partitions, and a day's raw partition is dropped once it is blocked, which leaves raw rows
     about two days old at most, and the blocks are pruned to the window.
  4. **#1531:** the trend setting on the metric type and the 30-day `metric_days` default.
- **Not chosen:**
  - **TimescaleDB Community.** Its columnstore cost 10 bytes a sample at 10 s to 60 s, worse than a
    block at this rule. It refuses an already partitioned table (42809), it grows the image by 635 MB,
    no managed Postgres offers its compressed edition, and its TSL sits beside an AGPL product (#1516).
  - **pg_deltax.** It refuses a late row into a compressed partition (55000), a restored dump cannot read
    its compressed partitions, a hot standby cannot read the table, and `DROP` orphans its companions
    (#1516).
  - **Fixed hour blocks.** Below about 170 samples an hour a block never reaches the 2 KB threshold and
    is stored uncompressed: 15 to 27 bytes a sample at 60 s to 5 min. `toast_tuple_target` does not move
    the threshold (#1516).
  - **Week or month blocks for everything.** Raw rows would wait up to a month at 133 bytes a sample,
    and late merges and short reads scale with the block. At 1 h a month block was 8% smaller than a
    week and merged nearly 6 times slower (#1518).
  - **A week minimum.** See decision 2.
  - **Hourly summaries on the block.** They duplicated `metric_trend`, at 2 to 9% of block bytes from
    10 s to 60 s and 54 to 70% at 1 h (#1518).
  - **Go-encoded Gorilla `bytea`.** Withdrawn 2026-10-27: not worth losing SQL-readable history.
  - **Microsecond `int8` offsets.** About 4 bytes a sample more, for digits a node clock cannot
    honour.
