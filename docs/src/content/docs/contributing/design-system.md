---
title: UI and the design system
description: The SolidJS and daisyUI console, a config-driven ListView shell over a generated typed client.
---

The operator console is a **SolidJS** SPA styled with **daisyUI 5** on **Tailwind CSS 4**. It is
a generated client of the API (typed via `openapi-fetch` off the committed `openapi.json`). The
same surfaces are also the **learning surfaces** (see
[the learning-tool restriction](/contributing/learning-tool/)).

:::note[What shipped]
Styling is **daisyUI 5 component classes + Tailwind utilities**, with two brand themes defined
through the daisyUI plugin (`omniglass-dark` default, `omniglass-light`) from the design-system
tokens. Bespoke CSS is kept to what daisyUI has no slot for: the domain severity/health colors,
the density lever, the column-resize handle, and the live pulse. Accessible interactive widgets
(dialog, combobox, select, popover) are built on **Kobalte**, styled by daisyUI, pulled in
primitive-first. The first consumers are the ⌘K command palette and the form/detail `Drawer`
(Kobalte `Dialog`).
:::

## The stack

| Concern | Choice |
|---|---|
| Framework | SolidJS (`solid-js`, `@solidjs/router`) |
| Components / theme | daisyUI 5 on Tailwind CSS v4 (the `omniglass-dark` / `omniglass-light` themes) |
| Interactive primitives | Kobalte (`Dialog` for the palette and Drawer; daisyUI `dropdown` for menus), styled by daisyUI |
| Data fetching | `@tanstack/solid-query` over a typed `openapi-fetch` client |
| Build / test | Vite, Vitest, `@solidjs/testing-library` |
| Flow / graph viz (future) | for the learning + explore surfaces; not built yet |
| Dashboards (future) | a widget grid for the dashboards surface; not built yet |

The typed client is generated, never hand-written: `openapi-typescript` turns `openapi.json` into
`schema.gen.ts`, so a route or shape change surfaces as a TypeScript error in the SPA. The cobra
CLI is generated the same way. `make gen` regenerates all of it; a non-empty diff fails the slice.

## Core UI contracts

- **One inventory shell: `ListView<N>`.** Every inventory page (Components, Systems, Locations)
  is a `ListConfig` over the one shell, **never a fork**. The shell owns the faceted filter
  header, the action rail (tree/list toggle, expand/collapse, column visibility + drag reorder,
  the primary create), tree and flattened rendering, the stacked detail blades, the full-page
  detail, the create/edit `Drawer`, and an optional summary widget board. Adding an entity of
  this class is a data layer + a config + a route (see the `add-inventory-view` skill).
- **The faceted filter is a tested engine.** `lib/predicate` is the pure matcher: values within a
  chip are OR, chips across keys are AND, clicking an active facet removes it. `FilterBar` is the
  thin staged combobox over it; the genuinely tricky list derivations (index, ancestor paths,
  flatten-vs-tree rows, client-preference parsing) are pure in `lib/listmodel`. Both are unit
  tested; `FilterBar` has a component test.
- **`can(me, resource, action)` from `/auth/me`.** The console reads the principal's flat,
  wildcard-expanded `permissions` once and gates UI affordances with O(1) checks; `ListView`
  gates create/update/delete by the entity's resource name. The server is the authority; this is
  a hint only.
- **Blades are ephemeral, the full page is addressable.** A row opens a stacked blade (the Azure
  model); Maximize promotes it to the `/<entity>/:name` URL. The blade stack holds node ids, so a
  blade survives a refetch.
- **Client preferences in localStorage, for now.** Column order/visibility and the widget board
  persist per browser; the eventual home is a per-principal user-preferences endpoint (a
  read/write swap), not the cascade.
- **Learning surfaces ride the real engine.** A concept page renders the actual pipeline against
  real or lab-simulated data, not a static diagram. The flow/graph library for these lands with
  the explore/learn surfaces.

## Button vocabulary

Buttons use a small set of **semantic intent classes** defined in `app.css`, never the raw daisyUI
color/emphasis classes, so styling is unified and a future theme restyles every button from the
theme tokens in one place. One intent per button; structural `btn`, size (`btn-sm` / `btn-xs`), and
shape (`btn-square`) still come from daisyUI.

| Intent | Class | Use |
|---|---|---|
| Primary action | `btn-action` | the main action (Save, Create, Edit, New) |
| Secondary / quiet | `btn-quiet` | Cancel, icon buttons, low-emphasis actions |
| Destructive | `btn-danger` | revoke, delete |
| State toggle | `btn-warn` / `btn-ok` | a reversible toggle that reads its state (Disable is a warning, Enable a success) |

The intents are `@apply`-composed from daisyUI in `app.css`, so they inherit the active theme's
tokens. A `style-guard` test scans the source and fails the build on a raw `btn-primary` /
`btn-ghost`, so the vocabulary cannot drift back to one-off button styling.

## Status pills

Status badges use `badge badge-sm` with a **soft hue** for a signalled state (`badge-soft
badge-success` for up/enabled/responding, `badge-soft badge-error` for down, `badge-soft
badge-warning` for stale). A **neutral** state (a node that has never checked in, a disabled task,
an unknown verdict) does **not** use `badge-neutral` or `badge-ghost`: against this theme's dark
`base-100` (`#080c16`), `badge-neutral` renders near-black and `badge-ghost` renders transparent, so
both read as invisible. Use a soft grey fill tinted from the text color instead
(`bg-base-content/10 text-base-content/70 border-transparent`), which reads as a visible pill in both
themes at the same weight as the soft hues. The same reason keeps `type` values (interface/task
`type`) as plain `font-data` text, not a `badge-neutral` chip.

## Primitives (the reuse target)

`ListView`, `FilterBar`, `Drawer`, `Donut`, `Badge`, `Fact`, `Page`, `DataTable`,
`CommandPalette`, plus the `Sidebar` / `TopBar` shell. New inventory pages consume these; new
surface *classes* (dashboards, alarms, explore, learn) add their own primitive rather than
bending `ListView`.

## Build and embed

The SPA builds with Vite (`npm run build`, into `internal/webui/dist`) and is embedded into the
Go binary under the `web` build tag, served at `/web`. One artifact serves the API and the
console. In dev, `npm run dev` serves the SPA on :5173 with `/api` proxied to a locally-running
`omniglass server`, so the frontend loop needs no rebuild.

## Tests

Component-level tests (Vitest + `@solidjs/testing-library`) cover the interactive widgets and the
pure list/filter logic (`lib/predicate`, `lib/listmodel`, `FilterBar`, the data layers). The
**browser-driven e2e tier** (drive the console as a user against the full stack) is the remaining
gate per the [test-first doctrine](/contributing/test-driven/); until it lands, user-observable
behavior is verified by hand/Playwright and is not yet a committed gate.

## How this relates to the UI architecture

This page is the **build and dev guide** for the console: the stack, the generated client, the
`ListView` shell and its primitives, and the build-and-embed pipeline. The **architecture** (the
information architecture, the read-side BFF, the live-update model) is [UI](/architecture/ui/) on
the architecture spine. Build mechanics live here; the model lives there.
