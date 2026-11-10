---
title: "Designed: a site's components spread across its nodes (#1638)"
date: 2026-11-09
refs: [1638, 1624, 1686, 1687, 1688, 1689, 1690]
---

The nodes page gains a "Load distribution across a site's nodes" design section, and ADR-1638
records the decision. Nothing in it is built yet.
- **Unit and pool.** The component is the unit (all its node-run endpoints on one node; a
  server-hosted push endpoint stays outside it). The pool is the
  nodes at the component's location or its nearest ancestor, later a cascaded property.
- **Pins and balancing.** An endpoint's `node_name` stays the operator's pin. A server reconcile
  balances unpinned components on expected cost (runs a second, a listen task at a flagged
  weight), and it is sticky.
- **Guards.** Each node's measured schedule loss ratio and pool busy fraction guard the balance.
  Dependency-edge neighbours are kept together when they can be, so the gate stays node-local.
- **Loss, join and moves.** A lost node's components move after the silent window plus a grace. A
  joining node fills gradually. A live move is two-phase and completes only on the old node's confirmation, so
  no device sees two sessions from live nodes.

Scaling's horizontal-scale fence now names distribution within a site. The build is five slices
under #1624: the busy-fraction signal (#1686), the one-node-per-component invariant (#1687),
assignment (#1688), rebalance on loss and join (#1689), and the two-node lab baseline (#1690). The
lab baseline the issue asked for could not be measured on main. The scale lab runs one node a site,
`lab measure` has no per-node figure, and the node publishes no lateness or busy fraction yet. #1690
is that thin cut.
