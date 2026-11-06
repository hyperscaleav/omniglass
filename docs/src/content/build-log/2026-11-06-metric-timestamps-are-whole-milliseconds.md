---
title: "Metric timestamps are whole milliseconds (#1526)"
date: 2026-11-06
refs: [1526, 1265]
---

A metric sample's `ts` now has no digits below the millisecond. The node truncates it where a batch
leaves the spool, and the ingest truncates it again before the row lands (truncating, not refusing,
so a spool replayed from an older node still lands). It holds for observed and calculated rows in
`metric` and for `metric_declared`; the property, event and log lanes keep their timestamps. A
migration truncated the rows already stored and set the columns' default to `now()` on its
millisecond.

The reason is that NTP-synced node clocks are only accurate to a few milliseconds, so the
microseconds were noise, and a raw row now agrees exactly with the compacted block (int4 millisecond
offsets) that a later slice of #1265 writes. Two samples of one series can share a millisecond; both
land, in arrival order. Event and calc rules judge the same samples the same way.
