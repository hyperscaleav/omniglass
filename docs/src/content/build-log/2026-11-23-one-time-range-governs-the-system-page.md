---
title: "One time range, held in the address, governs the system page's charts (#2304)"
date: 2026-11-23
refs: [2304, 1826, 1836, 2306]
---

A page's time range is now one primitive, `web/src/lib/time_range.ts`: a pure parse, serialize and
resolve over the URL (`?range=1h|6h|24h|7d|30d`, or `?from=&to=` in RFC 3339 for an absolute
range; absent is 24 hours, and anything unparseable falls back to it without throwing), plus a
`useTimeRange()` hook over the router's search params. `TimeRangePicker` is its Kobalte control,
and `TabRail` gained an `end` slot that carries it outside the tablist.

The system page is the first consumer: the Data section's sparklines and charts read the picked
range instead of a hard-coded day. A preset reads the series route's `hours` at a step that keeps
each chart a few hundred buckets wide (a day stays at five minutes, so the default load is the
read it always was); an absolute range reads through `/views:run` with `from`, `to` and
`step_seconds`, since the series route only counts back from now. Activity's reads (health history,
events, logs) still have fixed windows, so the control is offered on Overview only until they can
honor a range.
