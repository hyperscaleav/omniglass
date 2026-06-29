---
title: UI and the design system
description: The SolidJS and daisyUI console, a generated typed client over the ViewResult renderer contract.
---

The operator console is a **SolidJS** SPA carrying its own **design-system stylesheet**
(`theme.css`) on **Tailwind CSS** for layout. It is a generated client of the API (typed via
`openapi-fetch` off the committed `openapi.json`) and a renderer over the views BFF. The same
surfaces are also the **learning surfaces** (see [the learning-tool restriction](/contributing/learning-tool/)).

:::note[What shipped]
The console ships the "Omniglass Console" design's own `theme.css` (tokens plus component
classes: `.card`, `.btn`, `.input`, `.tbl`, `.badge`, `.eyebrow`, and the dark/light, type, and
density modes via `html` data-attributes). daisyUI was **dropped**: the design is a complete,
self-contained component CSS, so a second component framework would only conflict. Tailwind is
kept for layout utilities.
:::

## The stack

| Concern | Choice |
|---|---|
| Framework | SolidJS (`solid-js`, `@solidjs/router`) |
| Components / theme | the design's own `theme.css` (tokens + component classes), Tailwind CSS v4 for layout |
| Data fetching | `@tanstack/solid-query` over a typed `openapi-fetch` client |
| Tables | `@tanstack/solid-table` (group-by, sub-rows) |
| Flow / graph viz | `solid-flow` (collection functions, pipelines, DAGs) |
| Dashboards | `gridstack` (12-column widget grid) |
| Build / test | Vite, Vitest, `@solidjs/testing-library` |

The typed client is generated, never hand-written: `openapi-typescript` turns
`openapi.json` into `schema.gen.ts`, so a route or shape change surfaces as a TypeScript
error in the SPA.

## Core UI contracts

- **One renderer per view.** Every view returns `ViewResult` (`{columns, rows}`); the SPA
  renders any view through one contract, so adding a view does not add a bespoke renderer.
- **`useCan(...)` from `/auth/me`.** The console reads the principal's flat, wildcard-
  expanded `permissions` once and gates UI affordances with O(1) checks; `grants` drive
  scope chips and "why is this hidden" explanations.
- **The dense ops layout / `DensePage` primitive.** List pages follow one shape: summary
  (donut facets over the full set) then filter (keyboard chip `FilterBar`) then a group-by
  table then a click-row detail `Drawer` plus a full detail page. Facets drive the filter;
  the summary stays whole so click-to-filter is stable. The extracted primitives
  (`DensePage`, `FilterBar`, `Donut`, `SummaryFacet`, `Drawer`, `HealthBadge`,
  `Actor`, `Sparkline`) are the reuse target.
- **Learning surfaces ride the real engine.** A concept page (a collection function, a
  edge parse step, a calc rollup, an alarm lifecycle) renders the actual pipeline against real or
  lab-simulated data, not a static diagram. `solid-flow` is the workhorse for rendering the
  DAGs the engine actually runs.

## Build and embed

The SPA builds with Vite and is embedded into the Go binary (served under `/web`); the
docs/learning site is embedded and served under `/docs`. One artifact serves the API, the
console, and the docs. Component-level tests (Vitest) run in CI; user-observable behavior
gets an e2e (browser-driven) test per the test-first doctrine.

## How this relates to the UI architecture

This page is the **build and dev guide** for the console: the stack, the generated typed client, the
reusable primitives, and the build-and-embed pipeline. The **architecture** the console implements,
the `ViewResult` renderer contract, the views BFF (read side), one renderer per view, the dense-ops
layout as a pattern, the information architecture, and the live-update model, is
[UI](/architecture/ui/) on the architecture spine. Build mechanics live here; the model lives there.
