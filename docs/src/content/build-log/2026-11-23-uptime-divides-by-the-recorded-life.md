---
title: "A new system's uptime divides by the time it has existed (#2435)"
date: 2026-11-23
refs: [2435, 2472, 2493]
---

The Activity tab's uptime card now measures the healthy share of the entity's recorded life
inside the range (`max(range start, first_recorded_at)` to now), through the pure `uptimeOver`
in `web/src/lib/uptime.ts`, and its label names the window when it is shorter than the range.
With no health recorded it says "no health recorded yet" rather than a percentage. A health
stretch that held under half a second no longer lists as its own row (the 0 s "Commissioning"
entry). The history strip gains start and end labels and a per-segment hover (verdict, from, to,
duration), and the alarm-raise triangles now sit on the strip's own axis with a legend.
