---
title: "A wide bucketed read is whole hours and takes closed hours from the trends"
date: 2026-11-15
status: Accepted
summary: "A metric bucketed read over more than 48 hours, or past its series' hot window, is raised to whole hours, at least one, and reports the width applied. At whole hours an observed series takes each hour the trend fold has closed from metric_trend and every other hour from the series read, each hour once; calculated series, mixed instances and finer resolutions read the samples. The buckets equal the series read's, avg up to summation order. Amends ADR-1269's coverage floor."
refs: [1566, 1269, 1276, 1530, 1534, 1531]
---

- **Date:** 2026-11-15 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/),
  [metrics and properties](/architecture/properties/), [API](/architecture/api/)
- **Decision:** (#1566, amending the coverage floor of
  [ADR-1269](#adr-1269-a-bucketed-series-read-returns-the-whole-grid-and-names-where-retention-ends).)
  1. **The clamp.** A metric bucketed read whose window is longer than 48 hours
     (`BucketClampSpan`), or reaches past its series' hot window (the type's effective retention),
     has its resolution raised to whole hours: at least one, and a longer one rounded up to the next
     whole hour (`ClampBucketResolution`, a pure function). The response's `resolution_seconds` is
     the width applied, not the one asked. The API applies the span rule before it counts buckets,
     so 30 days at a second is 720 hourly buckets, not a 422; the storage read applies both rules,
     since only it knows the series' window. A property read is not clamped: a property has no
     trend.
  2. **Closed hours come from the trends.** At whole hours an observed series (`trendedSeries`)
     takes each hour from its `metric_trend` row from the later of the window's start and the trend
     window's edge, up to the earliest of the compacted mark and the first dirty hour inside the hot
     window. Every other hour comes from the series read (`metricReadSQL`, ADR-1530): the hours
     after that point, the open hours, and a partial hour at either edge. Each hour is read from one
     source, so none is counted twice.
  3. **Some series read the samples.** A calculated series, an instance with a calculated series
     beside its observed one, and any resolution finer than an hour or not a whole number of hours
     read the series read alone. A sparse-auto series and a series with trends turned off have no
     setting yet (#1531); `trendedSeries` is the one predicate that setting changes.
  4. **Coverage reaches back to the trends.** `covered_from` of a whole-hour read of an instance with
     a trended series is the later of the grid's start and the earlier of the samples' retention
     edge and the trend window's edge, so a chart past the hot window draws the trend's hours rather
     than empty buckets.
  5. **Exactness.** count, min, max and last equal the series read's exactly; avg equals it up to
     the order its sum was added in (the tests allow a relative 1e-9). A bucket carries no first, so
     the trend's first is not read.
- **Context:** the bucketed read unnested and sorted every sample of the window, so a 30-day hourly
  chart of a series at one sample every 30 seconds read 86,400 samples to draw 720 points, about
  80 ms over raw rows and 93 ms over folded blocks, four to five seconds for a 50-series dashboard.
  The hourly trends (ADR-1276) already hold count, min, max, sum, first and last for every closed
  hour, written once by the compaction and recomputed when a late sample dirties the hour. Measured
  on the same 50 series, the trend-served read takes about 3.2 ms a series, 185 ms for all fifty,
  and returns the same buckets.
- **Consequences:**
  - A metric chart of more than two days is hourly at best. The console's charts (a day at five
    minutes, an hour at a minute) are under the clamp and unchanged.
  - A late sample's hour reads from the samples from the first dirty hour on until the recompute
    clears it, then from its recomputed trend. A dirty hour whose day passes the hot window's floor
    before the recompute reaches it has no samples left to read, and the recompute skips it, so it
    is served from its trend as it stood before the late sample. That hour was already the oldest a
    chart shows and is not otherwise readable.
  - An instance where an observed and a calculated series share an hour reads it from the samples,
    since placing a trend row's last against another series' samples needs the samples. That costs
    the speed for that instance only.
  - A trend kept longer than the samples now shows on a chart, and `covered_from` moves back with it.
- **Not chosen:**
  - **Clamp every read to the hour:** a day's chart at five minutes reads 2,880 samples a series in
    a few milliseconds; it does not need the trends and would lose its shape.
  - **Refuse a fine resolution over a wide window:** callers would have to know the threshold; the
    clamp answers and says what it applied.
  - **Serve a dirty hour from its stale trend everywhere:** fast, but the read would disagree with
    the series read until the recompute runs, which the exactness rule does not allow inside the hot
    window.
  - **Store a last per series in the trend to merge mixed instances:** a trend row is one series by
    design; the fallback keeps it so, and a mixed instance is rare.
