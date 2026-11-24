---
title: "A system's alarm rows offer Snooze like a component's (#2448)"
date: 2026-11-24
refs: [2448, 2115, 1318]
---

`AlarmActions` draws Snooze only for an alarm that carries `snoozed_until`, so the system page's Why
strip, whose alarm rows come from the health read, could not offer it without also offering it on an
already snoozed alarm. `healthAlarmBody` now carries `snoozed_until` and `snoozed_by` (same names and
docs as the component alarm list; `make gen`), `AlarmRow` threads them, and the strip in `SystemDetail`
(the page and the blade render the same body, and acknowledge was already on) turns on Snooze, shows
the snoozed chip, and offers Unsnooze once snoozed. Tests: `TestHealthAPI` (the wire fields, red before
the struct change), `system_zoom.test.ts`, `SystemDetail.test.tsx`.
