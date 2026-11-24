---
title: "A severity level declares its health effect"
date: 2026-11-24
status: Accepted
summary: "An alarm's effect on its component's own health is declared on its severity level (none, degraded or outage), not implied by rank against a hardcoded floor. The shipped info level declares none, so an informational alarm is listed without badging its component. The API's outage flag stays as a deprecated alias."
refs: [2558, 917, 2459, 1002]
---

- **Date:** 2026-11-24 | **Status:** Accepted | **Pages:** [health](/architecture/health/), [alarms and actions](/architecture/alarms-actions/#severity-a-catalog-of-named-levels)
- **Decision:** (#2558, from the architect's ruling on #2459: a verdict comes only from a rule someone declared, never from an alarm merely existing)
  1. **Declared, not derived.** `severity_level.health_effect` is one of `none`, `degraded`, `outage`.
     `health.ComponentValue` folds the declared effects of a component's open alarms worst-wins; the
     `OutageRank = 40` constant is gone. A component whose only open alarms declare `none` reads
     `healthy` and still lists them. Slots are unchanged: a component occupies its slot unless its own
     health is outage.
  2. **Seed.** `info` none; `warning` and `average` degraded; `high` and `disaster` outage. This moves
     exactly one behavior: an open `info` alarm no longer degrades its component. The boot seed is
     authoritative for the official rows, and when it changes a level's effect it recomputes every
     component with an open alarm at that level in the same transaction, so a fleet upgraded with an
     open `info` alarm reads healthy after the first boot with no stuck degraded row.
  3. **Custom levels.** A custom level declares its effect on create; omitted, it takes what its rank
     had under the old floors (below `warning` none, below `high` degraded, else outage). The
     migration's backfill of existing custom rows keeps today's behavior instead (outage at rank 40
     and above, degraded below), so an upgrade changes nothing for an operator's own levels.
  4. **Compatibility.** The `/severity-levels` `outage` flag stays as a derived, deprecated alias
     (`health_effect == outage`); `health_effect` is the field to read. A re-rank no longer moves
     health; a change of effect does.
- **Context:** An informational alarm (a touch panel's firmware out of date) badged its component
  degraded because every open alarm did. System-level rules that turn member alarms into a system
  verdict remain #1002.
