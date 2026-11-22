---
title: "The console says health and reachability, not verdict (#2103)"
date: 2026-11-20
refs: [2103, 2086]
---

Following the API rename (ADR-2086), the console no longer says verdict. The outline filter key
`verdict` is `health` (a saved `verdict` chip no longer matches), the reachability panel's row label
reads "reachability", and the health history strip, the system and platform hints and the webhook
row's tip say health. In `web/src/lib` the `Verdict` type is `HealthState` and the `verdict*` helpers
are `health*` (`healthOf`, `healthRank`, `worstHealth`, `healthHue`, `HEALTH_HUE`); the reachability
helpers are `reachWord` and `ReachState`. `HealthBadge`, `HealthHistory` and `HealthGlyph` take a
`health` prop. The kept senses (soak, subscription test, target test, expression check) still say
verdict. A new Explore test covers the `health:` filter.
