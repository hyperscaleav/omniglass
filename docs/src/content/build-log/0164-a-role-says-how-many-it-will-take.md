---
title: "A role says how many it will take"
date: 2026-08-08
refs: [626]
form: bullet
---

- **A role says how many it will take** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `4925a7d`). Roles gain `capacity` (an optional upper bound above quorum) and `position_labels`
  (per-slot names); both are wholesale-declaration columns, but only `capacity` preserves on an
  edit that omits it: the storage upsert reads `coalesce(excluded.capacity, system_role.capacity)`,
  so a nil `*int` (the API caller left the field out) keeps whatever cap is already declared, a
  server-side guarantee rather than a rule every caller has to remember to uphold. `position_labels`
  carries no such coalesce, replacing wholesale like `quorum`, `accepted_types`, and `impact` (its
  own doc string: omit or empty clears labelling), the same defect `impact` already carried before
  this task's console fix (see below). A component may fill at most one
  role per system: the migration raises and names the offending pairs before adding the
  enforcing index rather than aborting mid-upgrade on an unnamed constraint violation, and
  `AssignRole` pre-checks inside its transaction so the refusal names both the component and the
  role it already holds (409: it depends on what else is currently assigned). Lowering a
  standard-owned role's capacity below what any one conforming system has assigned is refused the
  same way, since each conforming system stages independently. `mapRoleWriteErr` now
  discriminates by constraint name instead of collapsing every `23505` into "a role with this
  name is already declared here": a double-staffing race, a capacity-below-quorum CHECK, and a
  genuine name collision now report distinctly, and the refusal status follows whether the
  problem depends on other rows (409) or the declaration alone (422), recorded in
  [ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability).
