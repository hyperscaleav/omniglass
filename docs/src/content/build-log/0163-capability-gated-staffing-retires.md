---
title: "Capability-gated staffing retires"
date: 2026-08-08
refs: [626]
form: bullet
---

- **Capability-gated staffing retires** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `ca78bd3`, `dbfa284`). The `alarm -> alarm_capability -> degradedCapabilities -> role` chain is
  gone, replaced end to end: an active alarm impairs its component's own verdict wholesale, and a
  role counts an occupant as satisfying it only from that verdict, not from a per-named
  capability. Five tables drop (`capability`, `alarm_capability`, `component_capability`,
  `product_capability`, `system_role_capability`) along with everything wired to them: the
  `/capabilities` API and its console pages, `EffectiveCapabilities`, the component-capability
  routes, and the seeded capability registry. The typed-slot guard from the prior slice becomes
  the only assignment check; it plays no further part in health. A same-slice review round found
  `Occupies()` had landed as `Verdict == Healthy`, a stricter threshold than intended that let
  even an info alarm remove a component from every role it filled; `dbfa284` loosened it to the
  spec, `Verdict != Outage` (a merely degraded occupant still occupies, since severity is how
  loudly to page somebody, not a second staffing threshold), and raised every test and doc
  example that had used a warning-severity alarm as its "take this occupant down" trigger to
  critical, the true down case. Recorded as [ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability),
  which supersedes [ADR-0049](/architecture/decisions/#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)
  and amends [ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain),
  filed against this slice rather than at the time, a gap this entry closes.
