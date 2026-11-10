---
title: "A path upstream holds what is reached through it (#1652)"
date: 2026-11-10
refs: [1652, 1650, 1648, 1624]
---

A component reached through another now inherits its upstream's closed gate
([ADR-1652](/architecture/decisions/#adr-1652-a-path-upstream-holds-its-dependents-on-the-node)).
The worklist carries each in-band task's path upstreams (the upstream component and its in-band
endpoints on the same node), and the node derives an `upstream` gate reason with one pure,
cycle-guarded function each time any reason set moves. While the upstream is closed the dependent's
polls and probes are held and counted as `poll-held-count` and a command answers at once naming the
upstream; once it opens, the dependent waits for its own probe before it is released. The reason
names the root of a chain, is published in the rejection row with the upstream's id, and the
reachability read adds the component's name. The dev seed reaches `touch-a` through `bar-a`. Proven
over the real wire (`TestImpairingAnUpstreamHoldsItsPathDependent`): two simulated devices on one
node joined by a path edge; unplugging the upstream names it on the dependent's gate within a probe
pass, the dependent's held count climbs while its probe result and verdict stay up, and replugging
releases it once its own probe passes.
