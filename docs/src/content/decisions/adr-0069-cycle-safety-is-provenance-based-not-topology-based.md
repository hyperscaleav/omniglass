---
title: "Cycle safety is provenance-based, not topology-based"
date: 2026-07-30
status: "Accepted"
summary: "Cycle safety is provenance-based: consequence writes carry `provenance='calculated'` with a `source_rule` naming the producer, and rules never route on their own consequences; supersedes the \"alarms are terminal upstream and never write samples\" premise"
---

- **Date:** 2026-07-30 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/), [health](/architecture/health/)
- **Decision:** the guarantee that automation cannot feed back into itself rests on **provenance**, not
  on a topological "alarms are terminal upstream and never write samples" rule. A consequence write
  carries `provenance='calculated'` with a `source_rule` naming its producer (today: the health
  rollup's `state` sample, `source_rule='health-rollup'`, written on alarm raise and clear), and rules
  must never route on their own consequences: the routing layer refuses to re-trigger a rule off a
  sample whose `source_rule` is that rule.
- **Context:** the alarms page argued cycle safety from the premise that alarms never write samples.
  The build falsified it: raising or clearing an alarm recomputes health in the same transaction
  (`internal/storage/alarms.go`), and the rollup records the verdict transition as a
  calculated-provenance `state` row (`internal/storage/health.go`). That write is correct, it is the
  recorded-transition model of
  [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain);
  the premise was too strong. The real invariant is lineage-based and survives future consequence
  writers (calculations, action side effects) that a topology rule would forbid.
- **Supersedes** the terminal-upstream cycle-safety argument on
  [alarms and actions](/architecture/alarms-actions/); the page now derives safety from provenance.
