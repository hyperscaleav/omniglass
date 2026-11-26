---
title: "A system's Activity tab follows the page's time range (#2741)"
date: 2026-11-26
refs: [2741, 2304, 2735]
---

The time range picker now sits on a system's Activity tab too, and Uptime, the health history bar, the
stretch list, the alarm rows and their markers all read over it ([ADR-2741](/architecture/decisions/#adr-2741-a-systems-activity-follows-the-pages-time-range-opening-on-30-days)).
The health read carries the range's `since` (and `until` for a custom range, whose alarms are read over it
as well), keyed per range under the system's health key so every existing invalidation still reaches it.
With no range given Activity opens on the last 30 days and Overview and the Timeline on 24 hours, so
`lib/time_range.ts` gained `parseGivenRange`, a `given` accessor on `useTimeRange` and `useHostTimeRange`,
an `explicit` write that keeps a pick of 24 hours in the address, `rangePhrase` for a range inside a
sentence and `rangeQuery` for a read's `since` and `until`. `uptimeOver` takes the range and labels it
("of the last hour"), measuring a custom range to its own end; `withCarriedIn` (in `lib/timeline.ts`) opens
a window's edges at its start, which the bar and the stretch list share, so the bar starts at the range's
start in the carried-in health. The lab's timed system page load reads the health 30 days back, as the page
does.
