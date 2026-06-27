---
title: UI
description: "The operator console: one renderer library in two composition modes, reads through views, and an identity-based information architecture."
sidebar:
  badge:
    text: Design
    variant: caution
---

The UI is where an operator actually does the work, so it is built as one renderer over the same views the rest of the platform reads, with an information architecture organized around the entities you care about. This page covers the renderer / page / dashboard model and the information architecture. The stack, the typed client, the build pipeline, and
the concrete reusable primitives are the [design system](/contributing/design-system/).

## The renderer contract: ViewResult and the views BFF

The whole console rests on one contract. **All UI reads go through [views](/architecture/views/)**
(the read-side BFF), CRUD for writes; the operator never queries raw tables. Every view returns a
uniform **`ViewResult`** (`{columns, rows}`), and the SPA renders any view through **one renderer per
view**: adding a view does not add a bespoke renderer. This is what decouples the render layer from any
specific query and keeps the read contract uniform whether a page is coded or a dashboard widget is
configured.

The **dense-ops layout is an architectural pattern**, not a one-off page: list surfaces follow one
shape (a summary of donut facets over the full set, then a keyboard chip filter, then a group-by table,
then a click-row detail drawer plus a full detail page), and the facets drive the filter while the
summary stays whole so click-to-filter is stable. The concrete extracted primitives that realize the
pattern (`DensePage`, `FilterBar`, `Donut`, `SummaryFacet`, `Drawer`, `HealthBadge`, `Actor`,
`Sparkline`) live in the [design system](/contributing/design-system/); the pattern is the model.

## One renderer library, two composition modes

The factoring avoids both "every screen is hand-coded" and "everything must be a dashboard":

- **Renderer library** (coded once): `stat`, `table`, `status-grid`, `timeline`, `heatmap`,
  `line` / `area`. Each takes a **view result plus a field-mapping** (which column is the value /
  label / time / series key), so a renderer is decoupled from any specific view, and any view of the
  right shape can feed it. The set is closed but grown reactively, the same discipline as the
  reducer vocabulary.

  :::caution[Open question]
  The field-mapping contract between a view result and each renderer (the column roles per renderer
  type).
  :::
- **Coded pages** compose renderers plus custom interaction: the built-in information architecture
  (overview, drill-downs, config forms, exploration).
- **Composable dashboards** (config-driven): operator-built grids where each
  **widget = a view ref + a renderer + a field-mapping + params**, no code per dashboard.
  Dashboard-level params flow into widget view-params, so one "system overview" dashboard works for
  any system.

  :::caution[Open question]
  The composable-dashboard schema: the widget placement grid, the view binding, and the dashboard
  params.
  :::

  :::caution[Open question]
  Whether dashboards are themselves resources (carrying the `official` boolean, saved like views) or
  a thin layer over saved views.
  :::

The contract underneath both: **all UI reads go through [views](/architecture/views/)**, CRUD
for writes. The renderer library serves coded pages and dashboard widgets identically; the only
difference is whether the composition is code or config.

## Coded pages and dashboards share one view layer

Coded pages give the complete operator console; composable dashboards are the customization layer on
top (a grid editor, widget config, and the view-binding UI), and the view layer is what makes them
cheap. A built-in page **queries a default view, not a raw resource** (the Alarms page reads the
`firing-now` view, not `GET /alarms` directly), so the read contract is uniform and the same view
backs a dashboard widget unchanged.

## Live updates: polling by default

Live data is **query polling** (a refetch interval; slow-changing config uses a long stale time). A
read can also **stream over the view layer (a server-side SSE relay)** where latency or fan-out
earns it, the same earn-it-with-a-profile discipline. Presentation that depends on config (a severity
level's id to its label and color) resolves client-side from the config view. A datapoint
value resolves the same way: on read the UI converts canonical to the operator's preferred
display unit, looked up from the unit registry by the [datapoints](/architecture/datapoints/)
datapoint_type's canonical unit, so storage stays single-unit while one operator sees
Celsius and another Fahrenheit.

:::caution[Open question]
Which high-frequency surfaces move from polling to the SSE relay, and what latency earns it.
:::

## Configuration UIs

CRUD forms over the typed resource API, one per primitive (components, templates, rules, config,
tags, groups, schedules, severity levels, and the IAM resources). Editing a setting is editing
**[config](/architecture/variables/)**, an audited mutation, not a separate prop store
([audit](/architecture/audit/)). The standout is the **rule-authoring
page**:

- an **Expr editor** for the predicate or condition, with the prepared-input contract surfaced
  ([expressions](/architecture/expressions/));
- a **live blast-radius preview** (which entities a scope selects, which datapoints a rule would
  have fired on), so a rule is validated against reality before it is saved;
- the **AI-suggestion seam** ([AI](/architecture/ai/)): AI may propose a rule pre-filled with
  provenance; the operator edits and approves, and approval is the ordinary audited create. AI never
  saves a rule itself.

## Exploration UIs

Coded pages with rich interaction, all reading through views:

- **The cascade resolve view** (the standout): "why did this value win", rendered from the
  [cascade](/architecture/cascade/) resolve output: the effective value, the winning source, and the
  ordered shadowed bindings it beat. The feature that makes an opinionated cascade explainable.
- **Datapoint history**: a `line` or `heatmap` over a chosen time range, with the stale / unknown
  distinction surfaced ([time](/architecture/time/)).
- **Alarm drill-down**: the alarm, its triggering datapoint and history, the actions it fired, and
  ack / snooze / resolve controls.
- **Inventory and topology**: the location / system / component trees, navigable, with
  [health](/architecture/health/) (`status-grid`) at each level.
- **Event exploration**: query the event log by entity / time / category, with the audit trail.

## Information architecture

The IA has two layers, deliberately decoupled:

1. **Routes are flat and identity-based.** Every entity page is a top-level path (`/systems`,
   `/components`, `/templates`, `/config`); a page's URL addresses the *entity*, never its place in
   the menu. This is the contract we refuse to churn: bookmarks, deep links, and cross-links stay
   stable however the menu is later reorganized. There are no taxonomy-nested routes and no redirects
   to maintain.
2. **The sidebar groups those flat routes into clusters for browsing**: Home, Dashboards, Alarms,
   Inventory (systems, components, locations, interfaces, nodes, tasks), Catalog (templates, types,
   tags, rules), Explore, Settings (config, secrets, identity, audit). Grouping is pure
   presentation: a cluster is not a destination and carries no route of its own. It can be
   rearranged, and is user-customizable, without touching a single route.

**Home is distinct from Dashboards.** Dashboards monitor the *fleet* (datapoint views over the
inventory). Home monitors the *monitor*: the operator and admin situation room for config lifecycle
(stale or out-of-date templates), control-plane health (rules failing to evaluate, datapoints
dropped with no matching rule), and proactive suggestions. A dashboard cannot model that, so Home
earns its own slot; "Overview" is the name of the default dashboard, not the landing.

The theme is **dark-first** (the NOC aesthetic) on the brand palette (teal `#21CAB9`, navy
`#080c16`), semantic tokens only, no hardcoded colors in components.

