---
title: UI
description: "The operator console: one renderer library in two composition modes, reads through views, and an identity-based information architecture."
sidebar:
  badge:
    text: Partial
    variant: note
---

The console is one renderer over the same views the rest of the platform reads. This page is the renderer / page / dashboard model and the information architecture; the stack and the concrete primitives are the [design system](/contributing/design-system/).

:::note[What shipped vs the model below]
Roughly 22 live pages (inventory, catalog, values, admin, plus the shell) ship as **config-driven
`ListShell` pages (with `FlatList` / `TreeList` bodies) over the typed CRUD client**, not as the
`ViewResult` renderer described next: an inventory page is CRUD over a scoped resource. The views
model, the renderer library, and composable dashboards remain the intended **read side** for the
analytical surfaces (alarms, sample history, the cascade view, fleet dashboards), not built yet.
Realized shell: the [design system](/contributing/design-system/); operating it: the
[operator guide](/guides/operator/); per-slice breakdown:
[implementation status](/architecture/status/).
:::

## The renderer contract: ViewResult and the views BFF

:::design[Target design: the ViewResult contract, tracked in #523]
**All UI reads go through [views](/architecture/views/)** (the read-side BFF), CRUD for writes; the
operator never queries raw tables. Every view returns a uniform **`ViewResult`** (`{columns, rows}`),
rendered through **one renderer per view**: adding a view never adds a bespoke renderer.
:::

The **dense-ops layout is an architectural pattern**: facet summary over the full set, keyboard chip
filter, tree/list table, click-row detail blade plus a full detail page, the summary staying whole so
click-to-filter is stable. The inventory tier realizes it as the config-driven `ListShell` and its
primitives ([design system](/contributing/design-system/)); the analytical surfaces will reuse it.

## One renderer library, two composition modes

::::design[Target design: the renderer library and composable dashboards, tracked in #523]
Neither "every screen is hand-coded" nor "everything must be a dashboard":

- **Renderer library** (coded once): `stat`, `table`, `status-grid`, `timeline`, `heatmap`,
  `line` / `area`. Each takes a **view result plus a field-mapping** (which column is the value /
  label / time / series key); the set is closed but grown reactively.

  :::caution[Open question]
  The field-mapping contract between a view result and each renderer (the column roles per renderer
  type).
  :::
- **Coded pages** compose renderers plus custom interaction: the built-in information architecture.
- **Composable dashboards** (config-driven): operator-built grids, each
  **widget = a view ref + a renderer + a field-mapping + params**, no code per dashboard.
  Dashboard-level params flow into widget view-params, so one "system overview" dashboard works
  for any system.

  :::caution[Open question]
  The composable-dashboard schema: the widget placement grid, the view binding, and the dashboard
  params.
  :::

  :::caution[Open question]
  Whether dashboards are themselves resources (carrying the `official` boolean, saved like views) or
  a thin layer over saved views.
  :::
::::

## Coded pages and dashboards share one view layer

:::design[Target design: default views behind coded pages, tracked in #523]
A built-in page **queries a default view, not a raw resource** (the Alarms page reads the
`firing-now` view, not `GET /alarms` directly), so the same view backs a dashboard widget unchanged;
composable dashboards are the customization layer over the complete coded console.
:::

## Live updates: polling by default

Live data is **query polling** (a refetch interval; slow-changing config uses a long stale time).

::::design[Views and the SSE live relay, tracked in #523; the unit registry is #430]
A read can also **stream over the view layer (a server-side SSE relay)** where latency or fan-out
earns it. Config-dependent presentation (a severity level's id to label and color) resolves
client-side from the config view; a sample value converts on read to the operator's preferred
display unit via a future unit registry keyed by the
[property_type](/architecture/properties/)'s canonical unit, so storage stays single-unit.

:::caution[Open question]
Which high-frequency surfaces move from polling to the SSE relay, and what latency earns it.
:::
::::

## Configuration UIs

CRUD forms over the typed resource API, one per primitive (components, templates, types, tags,
rules, config, groups, schedules, severity levels, the IAM resources). The type registries each
hold their own page: location types (CRUD; Catalog, under Locations: Types) and secret types
(read-only, at `/secret-types` by URL with no subrail entry), the former tabbed Types page having split with the
system and component kinds already moved to Standards and Products ([build log](/architecture/build-log/)).
Editing a setting is editing **[config](/architecture/variables/)**, an audited mutation, not a
separate prop store ([audit](/architecture/audit/)).

:::design[The rule-authoring surface (ADR-0050); the expression editor is #524, the AI seam ADR-0001]
The standout is the **rule-authoring page**:

- an **Expr editor** for the predicate or condition, the prepared-input contract surfaced
  ([expressions](/architecture/expressions/));
- a **live blast-radius preview** (which entities a scope selects, which samples a rule would
  have fired on);
- the **AI-suggestion seam** ([AI](/architecture/ai/)): AI may propose a rule pre-filled with
  provenance; the operator edits and approves (the ordinary audited create). AI never saves a rule
  itself.
:::

## Exploration UIs

:::design[Target design: the exploration surfaces over views, tracked in #523]
Coded pages with rich interaction, all reading through views:

- **The cascade resolve view** (the standout): "why did this value win", from the
  [cascade](/architecture/cascade/) resolve output: effective value, winning source, the ordered
  shadowed bindings it beat.
- **Sample history**: `line` or `heatmap` over a time range, stale / unknown surfaced
  ([time](/architecture/time/)).
- **Alarm drill-down**: the alarm, its triggering sample and history, the actions it fired, and the
  acknowledgement (built today on the component's Alarms panel, not on a drill-down of its own).
  Snooze and resolve controls are **not** part of this: both were refused, with reasons, in
  [ADR-0109](/architecture/decisions/#adr-0109-an-alarm-carries-an-acknowledgement-and-not-a-snooze-or-a-resolve).
- **Inventory and topology**: navigable location / system / component trees,
  [health](/architecture/health/) (`status-grid`) at each level.
- **Event exploration**: the event log by entity / time / category, with the audit trail.
:::

## Information architecture

Two layers, deliberately decoupled:

1. **Routes are flat and identity-based.** Every entity page is a top-level path (`/systems`,
   `/components`, `/templates`, `/config`); a URL addresses the *entity*, never its place in the
   menu, so deep links stay stable however the menu is reorganized. No taxonomy-nested routes, no
   redirects to maintain.
2. **The sidebar groups those flat routes into clusters for browsing**: Home, Dashboards, Alarms,
   Inventory (locations, systems, components, nodes), Values (variables, secrets, config, files),
   Catalog (a single entry opening the catalog shell, next), Explore, Learn, Admin (users, roles,
   groups, audit, and the Settings leaf). A cluster is pure presentation, not a destination:
   rearrangeable and user-customizable without touching a route.

**The mode rides the URL too**
([ADR-0120](/architecture/decisions/#adr-0120-the-edit-face-is-a-url-fact)): `?edit=1` beside a
detail address (or beside a blade's id param, `?u=<id>&edit=1`) requests the edit face, behind the
same `<resource>:update` permission the footer Edit is behind; without it the link lands read-only.
Leaving edit (Cancel or Save) strips the param via history replace, so a refresh mid-edit keeps the
mode while Back never re-enters an edit the operator left. One hook (`web/src/lib/editurl.ts`) owns
both directions, the create-as-route and row-pencil handoffs are those URLs rather than in-memory
signals, and the name-to-uuid redirect keeps its query string so a name-shaped edit link survives
resolution. Every URL-reachable state is also a state the
[docs screenshot pipeline](/contributing/docs-with-everything/) can declare and capture, which is
what retired the one-shot handoffs.

**Catalog is one rail entry opening a shell.** Clicking Catalog opens a two-column catalog area: a
grouped subrail (Telemetry, Actions, Components, Systems, Locations, Metadata) whose entries
navigate to the registries' own canonical flat routes (`/products`, `/metrics`, ...), each real
page rendering in the pane beside it, and an **Overview landing** at `/catalog` with one card per
group and live registry counts, the [learning-tool](/contributing/learning-tool/) answer to "what
is all this". Subrail and cards derive from one group table judged through the same permission
filter the rail uses, so a gated entry drops from both surfaces at once and a group whose entries
are all gated away disappears with its header; secret types holds no subrail entry at all
(`/secret-types` stays routed and gated, rendering in the pane). The naming rule
([ADR-0083](/architecture/decisions/#adr-0083-the-catalog-rail-is-sectioned-by-the-estate-noun-each-registry-serves))
carries into the subrail: a group is named for the estate noun it serves, an entry keeps the
registry's own word, and where the registry's only word is "type" the entry is Types with the
group completing the sentence (Catalog, under Locations: Types). The organizing line the groups
teach: **Telemetry is what you receive, Actions is what you send or run**; an event is a record of
a happening (caught from the estate or caused by the platform), never an outbound message, which
is why Events sits in Telemetry while Rules, Commands, and the future Notifications sit in
Actions.

**Values is its own top-level group**, beside Inventory: values set on estate entities and resolved
down the cascade, a distinct genus from the entities themselves. **Config is the CI store** (desired
configuration, optionally observed back to detect drift and reconcile), distinct from platform
Settings (preferences: severity scales, schedules, retention, defaults) and Variables (free
interpolated values, no observed side); the full split is
[config, secrets, and variables](/architecture/variables/).

**Inventory holds the estate entities**: locations, systems, components, and **nodes**, the
collection daemons, monitored and scope-controlled (live, gated on `node:read` plus ABAC scope), so
a node sits in Inventory, not Admin. **Interfaces and tasks are not nav items**: an interface is a
panel on a component, a task a panel on a node, facets of the owning entity's detail page.

Admin is the renamed Settings group: Users, Roles, Groups, Audit, plus the live Settings leaf, the
platform-preferences page.

:::design[The Home situation room and the Dashboards tier, tracked in #523]
**Home is distinct from Dashboards.** Dashboards monitor the *fleet*; Home monitors the *monitor*:
config lifecycle (stale templates), control-plane health (rules failing to evaluate, samples dropped
with no matching rule), proactive suggestions. A dashboard cannot model that; "Overview" is the
default dashboard's name, not the landing.
:::

The theme is **dark-first** (the NOC aesthetic) on the brand palette (teal `#21CAB9`, navy
`#080c16`), semantic tokens only, no hardcoded colors in components.
