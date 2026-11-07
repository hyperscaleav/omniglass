---
title: "Scale lab plans seed as configuration rows (#1555)"
date: 2026-11-07
refs: [1555, 1357, 1553, 1554, 1562, 1069]
---

The second half of the scale lab's plan: `devseed.SeedPlan` takes a resolved `scenario.Plan` and lands
it as rows, the sites, buildings, floors and rooms, the systems in the rooms, the components filling
each system's roles, and the simulated endpoints on the components. It is the seeding function the
bring-up in #1554 calls. A plan system now carries `Roles`, the product and count filling each role,
because seeding has to staff the roles and the plan had dropped that mapping.

The seeder goes through the Gateway's create methods, one row per transaction, the path every other
seed takes. The tests seed a 20-system plan and check the systems, their rooms, each role's staffing,
the components and the endpoints against what the plan said, and that two seeds of one plan mint the
same names and addresses.

The issue asked for a bulk SQL path. Measuring first changed that. At 1,000 systems the row-by-row seed
takes 2 minutes 13 seconds and 317,374 round trips, already inside the 3-minute target, and the
dominant cost is not round trips: `locationVerdict`'s recursive subtree walk in the health recompute is
about 67 of the 131 seconds spent in statements, and it grows faster than the plan (0.8 ms a call at 100
systems, 4.1 ms at 1,000). A bulk path would have to skip that recompute or reimplement the Gateway's
name minting, labels, audit and slot checks beside it. We keep one implementation and fix the recompute
in the product (#1562), then batch transactions over the existing `*Tx` functions in a later PR.

`TestRunScenarioLargeWithinBudget` passes at 3 minutes 17 seconds against its 5-minute budget, so the
large dev-seed scenario needs no bulk path (#1069).
