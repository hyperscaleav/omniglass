---
title: "Log lines are pruned by severity (#417)"
date: 2026-10-28
refs: [417, 1265, 1145]
---

The retention pass now keeps the two log tables as well, a component's log lines and a node's own,
by severity: debug 3 days, info and notice 14, warning 30, and error and worse 90, four settings an
admin changes beside the sample windows. A line with no severity is kept as long as info.

Every prune is searched rather than walked. A new `(severity, ts)` index on each table, and a
`(ts)` index for lines with no severity, find exactly the lines past a band's window, ordered as the
index is so the planner never falls back to reading the table. So the BRIN limit the sample prune
lives with does not reach the logs.

The review found the cost that mattered most outside the log tables. Each deleted line clears the
link from any event that named it as its source, and with no index on that link, every deleted
line scanned the whole event table: 27.7 seconds for 2,000 lines over 200,000 events. That link now
has its own index, at 8.8 milliseconds.

A band only works if every line speaks the same vocabulary. A one-time backfill mapped the older
words still stored (`warn`, `ERROR`, a numeric `5`) onto RFC 5424's eight levels, and moved any
other word to the `severity_raw` attribute. A test holds the backfill's alias list to the one every
writer uses. A node's self-logs are now normalized at the gateway, as a component's have been since
#1145.
