---
title: "A node's worklist pull resolves every secret in one statement (#1581)"
date: 2026-11-08
refs: [1581, 1577, 1571]
---

After pins and rejections were batched, the headquarters node's worklist pull in the 1,000-system
corporate plan (806 components, 4,434 tasks) still issued 1,106 statements. Nearly all of them were
the secret resolve: per (secret reference, component) pair, a read of every secret by that name, a
cascade check per candidate, and a re-read of the secret types.

`NodeWorklist` now resolves every pair in one statement (`nodeSecrets`). The component and location
chains are walked once for the whole batch, each chain row tagged with the component it started from,
so a candidate secret covers only its own reference's component. The secret types are read once per
pull, and a credential several components share unseals once. The single-secret readers (webhook
delivery, command masking, the sim) are one-element calls of the same batch, so the delivery rules
live in one place.

Measured with `TestPlanWorklistMeasure` (`PLAN_MEASURE=1000`) on one machine, before and after:

| | statements | wall time (idle) |
| --- | --- | --- |
| headquarters, before | 1,106 | 954 ms |
| headquarters, after | 8 | 311 ms |
| all 18 sites, before | 4,228 | 3.07 s in statements |
| all 18 sites, after | 144 | 0.72 s in statements |

Every site's pull is now 8 statements, whatever its size. The under-load figure (1.24 s, from the lab
bring-up) was not re-measured here. `TestNodeWorklistStatementsDoNotGrowWithEndpoints` now carries
a secret, a pin and a rejection per component and compares 10 components with 100: it failed before
the change (36 statements against 306) and passes after it. It also asserts that each endpoint
receives its own values and nothing from another endpoint. `TestNodeWorklistResolvesEachComponentsOwnSecret`
pins every cascade shape against one shared name: a component's own secret, one inherited down the
component tree, from a building, from a site, a platform secret, a missing reference, a chain that owns
none (a sibling's secret never reaches it) and a chain that owns two (ambiguous, delivered as absence).
