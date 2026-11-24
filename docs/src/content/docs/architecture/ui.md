---
title: UI
description: "How the operator console is built: a SolidJS SPA over the generated client, flat identity routes grouped by a sidebar, detail views and blades, and reads that poll or follow a view binding."
sidebar:
  order: 51
  badge:
    text: Partial
    variant: note
---

The console is a single-page app the server embeds and serves at `/web`. It reads and writes only
through the [API](/architecture/api/), with the typed client `make gen` writes from the OpenAPI
document, so it can do nothing a script with the same token could not. The stack and the components it
is assembled from are the [design system](/contributing/design-system/); using it is the
[operator](/operator/) and [admin](/admin/) sections. This page is how it is put together.

## The stack

SolidJS with `@solidjs/router`, TanStack Query for every server read, Kobalte for the interactive
primitives behind the in-house wrappers in `web/src/components/ui/`, and daisyUI 5 on Tailwind 4 for
styling. The theme is dark-first on the brand palette (teal `#21cab9` on navy `#080c16`), and a
component names semantic tokens only, never a hard-coded color. Logic that decides what a page shows
lives in pure modules under `web/src/lib/` (the outline, the detail landing, the edit URL), unit-tested
without a browser, and the components render what they return.

## Information architecture

Two layers, deliberately separate:

1. **Routes are flat and name the entity.** `/systems/{id}`, `/components/{id}`, `/products`,
   `/variables`: a URL addresses the thing, never where the menu files it, so a link survives any
   rearrangement of the sidebar.
2. **The sidebar groups those routes for browsing** (`web/src/lib/nav.ts`): Home, Explore, Alarms,
   Values (variables, secrets, files), Catalog, and Admin (users, roles, user groups, audit, outposts,
   platform health, settings), plus Simulation while a simulator is connected. A group is presentation,
   not a destination. An entry is shown only to a caller whose permissions open it, from the same
   check the route itself makes.

