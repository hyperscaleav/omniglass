---
title: "A disruptive command closes its endpoints' gates"
date: 2026-11-09
status: Accepted
summary: "Accepted: the nodes page's post-command wait (a lane that blocks until the host reads up, behind a fixed timeout) becomes a disrupted reason on the endpoint gate, added at delivery to every endpoint of the component on the delivering node. Nothing runs during the type's quiet_seconds; after quiet each endpoint reopens on its own first up verdict begun after quiet; a counted hold_seconds backstop, kept apart from settle_window_seconds, reopens the rest. Reboot ships disruptive, quiet 15 s, hold 300 s."
refs: [1651, 1650, 1624, 1648, 1652, 1655]
---

- **Date:** 2026-11-09 | **Status:** Accepted | **Pages:** [commands](/architecture/commands/#the-command_type-registry), [nodes](/architecture/nodes/#implicit-reachability), [collection](/architecture/collection/#collection-telemetry-every-endpoints-account-of-itself)
- **Decision:**
  1. **Disruption is the command type's fact.** `command_type` gains `disruptive`, `quiet_seconds`
     and `hold_seconds`. A non-disruptive type carries neither duration and a disruptive type's
     hold must exceed its quiet (CHECKs, and a 422 at the API). The seed ships `reboot` disruptive
     with quiet 15 and hold 300; no other type is marked.
  2. **The delivery carries it.** `PendingNodeCommands` sets `Disruptive` and the **hold set**,
     every endpoint of the command's component placed on the delivering node, so the node needs no
     second lookup and an endpoint on another node is not held (#1655).
  3. **A gate reason, not a blocked lane.** The nodes page designed a post-command wait: the lane
     blocks until reachability reports the host back up, with a fixed timeout. The node instead
     adds `disrupted` to each hold-set endpoint's gate (ADR-1650's set) **before it sends**, dated at
     the delivery and naming the command. The lane is free: other endpoints' work runs, and a held
     endpoint's command, run now or preview answers at once naming the command and since.
  4. **Quiet, then each endpoint's own up.** For `quiet_seconds` nothing runs against a held
     endpoint: no poll (held, counted as `poll-held-count`, never a failure), no probe, no session
     dial, so a rebooting device is neither read as down nor hammered with reconnects. After quiet,
     an endpoint reopens on its own first up verdict, and only a probe that **began** after quiet
     counts, so an answer in flight at the delivery (the stale-up race) cannot release it. A down
     verdict releases nothing, and siblings release independently.
  5. **A counted backstop apart from the settle window.** An endpoint with no up verdict by
     `hold_seconds` reopens anyway and the window counts it as `hold-expiry-count`, a device that
     did not come back in time. The hold is not `settle_window_seconds`: settlement watches a value
     the command set, and a fire-and-forget reboot has none, while the hold bounds how long its
     endpoints stay closed.
  6. **A failed send still holds; a restart does not adopt.** Whether the device received a
     command whose send failed is unknown, so its gates stay closed until up or the backstop. A
     restarted node adopts only a refusal from its worklist: the restart outlasts the moment and
     the probe reads the device fresh.
- **Divergences and follow-ups:** the dependency-aware hold set (power, host and path edges,
  out-of-band endpoints) is #1648; holding dependents through `upstream` is #1652; hold-set
  endpoints and previews on another node are #1655. A per-binding quiet override is not built:
  boot time is the probe's to read.
- **Rejected:** blocking the lane until the host is up (it stalls every other task queued there and
  needs a timeout that is either too short for a slow boot or too long for a fast one); releasing on
  the first up seen after delivery (a reply already in flight reads up before the device goes down);
  reusing `settle_window_seconds` as the hold (one number would answer two questions, and a
  fire-and-forget command would need a settle window it never settles against); a second gate map
  for disruptions (ADR-1650 rejected two sources of truth for one question).
