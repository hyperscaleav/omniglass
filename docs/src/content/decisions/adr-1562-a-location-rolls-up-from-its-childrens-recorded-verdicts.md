---
title: "A location rolls up from its children's recorded verdicts"
date: 2026-11-07
status: Accepted
summary: "The health recompute records a location's verdict as the worst of its direct children's latest recorded verdicts (its placed systems and its child locations), not the worst over every system in its subtree. A recompute locks its whole location chain in id order first, then rolls it up bottom-up. The verdicts and the recorded transitions are unchanged; a write's recompute costs its chain's length, not its subtree's size."
refs: [1562, 1555, 670, 642]
---

- **Date:** 2026-11-07 | **Status:** Accepted | **Pages:** [health](/architecture/health/)
- **Decision:** (#1562; amends how [ADR-0092](#adr-0092-a-location-move-recomputes-both-ancestor-chains)'s
  context describes `locationVerdict`.)
  1. **Children, not the subtree.** `locationVerdict` reads one latest recorded verdict for each
     system placed in the location and each child location under it, each a `limit 1` read on its
     owner index (`property_system_owner_idx`, `property_location_owner_idx`), and takes the worst. A
     child with no recorded row has never been unhealthy and is skipped. Health's rollup is
     worst-wins and a child location's verdict is the worst of its own subtree, so this is the same
     verdict as the subtree fold it replaces.
  2. **Lock first, then bottom-up.** `recomputeChain` locks every location in its chain in id order
     (the global lock order, [#670](https://github.com/hyperscaleav/omniglass/issues/670)) before it
     computes any of them, then records them deepest first (`bottomUp`), so each location reads
     children this transaction has already refreshed. A child outside the chain was last written by a
     transaction that also held this location's lock, which has committed by the time this one holds
     it, and the recompute's statements read committed rows.
  3. **No schema change.** Both owner indexes and the `system.location_id` and `location.parent_id`
     indexes already existed.
- **Context:** [#1562](https://github.com/hyperscaleav/omniglass/issues/1562), found while measuring
  the scale lab's row-by-row seed (#1555). Every write recomputes each location above it, and each of
  those calls used to walk the location's whole subtree with a recursive CTE and a `distinct on` over
  every subtree system's whole health series. A campus's call covers the whole site, so the cost
  grew with the fleet: at 1,000 systems the rollup ran 4 times per `AssignRole` at 5.1 ms each,
  about two thirds of the time to staff the plan. Folding the subtree had been chosen so the recorded
  verdicts did not depend on the order a recompute visits its locations. The bottom-up order now
  provides that, and an equivalence test holds every recorded verdict to a full recompute after each
  kind of write.
- **Consequence:** a location's recorded verdict is correct only while its children's are, so a new
  trigger that changes a location's children has to recompute the chain above them, as every trigger
  already does. The health reads (`SystemHealth`, `LocationHealth`) still compute live from the
  subtree, so a missed trigger costs an edge in the history, never a wrong report.
