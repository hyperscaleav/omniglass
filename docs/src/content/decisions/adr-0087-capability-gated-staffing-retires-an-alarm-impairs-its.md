---
title: "Capability-gated staffing retires; an alarm impairs its component, not a named capability"
date: 2026-08-07
status: "Accepted"
summary: "The alarm-capability-role chain retires: an alarm impairs its component's own verdict wholesale, an occupant satisfies its role whenever its own verdict is not outage, and the typed-slot guard is the only assignment-time check; records the 409-vs-422 refusal line and the choice/alternate boot-seed reconciliation carve-out. Supersedes ADR-0049, amends ADR-0050"
---

- **Date:** 2026-08-07 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [health](/architecture/health/), [API](/architecture/api/)
- **Decision:** The **alarm -> alarm_capability -> degraded-capabilities -> role** chain
  [ADR-0049](#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set) and
  [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain)
  built retires. The replacement is three calls shorter: an **alarm impairs its component's own
  verdict wholesale**, no longer routed through a named capability; a role's occupant **satisfies**
  it whenever the occupant's own verdict is not `outage` (`Occupies()` is `Verdict != Outage`, so a
  merely degraded occupant still occupies its slot, since severity is how loudly to page somebody,
  not a second staffing threshold); and the **typed-slot guard** (`system_role_type`,
  `system_role_product`: a filling component's product must be classified within an accepted
  `component_type`, and, if pinned, be one of the named products) is now the **only** assignment-time
  check and plays no further part in health. `impact`, `quorum`, the worst-wins fold, and health as a
  recorded transition recomputed at the write are all unchanged; only the routing key changed, from a
  capability set to a component's own verdict.

  Two refusal rulings that fell out of the same rebuild, generalized here because nothing else names
  them: a role-write refusal is **409** when it depends on rows other than the one being written
  (double-staffing a component across roles, a capacity below the currently assigned count: "the
  declaration or assignment request is not invalid on its own, it conflicts with the estate's current
  state"), and **422** when the declaration alone is invalid regardless of other rows (capacity below
  quorum, an unresolvable typed-slot reference). And a boot-seed carve-out: `choice_alternate`
  (not `role_choice`, which keeps the ordinary insert-if-absent rule and no set-level reconcile;
  a choice row itself is never deleted by a reseed) reconciles to its declared YAML set on
  **every** boot, deleting a stored alternate that dropped out of the set (refusing instead, with
  `ChoiceInUseShortfall`, if a role still points at it) rather than leaving it in place. This is a
  **deliberate departure** from the
  platform's usual boot-seed rule (insert-if-absent, `ON CONFLICT DO UPDATE`, an operator's row never
  touched by a reseed): it is safe here only because `choice_alternate` has **no operator write
  path** (nothing but the seed ever writes one) and its **`position` is a packed 1..n sequence**
  within a choice, where a leftover orphan does not sit inert, it **collides** with the position a
  renamed or reordered entry now wants. It is a narrow exception for a position-ordered seeded child
  of a table nothing else writes, not a precedent: a seeded entity with an operator write path, or
  without a packed ordering an orphan can collide on, keeps the ordinary insert-if-absent rule.
- **Context:** Task 5 of the identity-model epic ([#626](https://github.com/hyperscaleav/omniglass/issues/626))
  shipped the retirement (`ca78bd3`, `dbfa284`) without filing this entry at the time; it is recorded
  now, against the gap, by Task 9 of the same epic. The forcing function was the typed-slot guard the
  prior slice had just landed (`c006c62`): once a component fills a role because its **product**
  classifies within an accepted **`component_type`**, gating the same assignment on a **second**,
  independent capability set was two guards asking the same question in different vocabularies, and
  the capability registry (`capability`, `alarm_capability`, `component_capability`,
  `product_capability`, `system_role_capability`, five tables) added a maintenance surface, an
  editing UI, and a resolved-set computation (`EffectiveCapabilities`) that nothing left standing
  needed. Health's own reasoning for routing through capability
  ([ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain),
  "capability is the only vocabulary shared by the thing that breaks and the thing that cares") no
  longer holds once assignment routes through type instead: the component **itself**, not a named
  fact about it, is what a role now cares whether is up. The 409/422 line and the boot-seed carve-out
  are recorded here rather than left as implicit code comments because both are the kind of local call
  a later slice reads out of context and either contradicts by accident or copies somewhere it does
  not fit; the boot-seed carve-out in particular must not be read as license to delete a shipped row
  for any other seeded entity, which is why its two preconditions are stated explicitly.
- **Supersedes:** [ADR-0049](#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)
  in full: `role_capability`, `component_capability`, and `EffectiveCapabilities` are gone, not
  merely superseded in wording, and the typed-slot guard is what a role now requires. **Amends**
  [ADR-0050](#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain):
  its chain item 1 (capability as the routing key) retires; items 2 through 5 (the pure judgement
  package, the transition-only record, recompute-at-the-write, and a report computing what it
  serves) are undisturbed and this entry changes nothing about them.
- **Tracked under** epic [#626](https://github.com/hyperscaleav/omniglass/issues/626).
