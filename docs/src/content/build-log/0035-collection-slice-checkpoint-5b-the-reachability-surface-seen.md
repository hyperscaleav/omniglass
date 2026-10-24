---
title: "Collection slice (checkpoint 5b): the reachability surface, seen"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoint 5b): the reachability surface, seen.** The first datapoint read surface. A
  per-component read BFF `GET /components/{name}/reachability` (permission `component:read`, scope-injected, so a
  viewer gets a 404 on a component outside its scope) composes, per interface, the latest verdict (`LatestState`
  on `interface.reachable`), the layer signals (`LatestMetric` on `tcp.open` / `icmp.reachable` and their rtt),
  and the verdict's transition history (`StateTransitions` over a window). The operator console gains a
  **Reachability panel** on the component detail: one row per interface with a verdict pill (`responding` /
  `down` / `stale` / `unknown`, staleness derived at read time), an **availability strip** drawn from the state
  transitions (up/down over time, not a latency trend), and an expandable L3/L4 gate breakdown (ping and port,
  the inline probes this slice ships; the L7 app-layer gate lands with the snmp/http/ssh interface types) with
  the reason a down interface is down. It renders only real API fields. The availability-percentage SLI
  (`time_in_state` over the verdict) still rides in with the health slice; the panel shows the transitions
  directly. Interface and task authoring (the "add check" affordance and the flat Nodes / Interfaces / Tasks
  pages) are checkpoints 5c and 5d.
