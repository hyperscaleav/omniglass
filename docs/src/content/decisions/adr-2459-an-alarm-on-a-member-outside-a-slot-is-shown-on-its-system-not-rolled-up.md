---
title: "An alarm on a member outside a slot is shown on its system, not rolled up"
date: 2026-11-23
status: Accepted
summary: "A system's page and drawer show every open alarm on every member, slotted or not, and name the members outside every slot with the way to place them; the system health read carries the members and a reason line naming the slots behind the verdict. Health still rolls up through slots alone: an alarm on a member outside a slot moves nothing above its component, because the slot and its impact are the system-level health rule."
refs: [2459, 1002]
---

- **Date:** 2026-11-23 | **Status:** Accepted | **Pages:** [health](/architecture/health/#reading-health), [Explore](/operator/fleet/), [alarms](/operator/alarms/)
- **Decision:** (#2459, ruled by the architect in the console review)
  1. **Shown.** `GET /systems/{name}/health` lists every member of the system with its own health,
     its open alarms, the slot it fills (`slot`) or `outside_slot` when it fills none, and a `reason`
     line naming the slots behind the verdict. The console's system page and drawer list every
     member's open alarm (labelled "impairs X", "in X" or "not in a slot"), say the reason beside the
     health, and name the members outside every slot with the action to place one in a slot.
  2. **Not rolled up.** A system's health is still decided by its slots alone: an alarm on a member
     outside every slot moves nothing above the component, whatever its severity, exactly as the
     health page already stated. The slot and its impact are the system-level health rule.
  3. **Verdicts come only from declared rules.** A system's health is decided by rules somebody
     declared (today, its slots and each slot's impact), never implied from an alarm existing. An
     operator may well want "any open alarm on a member degrades this system" for some system, and
     that is a rule they would declare on it; the platform does not work that way implicitly. Its
     home is system-level rules over system-owned samples and system-owned alarms (#1002), where the
     use case is recorded. Nothing of it is built here.
- **Context:** A component of a hand-built system (no standard, so no slots) could be in outage while
  its system's page listed no alarm and read healthy, with nothing saying why (#2459). The defect was
  that the alarm was invisible, not that the verdict ignored it.
- **Not chosen:**
  - **Degrade the system.** Ruled first and built in review: any open alarm on a member outside a
    slot made its system degraded, and an occupant still to attach (a driver product with no
    endpoint) read incomplete. Withdrawn by the architect the same day: a component may alarm without
    its system being degraded, such as a touch panel whose firmware is out of date, and deciding what
    an alarm costs the system is what the slot's impact is for. The platform system (outposts and
    servers, in no slot) is the same case.
