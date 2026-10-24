---
title: "The Catalog rail is sectioned by the estate noun each registry serves"
date: 2026-08-06
status: "Superseded by [ADR-0084](#adr-0084-the-catalog-shell-and-five-signal-lanes)"
summary: "the Catalog rail is sectioned by the estate noun each registry serves, entries keep the registry's own word (Types where that is all there is), Telemetry holds what gets recorded and Action what the platform does, and the /catalog hub teaches the map with live counts"
---

- **Date:** 2026-08-06 | **Status:** Superseded by [ADR-0084](#adr-0084-the-catalog-shell-and-five-signal-lanes) | **Pages:** [UI](/architecture/ui/)
- **Decision:** the Catalog nav cluster renders non-folding section headers under one naming rule:
  a **section is named for the estate noun it serves** and an **entry keeps the registry's own
  word**, collapsing to plain **Types** where the registry has no other word (Locations > Types,
  Secrets > Types). The sections, mirroring Inventory's order: Components (products, vendors,
  drivers, capabilities), Systems (standards), Locations, Secrets, Telemetry (metrics, properties,
  events, and the future log catalog as a soon entry), Action (rules, commands, and the future
  notifications), General (tags). The organizing line: **Telemetry is what gets recorded, Action
  is what the platform does**; Commands left Telemetry on the observed-versus-issued split, and
  Events stays in Telemetry because the shipped lane records happenings (caught from the estate,
  caused by the platform) and never sends them. Headers render from the permission-filtered entry
  list, so a fully gated section disappears with its entries; the palette tags sectioned entries
  `Catalog · <section>`; a visible Overview entry opens the `/catalog` hub, one card per visible
  section with live registry counts. Templates leaves the rail until the registry is real. Routes,
  tables, resources, and API surfaces are untouched: the rule governs presentation only.
- **Context:** fourteen flat entries hid five real clusters: products, vendors, drivers, and
  capabilities serve components with nothing saying so, and standards served systems invisibly.
  Rejected along the way: a flat rail with hub-only teaching (daily wayfinding regresses to the
  soup), hover flyouts (hiding is the disease being treated, hostile to touch and keyboard, and a
  "Types" flyout bucket recreates the one-word-hides-many shape ADR-0082 retired), and a uniform
  Types suffix (reverses the lane-noun labels the five-lane epic settled). The nav word for the
  rule registry is Rules; the vocabulary beneath it (table, API, resource words for rules and the
  alarm rows they raise) is unsettled and tracked in #606, out of this decision's scope.
