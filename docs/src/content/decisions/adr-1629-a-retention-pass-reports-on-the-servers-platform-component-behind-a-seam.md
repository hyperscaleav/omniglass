---
title: "A retention pass reports on the server's platform component, behind a seam until the server has one"
date: 2026-11-09
status: Accepted
summary: "Accepted: each retention pass lands its figures as platform metrics written by the process: per lane a deleted count and a behind flag keyed on the lane's own name, a behind flag per compaction, the pass's duration, failures and partition roll, the mark lags, and a status read of the partitions and table sizes. A lane whose prune failed is also flagged behind. The seeded `retention-behind` rule opens a warning after 5 minutes. The component is a `bus.Config` field production leaves empty until #1627 gives the server one; #1671 wires it."
refs: [1629, 1094, 1625, 1627, 1671, 1417]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [storage](/architecture/storage/), [collection](/architecture/collection/)
- **Decision:**
  1. **The pass reports what it did, on one component.** `retentionSamples(component, report,
     status, now)` is pure and returns the gateway's `PlatformMetric` rows (not the
     `MetricSampleWrite` the ticket named): the rows carry their component, so the pure map is the
     whole write and `RecordPlatformMetrics` lands it as the process. One pass is one sample per key
     and instance.
  2. **Lanes are keyed on their names.** The deleted count and behind flag take the lane's name as
     the instance (`metric`, `metric/<type>`, `log_line/info`), and the compactions report a behind
     flag of their own (`metric_trend compaction`, `metric_block compaction`) beside the metric
     window, only when the pass ran them.
  3. **A failure is also behind.** A lane whose prune errored did not reach its window, so the pass
     names it behind as it already did, and counts the error in `retention-pass-failure-count`. The
     failure count says a fault happened; the behind flag held says the lane is not keeping up,
     whatever the cause.
  4. **The rule's sustain is 5 minutes.** A behind pass retries a minute later, so 5 minutes is
     several catch-up passes; the alarm is a warning (a lane that falls behind costs disk, not
     data) and clears on the first pass that reads the lane 0.
  5. **The seam is thin.** The server's platform component is #1627's, not yet built, so
     `bus.Config.PlatformComponentID` carries it and production leaves it empty, writing nothing.
     The integration test stands a node's platform component in for the server's. Passing the
     server's id at boot is #1671.
- **Context:** retention reached operators only as INFO log lines, and a pass falling behind showed
  first as a full disk (#1629, under #1094).
- **Rejected:** building the server's component in this slice (it is #1627's, in flight); reading
  the log tables' oldest row (a full scan with no `ts`-leading index, until #1417); a behind flag
  that excludes failed lanes (an operator would then need two rules to see a lane not keeping up).
