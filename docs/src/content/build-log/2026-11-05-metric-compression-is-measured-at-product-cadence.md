---
title: "Metric compression is measured at product cadence, with pg_deltax and TimescaleDB beside the hour blocks (#1516)"
date: 2026-11-05
refs: [1516, 1265]
---

The storage efficiency epic (#1265) rested on one figure: about 10.4 B a metric sample in hour
blocks of SQL arrays, measured once, at the 2 s lab cadence. `make compress-bench` now measures
the candidates on the same seeded series, with duplicate instants (ADR-0061), on throwaway
Postgres 18 containers, at 2 s, 30 s, 60 s and 24 h. Every candidate gives back exactly what it
was given, sample for sample and bucket for bucket, or the run fails. The extensions run from
test-only images built from their official, unmodified releases (pg_deltax 0.2.1 under Apache 2.0,
TimescaleDB 2.31.0 under the Timescale License with its Apache 2.0 core), licenses installed beside
them. No product schema, ingest or Helm change. It does not choose an approach.

- **F0:** `float8[]` plus `timestamptz[]` per series per hour. Every block row carries `series_id`,
  the hour, count, min, max, sum, first and last.
- **F1:** F0 with `toast_tuple_target = 128` and lz4.
- **F2:** `float8[]` plus `int4[]` millisecond offsets from the hour.
- **F3:** F2 tuned like F1.
- **F4:** pg_deltax on the narrow row, segmented by `series_id`, ordered by `ts` then `id` (which
  keeps duplicates of one instant in landing order), on day partitions.
- **F5:** TimescaleDB's columnstore on the narrow row, on day chunks, segmented and ordered the
  same, plus an hourly continuous aggregate (`F5 cagg`).

At 24 h the run adds day blocks (`/day`) and month partitions or chunks (`/month`) beside the hour
blocks and day partitions. Every figure is the median of 3 runs; every cell round-tripped exactly.
The full per-cadence tables, with ranges, are what `make compress-bench` prints.

Bytes per sample after `VACUUM`, every table, index, TOAST and companion relation counted:

| candidate | 2 s | 30 s | 60 s | 24 h |
|---|--:|--:|--:|--:|
| raw | 133.25 | 133.22 | 133.06 | 102.14 |
| F0 | 8.16 | 12.72 | 19.66 | 184.92 |
| F1 | 9.69 | 13.53 | 19.66 | 184.92 |
| F2 | 7.12 | 13.67 | 15.41 | 177.25 |
| F3 | 7.54 | 13.67 | 15.41 | 177.25 |
| F0/day, F1/day | n/a | n/a | n/a | 184.92 |
| F2/day, F3/day | n/a | n/a | n/a | 177.25 |
| F4 | 9.47 | 17.02 | 18.67 | 1889.08 |
| F4/month | n/a | n/a | n/a | 140.85 |
| F5 | 8.42 | 10.04 | 10.03 | 434.34 |
| F5 cagg | 0.15 | 1.37 | 2.51 | 138.98 |
| F5/month | n/a | n/a | n/a | 41.03 |
| F5/month cagg | n/a | n/a | n/a | 146.56 |

Reads in ms, a 24 h raw read of one series, then a 30 d hourly bucketed read of it:

| candidate | 2 s | 30 s | 60 s | 24 h |
|---|--:|--:|--:|--:|
| raw | 8.37, 388.29 | 0.72, 24.21 | 0.45, 8.49 | 0.27, 0.18 |
| F0 | 15.02, 0.58 | 1.30, 0.64 | 0.68, 0.73 | 0.23, 0.15 |
| F1 | 15.32, 0.70 | 1.21, 1.02 | 0.68, 0.53 | 0.25, 0.14 |
| F2 | 18.61, 0.77 | 1.66, 0.88 | 0.78, 0.57 | 0.24, 0.20 |
| F3 | 18.71, 0.59 | 1.34, 0.90 | 0.67, 0.61 | 0.36, 0.12 |
| F4 | 29.06, 756.89 | 16.78, 85.43 | 16.87, 140.13 | 21.37, 101.39 |
| F4/month | n/a | n/a | n/a | 22.25, 76.54 |
| F5 | 6.14, 39.71 | 0.74, 6.66 | 0.45, 4.44 | 0.41, 2.31 |
| F5 cagg (bucketed only) | 1.19 | 1.11 | 1.04 | 0.55 |
| F5/month | n/a | n/a | n/a | 0.55, 0.66 |

Compression in ms per 1000 samples:

| candidate | 2 s | 30 s | 60 s | 24 h |
|---|--:|--:|--:|--:|
| F0 | 0.80 | 0.63 | 0.60 | 3.03 |
| F1 | 0.51 | 0.59 | 0.57 | 3.46 |
| F2 | 0.75 | 0.73 | 0.75 | 3.47 |
| F3 | 0.65 | 0.71 | 0.75 | 3.37 |
| F4 | 1.08 | 1.54 | 1.58 | 165.59 |
| F4/month | n/a | n/a | n/a | 12.29 |
| F5 | 0.35 | 0.40 | 0.42 | 6.58 |
| F5 cagg | 0.14 | 0.19 | 0.24 | 4.94 |
| F5/month | n/a | n/a | n/a | 0.79 |

**What the numbers say.**

- **At 2 s everything compresses about the same.** Every candidate lands between 7.1 and 9.7 B a
  sample, 14 to 19 times smaller than the raw row. F2's millisecond offsets are the smallest.
- **At 30 s and 60 s the block formats lose ground and F5 does not.** A 60 s hour block is 60
  samples, under 1 KB of arrays, so Postgres stores it uncompressed and F0 rises to 19.7 B. F5
  compresses up to 1000 rows of a series together whatever the cadence and holds 10.0 B.
- **The F1 and F3 tuning does not do what F1 and F3 assumed.** `toast_tuple_target` sets how far
  Postgres shrinks a row once it decides to compress, not when it decides, which is still the
  fixed 2 KB threshold. So F1 equals F0 at 60 s, and at 2 s lz4 compresses these arrays worse
  than the default pglz (F1 9.69 B against F0's 8.16 B).
- **Blocks answer bucketed reads from their own aggregates.** A 30 d hourly read of a block
  format takes under 1.1 ms at every cadence, because each block row already carries count, min,
  max and sum. F5 has to scan its columnstore (39.7 ms at 2 s) unless the cagg answers (1.19 ms);
  the raw row takes 388 ms. For a raw 24 h read F5 leads (6.1 ms at 2 s) and the blocks pay for
  unnesting (15 to 19 ms).
- **pg_deltax is the slowest at every read.** Its 30 d bucketed read at 2 s takes 757 ms, longer
  than the uncompressed raw row, and its raw 24 h read never drops under 16 ms.
- **At 24 h per-row overhead decides, and only month chunks beat the raw row.** One sample a day
  makes an hour block, or a day block, a block of one, so the blocks cost 177 to 185 B against
  raw's 102 B. Day partitions cost F4 1889 B a sample (its companions per partition) and F5 434 B.
  On month chunks F5 drops to 41 B; F4 on month partitions to 141 B.
- **The cagg is cheap where the data is dense.** It adds 0.15 B a sample at 2 s and 2.5 B at
  60 s, but at 24 h an hourly bucket holds one sample and the cagg costs more than the data.
- **Compression is cheap everywhere but pg_deltax at 24 h.** Every format compresses 1000
  samples in under 1.6 ms from 2 s to 60 s, F5 fastest. At 24 h on day partitions F4 takes
  166 ms per 1000.

**F0 at 2 s is 8.16 B, not 10.4.** Splitting one 2 s hour block into its columns on a bare
Postgres 18 shows why. The `timestamptz[]` column costs about 5.4 B a sample after Postgres
compresses it, whether timestamps carry millisecond or microsecond jitter. The `float8[]` column
costs anything from 0.1 B (a value that never changes) to 8.0 B (full-precision noise, which does
not compress). So F0 is about 5.4 B plus the value mix's share. The generator's mix is a fifth each
of constant, counter, two rounded walks and full-precision noise; 10.4 B means a value column of
about 5 B, a lab whose series were mostly full precision. The figure is a property of the fleet's
values, not of the format, which is why the table reports every cadence and the round trip
rather than one number.

Caveats, all deliberate:

- The containers run with `fsync` off, so compression time measures CPU, not the disk.
- F2 and F3 hold timestamps to the millisecond. The generator emits milliseconds, so they round
  trip; the node stamps microseconds today.
- Postgres compresses an array only once its row passes about 2 KB, which is why the blocks' cost
  jumps at 30 s and 60 s. `toast_tuple_target` does not move that threshold (the reading above).
- A block format's compression time is materializing blocks from a staged narrow table. F4 and
  F5's is the extension's compress call alone, after the same rows were loaded narrow.
- pg_deltax keeps a `_deltax_compressed` companion table per compressed partition; its bytes are
  counted.

**The fit questions.** Each answer is a statement run against a real server and what it returned
(`make compress-bench` prints every step).

**Q1, the month partitioning, the roll (#1275), retention (#417) and the ADR-0080 floor.**
pg_deltax accepts `deltax_create_table` on the product's month-partitioned `metric`, but it only
manages the day partitions it creates. The product's own month partition stays attached and is
never compressed, and draining the default partition is refused (SQLSTATE 428C9) until the
`GENERATED ALWAYS` identity becomes `GENERATED BY DEFAULT`. Its retention is a background worker
that dropped 3 compressed day partitions, companions included, within 55 s; it runs only in the
worker's target database and has no run-now call, so the product's retention pass cannot drive it.
TimescaleDB refuses `create_hypertable` on an already partitioned table (42809): adopting it means
the hypertable replaces the month partitioning and the roll. Its retention is `drop_chunks`, which
dropped 2 compressed chunks whole and left no orphans, the same whole-unit drop the ADR-0080 floor
assumes of a month partition today. With day chunks the unit is a day, not a month.

**Q2, a late row.** pg_deltax refuses an `INSERT` into a compressed partition (55000), and
`UPDATE` and `DELETE` (XX000). `storage.IsRefusedWrite` classifies 55000 as retryable, so the
consumer Naks the batch, retries it 5 times and Terms it: the whole batch, its on-time rows
included, is lost. A late row would need decompressing the partition first, or a classifier change
plus a dead-letter path. TimescaleDB accepts the late `INSERT` into a compressed chunk, the batch
lands and is acked, and the row reads back; `UPDATE` and `DELETE` are accepted too.

**Q3, DETACH and DROP, dump and restore, physical replication.** The retention pass's
`DETACH PARTITION CONCURRENTLY` is refused by both (0A000): pg_deltax while any partition is
compressed, TimescaleDB because a chunk is an inheritance child; `drop_chunks` is its path. A
pg_deltax `DROP TABLE` of a compressed partition is accepted but leaves all 13 companion relations
behind and still listed. A pg_deltax dump restores clean and then cannot be read (XX000, no
compressed partition info), and a hot standby cannot read the table at all ("cannot assign
TransactionIds during recovery"); only after promotion do the rows match. TimescaleDB's dump,
between `timescaledb_pre_restore` and `timescaledb_post_restore`, restores with matching rows and
3 of 3 chunks still compressed; its standby and its promoted standby match the primary exactly.

**Q4, image cost and managed Postgres.** The pg_deltax image is 669.0 MB, 24.8 MB over
`postgres:18` (one 12.7 MB shared object). The TimescaleDB image is 1279.2 MB, 635.0 MB over it
(54 shared objects, 414.0 MB). Neither extension is offered on RDS (which offers `pg_tle` for
trusted-language extensions only), on Aurora at any version, or on Cloud SQL, which does not
allow installing your own. Azure Flexible Server offers TimescaleDB 2.24.0 in its Apache 2.0
edition only, which has no compression, behind `shared_preload_libraries`; it does not offer
pg_deltax. The block formats are plain SQL and run on all four.

**Q5, the F5 cagg's size and refresh.** The cagg costs 0.15 B a sample at 2 s, 1.37 B at 30 s,
2.51 B at 60 s and 139 B at 24 h (the tables). It is materialized only: a late row (1e6) into a
materialized, compressed hour left it at 61 samples, max 1, until `refresh_continuous_aggregate`
over that hour brought it to 62 samples, max 1e6. The refresh policy's job (`run_job`) picked up a
second late row (63 samples, max 2e6) because its window reached back past it; a policy whose
`start_offset` stops short of a late row leaves it out until a manual refresh.

Four pg_deltax findings contradict its documentation or look like defects; none is filed upstream:
`DROP TABLE` orphans every companion, a restored dump cannot read its own compressed
partitions, a hot standby cannot read them, and `DETACH` is refused while any partition is
compressed.

Sources for Q4:
[RDS for PostgreSQL extensions](https://docs.aws.amazon.com/AmazonRDS/latest/PostgreSQLReleaseNotes/postgresql-extensions.html),
[Aurora PostgreSQL extensions](https://docs.aws.amazon.com/AmazonRDS/latest/AuroraPostgreSQLReleaseNotes/AuroraPostgreSQL.Extensions.html),
[Cloud SQL extensions](https://cloud.google.com/sql/docs/postgres/extensions),
[Azure Flexible Server extensions](https://learn.microsoft.com/en-us/azure/postgresql/extensions/concepts-extensions-versions)
and its [considerations](https://learn.microsoft.com/en-us/azure/postgresql/extensions/concepts-extensions-considerations),
[pg_deltax](https://github.com/xataio/deltax).
