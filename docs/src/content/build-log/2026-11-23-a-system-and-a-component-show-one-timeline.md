---
title: "A system and a component show one timeline of health, alarms, data and commands (#2425)"
date: 2026-11-23
refs: [2425, 1826, 1832, 2304, 2305, 2424]
---

A component's view and a system's view gain a **Timeline** tab: health over the page's range, every
alarm raised in it as a span from raise to clear that says how long it was raised, the properties
and metrics the alarming rules watch (or the ones the operator picks), and events, log lines and
commands as markers, all on one time axis. The cursor is the page's shared one (#2424), so it
crosses every lane and the metrics grid under the timeline; hovering a mark says what it is and
clicking opens it (an alarm's history, the rows behind a marker, a command's arc). Dense markers
cluster into a count. It is the first cut of #1832's timeline widget, on plain range reads rather
than the live binding.

The derivation is pure (`web/src/lib/timeline_lanes.ts`: verdict spans, alarm spans clipped to the
range with their whole duration kept, value bands, clustered markers, a metric's line), and the
component takes `{ kind, id, range }` with no page logic. Every read honors the range on the
server: the component and system alarm lists gain `since`/`until` (an alarm is kept when it was
raised by `until` and still raised at `since`), the health reads answer `carried_in` (the verdict in
force when `since` opened the range, so a quiet range is one span of the health that held, not an
empty lane), events, logs and property changes read through `POST /views:run` with `from`/`to`, and
commands through their `since`/`until`. A nested cursor provider now shares the outer cursor.