Every sidebar entry is one word, with two exceptions whose page has no good single word, User groups
and Platform health (`nav.test.ts` holds it). **Catalog** is one entry that opens a shell: a subrail
grouped by the fleet noun each catalog serves (Telemetry, Actions, Components, Systems, Locations,
Metadata) beside the routed page, and an overview at `/catalog` with a card per group
([ADR-0083](/architecture/decisions/#adr-0083-the-catalog-rail-is-sectioned-by-the-estate-noun-each-registry-serves)).
The rule its grouping teaches: Telemetry is what you receive, Actions is what you send or run.

Endpoints and tasks are not sidebar entries: an endpoint is a panel on its component, a task a panel on
its outpost.

## Explore and the detail views

**Explore** (`/explore`) is the one door into the fleet: an outline of locations with the systems that
sit at each, drawn from one projection, `GET /views/fleet`, rather than a component row per mark. Its
model is one pure function (`web/src/lib/outline.ts`) that nests the locations, folds single-child
chains, and counts systems by health; what may sit under a location is read from its type's
`allowed_parent_types`, never from a type's name.

A **detail view** is one per system, with its location as context
([ADR-0139](/architecture/decisions/)): a location card above the system card, then tabs that are URL
facts (`?tab=`), Overview, Activity, Timeline, and Configure for a caller who may change it. A
location holding exactly one system lands on that system; a component's view carries its location
card. Where a view lands is pure (`web/src/lib/detail.ts`).

A **blade** is the right-hand panel a list row opens (`BladeStack`). A fleet blade renders the detail
view's own body, so there is one way to look at a thing. While a blade is open the shell behind it is
`inert`: focus, the accessibility tree and clicks reach only the top blade.

**A name-shaped address** (`/web/components/mic-1`) resolves to the uuid and redirects, keeping its query
string, when it names exactly one visible entity; when it names several, the page says so and points
to Explore rather than guess.

## The edit lives in the URL

`?edit=1` on a detail address lands Configure already editing, and on a blade's address opens it
editing, behind the same `<resource>:update` permission as the Edit button
([ADR-0120](/architecture/decisions/#adr-0120-the-edit-face-is-a-url-fact),
[ADR-0132](/architecture/decisions/#adr-0132-configure-is-the-one-deep-editor)). Leaving the edit
strips the parameter with a history replace, so a refresh keeps the mode and Back never re-enters an
edit someone left (`web/src/lib/editurl.ts`). Every state a URL reaches is one the
[docs screenshot pipeline](/contributing/docs-with-everything/) can capture.

One edit slot per host knows an editor is open, and three rules hang off it: a delete invalidates every
read that held the row, a failed save keeps the editor open with the server's reason beside Save, and
leaving Configure with unsaved drafts asks first (`web/src/lib/leave_guard.ts`). A changed route id
keys a fresh Configure face, so one entity's half-typed edit never shows under another.

## Tooltips, not prose

An operator surface carries no explanatory paragraphs under its labels
([ADR-0127](/architecture/decisions/)): every explainer rides its label's `(i)` tooltip (`InfoTip`),
reachable by keyboard as well as hover, so the page teaches on demand and stays scannable.

## The renderer contract: ViewResult and the views BFF

A page built into the console reads through its **typed routes**: a component's alarms panel reads
`GET /components/{name}/alarms`. A small set of composed reads, the views BFF, serve a screen whose
shape no row read gives: `GET /views/fleet` for Explore and Home, `GET /views/platform` for Platform
health, `GET /search` for the `⌘K` command palette.

Where a surface draws samples, it binds a [view binding](/architecture/views/), whose answer is a
`ViewResult`: typed series per entity and key for a metric or a property, a page of rows for events and
logs. `createViewBinding` (`web/src/lib/binding.ts`) reads the history through the query cache, then
folds in the live rows from the page's one `/views:watch` stream, so a chart moves as the server
records rather than on a poll. Platform health's sparklines are drawn this way.

::::design[The renderer library and composable dashboards, tracked in #1826]
One renderer library, two ways to compose it:

- **Renderers**, coded once (a stat, a table, a status grid, a timeline, a line), each taking a
  binding's series or rows and a field mapping, so adding a view never adds a renderer.
- **Coded pages** compose them with their own interaction: the console as shipped.
- **Dashboards**, built by an operator with no code: a canvas of widgets, each a binding, a renderer,
  a field mapping and parameters, with dashboard parameters flowing into every widget so one dashboard
  points at any system.

:::caution[Open question]
The field-mapping contract between a binding's result and each renderer.
:::
::::

## Live data: polling, and the tail where it matters

A read that changes is refetched on an interval by TanStack Query; a slow-changing one (a catalog, a
setting) keeps a long stale time. A surface that draws samples follows the view binding's stream
instead. A configured presentation, such as a severity level's label and color, resolves in the
browser from its catalog read, so a result never bakes it in.

## Handing the console to omnibot

[omnibot](/architecture/ai/) opens in a panel from the top bar or the command palette. Each prompt
hands it the page the person is on as a reference, never what the console rendered: its route and
arguments, with a detail page's uuid replaced by the entity's name or address. The panel shows that
reference above the composer and reads it at send time, so a prompt follows the person, and dismissing
it leaves it out of one prompt
([ADR-2162](/architecture/decisions/#adr-2162-omnibot-is-handed-the-page-not-what-the-person-gathers)).
Each turn's reasoning and tool calls fold into one collapsed block, a tool with a renderer shows its
result the way the console shows that thing elsewhere, and a call waiting on the person is a confirm
card ([ADR-1167](/architecture/decisions/#adr-1167-the-omnibot-panel-shows-each-call-through-a-renderer-registry-over-a-generic-card)).

## Home

Home (`/`) is a row of tiles, each a real scoped read: Locations, Open alarms, Need attention and
Systems from the fleet projection, and Outposts and Platform for a caller who may read outposts. A tile
opens the page behind it, and one that cannot count says why (loading, an error, a permission it
lacks), never a dash.
