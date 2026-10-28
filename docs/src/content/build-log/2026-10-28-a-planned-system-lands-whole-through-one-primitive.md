---
title: "A planned system lands whole through one primitive (#1325)"
date: 2026-10-28
refs: [1325, 827]
---

The simulator's add-a-system path generalizes into the planned-system primitive the template layer
builds on. The pure planner moves to `internal/systemplan` and takes each role's candidate products as
an input: the simulator passes the products it serves (`simfleet.PlanSystem` is now that call), and a
system template will pass the product it names per role. The transactional half is
`storage.CreatePlan`: standards to fork when absent, a subtree of places parents first, systems with
the components that fill their roles (each with its endpoints and declared values), and standalone
components, all in one transaction, each piece the ordinary create in the caller's scope with its own
audit row. A plan's template, when it has one, is written into the detail of every audit row it
writes, the only trace of where a row came from. `CreateSimSystem` is now a consumer, and the
simulation route's tests pass unchanged; the location, declared-value and standard-fork writes gained
the in-transaction forms the plan shares with their own routes.
