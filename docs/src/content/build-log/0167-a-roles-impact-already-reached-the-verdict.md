---
title: "A role's impact already reached the verdict"
date: 2026-08-08
refs: []
form: bullet
---

- **A role's impact already reached the verdict**
  ([#626](https://github.com/hyperscaleav/omniglass/issues/626)). The epic's claim that
  `system_role.impact` was declared but never consumed, so a short role contributed outage
  regardless, was false against this branch, and this slice ships no code to fix it.
  `Role.Contributes()` (`internal/health/verdict.go`) has exactly two branches, healthy when
  satisfied and `ImpactVerdict(r.Impact)` when impaired, and `SystemVerdict` folds it worst-wins
  into both the recorded outcome and the served one; the chain landed in `5a050e5` (#323), well
  before this epic was written. Task 5's rebuild changed only what makes a component *occupy*
  a slot, across two commits: `feat: capability-gated staffing retires` (`ca78bd3`) first read
  `Occupies()` as `Verdict == Healthy`, a stricter threshold that let even an info alarm remove
  a component from every role it filled, then its own review-round fix, `fix: a degraded
  occupant still satisfies its role` (`dbfa284`), loosened it to the intended `Verdict !=
  Outage`. Neither commit touched `ImpactVerdict`, `Contributes`, or the fold. The one real
  gap in this area was vocabulary, not behavior: `understaffed` (the roles read,
  `Quorum - len(AssignedTo)`) is health-blind assignment arithmetic, while `short` and
  `satisfying` (the health read) are occupancy-aware, so a role whose sole assignee carries a
  critical alarm can report `understaffed: 0` and `short: 1, impaired: true` at the same
  instant, both correct under their own definition. Both doc strings (`internal/api/roles.go`,
  `internal/api/health.go`) and the glossary now say so explicitly, and
  `TestShortAndUnderstaffedDivergeOnCriticalAlarm` (`internal/storage/health_test.go`) pins that
  a critical alarm makes the two figures diverge while a warning does not, since `Occupies()` is
  `Verdict != Outage`. No `fix:` commit: nothing in the chain needed to change.
