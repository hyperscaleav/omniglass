---
title: "Collection slice (checkpoint 5d-ui): the interface and task authoring surfaces"
date: 2026-07-19
refs: []
form: bullet
---

- **Collection slice (checkpoint 5d-ui): the interface and task authoring surfaces.** The console gains the two
  authoring pages the 5d-api CRUD feeds, plus the component-scoped **Add reachability check** affordance. The
  `/interfaces` and `/tasks` stubs become live **Interfaces** and **Tasks** inventories (configs over the shared
  `FlatList`, the flat sibling of the tree lists): Interfaces shows a row per endpoint (name, type, owning
  component, node placement, probed target derived from `params`) with name / type / component facets; Tasks shows
  a row per unit of work (display name or content-addressed id, interface, mode, an enabled pill, node) with
  interface / mode / enabled facets. A row opens a detail Drawer (facts plus an inline edit of the mutable fields
  and a delete), and **New interface** / **New task** open the create Drawer. Because there is no `interface_type`
  list endpoint, the type picker offers the built transports (`icmp`, `tcp`, `ssh`, `http`) this slice ships; a
  future `GET /interface-types` registry route can replace the static options. The **Add reachability check** button
  on the Reachability panel (gated on `interface:create` *and* `task:create`) authors a check the way the node runs
  one: it creates the interface (`type` = the chosen transport, **named by its protocol**, owned by that component,
  `params.target` the `host[:port]` the probe dials) then a **poll** task over it (`mode=poll`, enabled), then invalidates the
  reachability, interfaces, and tasks queries so the panel and pages refresh. The two writes are handled honestly:
  if the task create fails after the interface exists, the affordance surfaces the partial state (the interface is
  created; the operator retries just the task) instead of swallowing it. Every nav tab and action gates on the
  real permission via the same `can()` the sidebar and route guard read (`interface:read` / `task:read` tabs and
  URL guard; create / update / delete actions), and the pages render only real `InterfaceBody` / `TaskBody`
  fields. No API or engine change (5d-api already ships the routes and typed client), so `make gen` shows no
  drift. Proven by vitest: the two data layers (envelope unwrap, create / patch bodies, filter keys, the target
  derivation), both pages (rows, the create gate hidden without the perm, the type / mode / interface pickers),
  and the Add-check affordance (the gate needs both perms, submit fires the interface then the task create with
  the right bodies, and the two-step error path surfaces). This closes the collection slice's authoring surfaces.
  (Superseded before the PR: checkpoint 5f removes the Add-check affordance, and 5g folds the standalone
  Interfaces and Tasks pages into the component and node details and **derives** the task, so these standalone
  surfaces and the `task:create` gate do not ship; the flat pages became detail panels.)
