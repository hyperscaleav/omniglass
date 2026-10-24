---
title: "Explore is the fleet's door, and it is an outline of places"
date: 2026-08-31
status: "Accepted"
summary: "Explore (`/explore`) is the fleet's one door, and it draws one thing: an outline of places. A place wears the system that sits there, its components beneath; a chain of single-child places folds into one row; a collapsed row says what it holds in the registry's own labels and counts its systems by verdict, never worst-wins. Depth and what may be added come from `allowed_parent_types`, never a type's name. The filter is a predicate over live state, never a scope. One EntityForm per kind; three tabs and one counts line per workspace; the band canvas, KPI chips, jump anchors, the renderer library and the inventory tables retire (#826, #861, superseding ADR-0132's anchors and ADR-0129's canvas landing)"
---

- **Date:** 2026-08-31, rewritten 2026-10-03 | **Status:** Accepted | **Pages:** [ui](/architecture/ui/), [views](/architecture/views/)
- **Decision:** The sidebar has one door into the fleet, **Explore** (`/explore`), and it
  draws one thing: an **outline of places**, the location tree as an ARIA tree with five
  rules.

  **A place wears its system.** Operators think of a room as its system; the location is
  where it sits in the tree. So a place where one system sits carries that system's standard
  and verdict on its own row, and its components are directly beneath. A place holding
  several systems heads a row per system. A component two systems share appears under both,
  marked with the other; a component in no system sits under its place. Systems and
  components with no place the caller can read gather under one node, **Placed nowhere you
  can see**, so nothing disappears because its place is out of scope.

  **A chain of one folds.** A place whose only content is one child place folds into it,
  rendered as the path (`Headquarters / West Building`), and its add menu offers each place
  it joins. Depth is never counted: it is the customer's fact through `location_type` and
  `allowed_parent_types`, and what may be created under a row is read from the same graph,
  never from a type's name.

  **A collapsed row says what it holds, and counts, never rolls up.** It states its contents
  in the registry's own type labels (`2 buildings, 1 floor`, `19 components`, `Empty`), and
  counts the systems beneath it by verdict in four fixed slots, healthy grey and only trouble
  in colour. Worst-wins is right for one system's verdict and wrong for a place standing in
  for forty: at any realistic failure rate almost every large place holds one outage, so a
  rolled-up colour saturates and says nothing.

  **The filter is a predicate, never a scope.** Filtering on live state (verdict, standard,
  type, product, a name or path fragment, a tag) is exploring, and turns the tree into a flat
  list of matches that reveal into the tree. Naming subjects to include is choosing a scope,
  which a dashboard widget owns; so there is no location facet and no saved view. The filter
  rides the address (`?chips=`), as does one revealed row (`?node=`); what is open is the
  browser's.

  **One door, one shape behind it.** One `EntityForm` per kind renders read or edit wherever
  the operator meets the entity (side panel, Configure tab; create is the same form empty at
  `/<kind>s/create`, with `?under=` prefilling placement); every workspace carries three tabs
  and one counts line with zeros left out. The band canvas (`/fleet`), the KPI chip rows, the
  blade's jump anchors and the per-kind inventory tables retire, and their addresses land on
  Explore. "Fleet" stays the noun (ADR-0122); the sidebar entry is the verb.
- **Context:** The #779 canvas conflated exploring with monitoring (a KPI wall as
  navigation), and the #800 anchors patched a blade that had diverged from the page instead
  of restoring the vision that view and edit are one component. #826 replaced both twice
  before anything shipped. A Miller-column drill fell to a generated fleet of a thousand
  systems across twelve roots of four tree shapes: it spent a screen on four levels and
  assumed a uniform depth. A renderer library followed (cards cut at each root's shallowest
  repeated container type, bands, a mosaic and a standards matrix, with label and area
  budgets and saved presets), and its rules were each earned by a render: the first mosaic
  came out uniformly red, which is where the count-not-rollup rule above comes from. Judged
  in use it was hard to navigate, confusing, and opinionated: four drawings of one fleet, a
  cut the operator had to reverse-engineer, and controls that came and went with the
  renderer. The architect asked for one simple way to see the stack at a glance and drill in
  as needed (#861). The outline keeps the earned rules (depth is the customer's, counts over
  rollups, predicate over scope) and drops everything that asked the operator to choose how
  to look. Nothing had shipped, so this rewrites the decision in place rather than
  superseding it. This supersedes ADR-0132's blade-as-quick-face-with-anchors (its Configure
  tab survives as the form's page host) and ADR-0129's canvas landing (its identity-route
  default stands: the workspaces are the routes' faces). The map renderer and component
  location override are #836; whether a system should be modelled as a facet of its
  location is #862.
