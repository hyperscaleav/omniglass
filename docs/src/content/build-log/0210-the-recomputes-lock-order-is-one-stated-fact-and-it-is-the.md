---
title: "The recompute's lock order is one stated fact, and it is the id"
date: 2026-08-12
refs: []
form: bullet
---

- **The recompute's lock order is one stated fact, and it is the id.** `locationsOver` ordered its
  result by `name` while `recomputeChain`'s comment said the order was by id and `lockHealthOwner`'s
  said it was by name: three statements, two of them wrong, and harmless only while a location name
  was unique estate-wide. Scoping name uniqueness to placement retired that guarantee, so two rooms
  under different buildings can both be `415a` and the comparison ties. A tie is not an order: it
  hands the visit order to the plan, and the plan reads its input, so the two production trigger
  shapes really did disagree. A location move (both rooms named outright) resolved the pair
  newest-first and a system move (one room reached through the system placed in it, the other named
  as the one it left) resolved it oldest-first, which is exactly the precondition the per-owner
  advisory locks assume away. The query now orders by id, matching what the two comments claim and
  what `refSet.sorted()` already did for the component and system tiers. The guard is the ordering
  itself, asserted two ways and deterministic in both; the concurrency test the issue asked for is
  kept as a liveness check and labelled as one, because driven against the unfixed ordering it
  survived over four thousand paired rounds without deadlocking and odds are not a guard.
