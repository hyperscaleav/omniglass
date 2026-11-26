---
title: "A system's Activity follows the page's time range, opening on 30 days"
date: 2026-11-26
status: Accepted
summary: "A system's Activity tab reads its Uptime, health history bar, stretch list, alarm rows and markers over the page's one time range, reversing the design in which Activity kept its own fixed windows. With no range given Activity opens on the last 30 days and Overview and the Timeline on 24 hours; a range once picked governs every tab, so the system page writes every pick to the address, 24 hours included. The bar opens at the range's start with the carried-in health, and Uptime's label names the range."
refs: [2741, 2304, 2425, 2722, 2735]
---

- **Date:** 2026-11-26 | **Status:** Accepted | **Pages:** [explore your fleet](/operator/fleet/), [the visualization inventory](/contributing/visualization-inventory/)
- **Decision:** (#2741, the default ruled by the architect.) The system page's time range (#2304)
  governs Activity too: the health read carries the range's `since` (and `until` for a custom range),
  Uptime is measured over it and labelled by it ("of the last hour", a custom range by its dates,
  still "since ..." for a system younger than the range), the history bar opens at the range's start in
  the health carried in (`carried_in`), and the alarm rows and markers read the same window (a custom
  range also reads its alarms with `since` and `until`, since the default alarm read covers only the
  last 30 days of cleared ones). With no range in the address, Activity opens on the last 30 days
  (`HISTORY_DEFAULT_RANGE`, a status page's window) and Overview and the Timeline keep 24 hours; the
  picker shows the window each tab reads. The range primitive gains `given` (the range the address or a
  blade's own pick carries, undefined when none) and an `explicit` write, so on a page whose tabs open
  on different defaults a pick of 24 hours lands in the address and holds on Activity. A custom range a
  caller without `view:read` cannot read falls back to each tab's default, as before. A blade keeps its
  own range.
- **Context:** Activity kept fixed windows when the range landed (#2304), because its reads could not
  honor one; the health read has since taken `since` and `until` and returned `carried_in` (#2425,
  #2735). Over a fixed 30 days a ten-minute outage is a fraction of a pixel and Uptime cannot be read
  for the hour an operator is looking at; asked for while recording the marketing teaser's
  before-and-after health beats (#2722). Events and logs keep their 24-hour lists for now.
