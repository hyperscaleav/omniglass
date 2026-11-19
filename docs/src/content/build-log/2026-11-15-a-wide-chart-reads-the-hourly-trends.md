---
title: "A wide chart reads the hourly trends (#1566)"
date: 2026-11-15
refs: [1566, 1269, 1276, 1531]
---

A bucketed metric read sorted every sample of its window, so a month's hourly chart of a series at
one sample every 30 seconds read 86,400 samples for 720 points. A metric read over more than 48
hours, or past its series' hot window, is now raised to whole hours (`ClampBucketResolution`), and the
response's `resolution_seconds` names the width applied; 30 days at five minutes answers 720 hourly
buckets where it was a 422. At whole hours an observed series takes each hour the trend fold has
closed from `metric_trend`, and its open hours, any hour from the first dirty one on, and the partial
hours at either edge from the series read, each hour once. A calculated series, an instance with a
calculated series beside its observed one, and a finer resolution read the samples alone. The
decision is ADR-1566; the trend setting that would turn a series' trends off is #1531.

The fleet measurement (`BUCKET_MEASURE=1 go test ./internal/storage/ -run TestFleetBucketReadProbe -v`):
50 series at 30 s for 30 days, 4.3 million samples, each read hourly over the 30 days, warm.

| Read | Median a series | All 50 |
| --- | --: | --: |
| From the trends | 3.2 ms | 185 ms |
| Series read, raw rows | 80 ms | 4.1 s |
| Series read, folded blocks | 93 ms | 4.8 s |
| From the trends, days folded | 3.4 ms | 185 ms |

Both ways answered the same buckets. Under `BUCKET_EXPLAIN=1` the trend read executes in 2 to 4 ms
against 133 ms for the block read.

`TestTrendServedBucketsEqualTheSeriesRead` holds the trend-served grid to the samples-only grid at 1,
6 and 24 hours, a one-sample spike and a duplicated instant among the rows, and shows open hours ignore
a poisoned trend row and closed hours survive their raw rows being deleted.
`TestALateSampleReadsBackBeforeAndAfterItsHourIsRecomputed` reads a late spike through its dirty hour
and again from the recomputed trend. `TestBucketsWithoutTrendsReadTheSamples` and
`TestAnInstanceWithACalculatedSeriesBesideReadsTheSamples` cover the fallbacks, and
`TestAWideBucketReadIsClampedToWholeHours` and the API's clamp cases cover the rule, with the
standards end-to-end test reading 720 hourly buckets for 30 days asked at five minutes.
