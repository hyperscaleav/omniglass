---
title: "Collection slice (checkpoint 5f): reachability lands read-only; check authoring deferred"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoint 5f): reachability lands read-only; check authoring deferred.** Before the PR, the
  component-scoped **Add check** affordance was **removed**: it was a stopgap that a proper **driver**-based
  authoring flow (attach a driver to a component, fill its inputs) replaces, and the driver/collect layer is
  under active design (template-centric vs driver-centric), decided deliberately rather than on momentum
  ([ADR-0020](/architecture/decisions/) status note). So the reachability panel is now **read-only display**; a
  reachability check is authored by adding an **interface** to its component (checkpoint 5g settles the interface
  as the only authored primitive, its poll task **derived**, and folds the interface and task surfaces into the
  component and node details). The slice lands as **remote node reachability**: enroll a
  node, it probes, the operator sees per-interface reachability. The collect layer (drivers, the normalized menu,
  SNMP) is its own slice after the driver design.
  out of `TreeList` into a standalone `BladeStack` primitive over a `createBladeController`, and both the
  inventory tree and the flat identity pages (`FlatList`) now consume it. A stack entry is a cross-entity
  `{ kind, id }` ref against a registry, so a user's group opens the group's blade over it and a group's member
  opens that user's blade, both stacking. Depth is bounded by construction: each page roots one kind and drills
  one direction (Users: user to group; Groups: group to user; Roles: role), so the drill graph is acyclic and
  the reverse relation on a terminal blade is read-only. A shared `DetailShell` (`Fact`, `RelatedList`,
  `DetailActions`) standardizes the chrome, so a group's members and a user's groups render through one list
  idiom and every footer places the destructive action left of the primary. Roles moves from the card catalog
  onto the same `FlatList` + read-only blade. Behaviour-preserving for the inventory pages (the Locations blade
  is identical); proven by the web suite (the `BladeStack`, `DetailShell`, and both-way identity drill tests)
  and tsc.
