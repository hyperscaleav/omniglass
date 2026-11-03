---
title: "A bucketed series read returns the whole grid and names where retention ends"
date: 2026-11-02
status: "Accepted"
summary: "Ruled by the architect on 2026-11-02 for #1269. A series read given a resolution returns every regular, epoch-aligned bucket of the window, empty ones as count 0 with null values, and carries covered_from, the edge of the type's effective retention. A bucket aggregates one instance's measured rows (observed and calculated), never another instance's and never a declared or intended row; an unnamed instance is the one with measured rows, or a 422 listing them (amended on review of #1485). The cadence-derived gap break and a failed check drawn as its outcome leave this slice for #1475, after #1264"
refs: [1269, 1265, 1264, 1276, 1475, 1485]
---

- **Date:** 2026-11-02 | **Status:** Accepted | **Pages:** [metrics and properties](/architecture/properties/#a-series-chart-is-truthful-at-every-zoom-1269)
- **Decision:** (ruled on #1269.)
  1. **The whole grid.** The read returns every bucket of the window, oldest first, aligned to the epoch so a boundary is stable between reads. An empty bucket is present with `count: 0` and null `min`, `max`, `avg` and `last`. 30 days at one hour is 720 buckets. A request over 5,000 buckets is refused with a 422.
  2. **`covered_from`.** The body carries the instant before which nothing is read: the later of the grid start and now minus the type's effective retention (its `retention_days`, else the install's `retention.metric_days` or `property_days`). Nothing is read beyond the hot window until trends ([#1276](https://github.com/hyperscaleav/omniglass/issues/1276)).
  3. **`last` per bucket is kept.** A property drawn as steps needs the value a bucket ends on, and a metric's end label and dot need the newest reading, which an average would misstate.
  4. **A bucket is one series: one instance, measured rows only.** (Amended on review of [#1485](https://github.com/hyperscaleav/omniglass/pull/1485).) A bucket aggregates the observed and calculated rows of one instance of the type on the owner, never a declared or intended row: a want is not a reading, and a commanded 9999 must not become a bucket's max or last. The read takes an optional `instance`. Unset, it is the one instance with measured rows in the window; when more than one has them the read is a 422 whose problem detail lists each at `query.instance`, so the caller picks rather than receiving a blend of two series. Rows of one instance from different sources or collector nodes are aggregated together: a node move or a task reassignment does not split what was measured. The bucketed statement puts the range's lower bound and the coverage floor in the sample table's own `where`, as bind parameters and `now()`, so the monthly partitions outside the range are pruned and each remaining one is searched on `(series_id, ts)`; `resolution` is bounded to 30 days by the schema.
  5. **Moved out of this slice.** The gap break derived from a series' cadence (and its factor k) and a failed check drawn as its outcome move to [#1475](https://github.com/hyperscaleav/omniglass/issues/1475), which lands after task liveness (#1264) ties a series to its cadence. Until then the avg line runs across an empty bucket (no band or point is drawn there), as the raw line ran between samples. No schema change is made here.
- **Context:** the issue's draft asked for both of the moved items; the architect split them off, since the cadence they read from is #1264's.
- **Not chosen:**
  - **Skipping empty buckets.** A gap would then read as a short line and a chart could not tell no data from a quiet value.
  - **Reading past retention for a longer range.** Rollups are #1276's.
  - **Aggregating every series of the type on the owner.** The first cut did, and two endpoints' readings (or a reading and a command's want) blended into one min, max, avg and last that described neither.
  - **Picking an instance when several are measured** (the newest, or the busiest). A silent pick charts a different series from one read to the next; a 422 that lists them makes the choice the caller's.
  - **Splitting a series by source or collector node.** One instance is one logical series across a node move; splitting would chart a reassignment as a gap.
