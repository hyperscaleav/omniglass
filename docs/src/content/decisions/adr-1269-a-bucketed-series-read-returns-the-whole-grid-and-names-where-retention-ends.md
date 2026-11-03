---
title: "A bucketed series read returns the whole grid and names where retention ends"
date: 2026-11-02
status: "Accepted"
summary: "Ruled by the architect on 2026-11-02 for #1269. A series read given a resolution returns every regular, epoch-aligned bucket of the window, empty ones as count 0 with null values, and carries covered_from, the edge of the type's effective retention. The cadence-derived gap break and a failed check drawn as its outcome leave this slice for #1475, after #1264"
refs: [1269, 1265, 1264, 1276, 1475]
---

- **Date:** 2026-11-02 | **Status:** Accepted | **Pages:** [metrics and properties](/architecture/properties/#a-series-chart-is-truthful-at-every-zoom-1269)
- **Decision:** (ruled on #1269.)
  1. **The whole grid.** The read returns every bucket of the window, oldest first, aligned to the epoch so a boundary is stable between reads. An empty bucket is present with `count: 0` and null `min`, `max`, `avg` and `last`. 30 days at one hour is 720 buckets. A request over 5,000 buckets is refused with a 422.
  2. **`covered_from`.** The body carries the instant before which nothing is read: the later of the grid start and now minus the type's effective retention (its `retention_days`, else the install's `retention.metric_days` or `property_days`). Nothing is read beyond the hot window until trends ([#1276](https://github.com/hyperscaleav/omniglass/issues/1276)).
  3. **`last` per bucket is kept.** A property drawn as steps needs the value a bucket ends on, and a metric's end label and dot need the newest reading, which an average would misstate.
  4. **Moved out of this slice.** The gap break derived from a series' cadence (and its factor k) and a failed check drawn as its outcome move to [#1475](https://github.com/hyperscaleav/omniglass/issues/1475), which lands after task liveness (#1264) ties a series to its cadence. Until then the avg line runs across an empty bucket (no band or point is drawn there), as the raw line ran between samples. No schema change is made here.
- **Context:** the issue's draft asked for both of the moved items; the architect split them off, since the cadence they read from is #1264's.
- **Not chosen:**
  - **Skipping empty buckets.** A gap would then read as a short line and a chart could not tell no data from a quiet value.
  - **Reading past retention for a longer range.** Rollups are #1276's.
