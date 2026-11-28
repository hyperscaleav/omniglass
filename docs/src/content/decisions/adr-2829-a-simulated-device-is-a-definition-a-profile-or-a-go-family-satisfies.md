---
title: "A simulated device is a definition that a YAML profile or a Go family satisfies"
date: 2026-11-27
status: Accepted
summary: "Every consumer of the simulator catalog reads one Definition interface; a Go family (one package per wire API, models as a typed table, handlers as Go funcs) satisfied it beside the YAML profiles while the simulator migrated, and is the only thing that does now (#2837). The list of families is generated."
refs: [2829, 2826]
---

- **Date:** 2026-11-27 | **Status:** Accepted | **Pages:** [simulation](/architecture/simulation/)
- **Decision:** `sim/spec` defines `Definition` (id, kind, identity, surfaces, initial state, products,
  behaviors, elapsed uptimes, push and webhook faces). A YAML profile satisfies it through an adapter
  (`spec.FromProfile`); a Go family satisfies it as a `spec.Spec` (identity, a typed `Model` table,
  interfaces and ports, the state tree, `HTTPRoute` and `Command` handlers as Go funcs, an `Animator`
  seam). `spec.Catalog` resolves a product: a family that claims it (its name or one of its models)
  wins, else the YAML profile that simulates it. A family may not share a name with a YAML profile (the
  profile is deleted when its family lands) and two families may not claim one product. The engine,
  the outpost, the fleet planner, the dev seed, the lab scenario's catalog, the footprint sweep and the
  simulation routes all read the catalog, never the profile store. `make gen` writes the list of
  families (`sim/device`) by scanning its directories.
- **Context:** #2826 migrates the simulator's 31 profiles to Go one family at a time, so both kinds must
  run side by side and every consumer must be indifferent to which it has. A family's routes and
  commands are listed on its surfaces (marked `Native`, no template behind them) so the console shows
  a family's API as it shows a profile's. The `Animator` is the seam the shared behavior primitives
  plug into; until they land a family may run YAML-style behaviors through the existing machinery.
  The Go handlers are not rendered from templates: a response is a typed Go value the engine encodes.
- **Amended (#2845):** a family's read-derived uptime is a typed `spec.Elapsed{Path, Since}` in
  `Spec.Elapsed`, not the YAML `profile.Computed` it first reused; `Definition.ElapsedList` replaces
  `ComputedList`, and a YAML profile's `computed:` block is presented through the same list. The value
  read, and so every wire transcript, is unchanged.
- **Amended (#2837):** the strangler ended. The last YAML profile was deleted with #2836, and #2837 deleted
  what only profiles used: the YAML store and its loader, `schema.json`, the surface fragments and the
  response and behavior templates, the `spec.FromProfile` adapter and `Kind` (a definition has no second
  kind to name), `Definition.BehaviorList` and `Spec.Behaviors`, the engine's template routes, commands and
  behaviors, and the `Native` workaround fields (a family's listing is built from its own typed
  declarations, `wire.Route.Sets` and `wire.Interface.OIDs`). `sim/profile`, a package of what a family
  still composes, is `sim/wire`. `spec.NewCatalog` takes the families alone, and a guard test fails the
  build if a YAML profile or a behavior template reappears. A product resolves to the family that claims
  it; there is no fallback to resolve to. The coverage accounting moved from `coverage.yaml` to Go
  (`sim/device/coverage.go`), and the simulation page's family and coverage tables are generated from the
  specs (`cmd/simgen`).
