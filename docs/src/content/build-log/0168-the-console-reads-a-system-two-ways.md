---
title: "The console reads a system two ways"
date: 2026-08-08
refs: [626]
form: bullet
---

- **The console reads a system two ways** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  the epic's last task). A predating bug shipped first as its own commit:
  `RoleEditor`'s `buildSpec` sent only quorum, display name, and the typed-slot sets, so the
  PUT's wholesale replace silently reset a role's `impact` to `degraded` on any unrelated edit
  (the server defaults an omitted impact), invisible in the console with no warning; capacity
  and `position_labels` had the same gap. Every **system** now carries a **colour of its own**, a
  hue hashed from its uuid (`system_color.ts`, FNV-1a over the WHOLE string, since a uuidv7's
  leading 48 bits are a shared millisecond timestamp within one devseed run and hashing only a
  prefix would land every seeded system at nearly the same hue), stepping past the five bands
  the theme's semantic tokens already claim, rendered through a `.og-system-dot` swatch reusing
  `.tag-pill`'s per-theme lightness and chroma. **The by-role lens** (`RolesPanel`) reads the
  health body alongside the roles read for occupancy-aware short/spare arithmetic in place of
  the health-blind understaffed/assigned figures, marks a down occupant in place, and fixes the
  assign picker offering a component already staffing a different role in the same system,
  which the server refuses with a 409 (a component fills at most one role per system) nobody
  could have anticipated; it now renders disabled, naming the role it already holds. **The
  by-role occupants drag into a new
  order**, wired through the same draggable/onDragStart/onDragOver/onDrop shape `ColumnMenu`
  already uses (no dnd dependency added), decomposed into the server's only reorder primitive
  (a pairwise position swap) by a new pure `swapPath`. **The by-device lens** (`MembersPanel`)
  names the role, if any, each member fills, pivoted client-side from the same roles fetch
  (`roleByComponent`; no reverse route exists). A standing bug closed alongside: `HealthPanel`
  rendered a role impaired without consulting its `active` flag, so a role belonging to a
  choice's losing alternate (#626) showed as an ordinary impairment beside a healthy status
  banner, flatly contradicting it on the seeded huddle-room shape; both derivations now read
  through a new `activeRoles`, with the excluded roles named under their own heading rather than
  dropped. Records [ADR-0087](/architecture/decisions/#adr-0087-capability-gated-staffing-retires-an-alarm-impairs-its-component-not-a-named-capability),
  which supersedes [ADR-0049](/architecture/decisions/#adr-0049-the-system-role-capability-gated-staffing-and-the-resolved-capability-set)
  and amends [ADR-0050](/architecture/decisions/#adr-0050-health-is-a-recorded-transition-computed-from-the-alarm-capability-role-chain),
  and backfills five build-log entries the epic had carried without one.
