---
title: "A silent node and a nearly full data disk each open an alarm (#1707)"
date: 2026-11-11
refs: [1707, 1094, 1625, 1628]
---

Two official event rules now ship. `node-silent` reads the `checkin-age` the server writes for every
claimed node every 15 s: past 90 s, three missed heartbeats, it opens a `high` alarm on the node's
platform component with no sustain, and the next heartbeat clears it. It replaces the node-liveness
sweep the nodes page designed, which would have raised a `node-down` alarm of its own
([ADR-1707](/architecture/decisions/#adr-1707-a-silent-node-alarms-through-a-seeded-rule-over-its-checkin-age-not-a-node-liveness-sweep)).
`disk-nearly-full` reads a new server figure, `data-dir-disk-used-ratio`, the used share of the
filesystem holding `OMNIGLASS_DATA_DIR`, read by statfs on every report window: over 0.9 for 2
minutes it opens a `warning` on the server's component, and at 0.85 or under it clears. A failed
read writes no value for that window and logs one WARN until the read recovers; the other figures
still land. The alarm reaches Platform health in the server's open count; no panel draws the ratio
yet.
