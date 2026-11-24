---
title: "Events and logs render through one occurrence row (#2437)"
date: 2026-11-24
refs: [2437, 1832]
---

A system's Activity tab and a component's now list events and logs through one `OccurrenceList`
(section head, one filter bar) and one `OccurrenceRow`, a one-line grid whose fields line up down the
list: relative time (the absolute on hover), severity or event key (info muted, warning and above
colored), the source on a system's list only, a log's facility, and the message truncated with the full
text on hover. The row is a button (`aria-expanded`); a click opens source, facility and attributes in place. `EventsPanel`, `LogsPanel`, `LogFilters` and the system page's monospace log table are gone;
the outpost's self-logs use the same list unfiltered. A 64-hex task id in a message renders as a short
labelled reference. The filters narrow the server read: the events reads gain `key`, `source` and `q`,
the logs reads gain `q`. Not built: the page range (#2304) on these lists, which still read a fixed 24
hours, and live append (#1830).
