---
name: add-inventory-view
description: Use when adding an operator-console page for an inventory-class entity (a scoped resource with list/tree + CRUD, e.g. interfaces, nodes, tasks, types, tags). Covers the rail (generated typed client + hand-written ListView config), the data layer, the page config, routing/nav, the test rail, and the invariants (id-as-address facets, edit-only-mutable-fields, cycle-safe parent, authz). NOT for dashboards/alarms/explore/learn surfaces, which need their own primitives.
---

# Add an inventory-class console view

The operator console renders every inventory entity (Components, Systems, Locations) as a
**config over the one `ListView` shell**, never a fork. A new entity of this class is a data
layer plus a config plus a route. The reference implementations are
[Locations](../../../web/src/pages/Locations.tsx) (tree + summary board),
[Systems](../../../web/src/pages/Systems.tsx) (tree + cross-page nav), and
[Components](../../../web/src/pages/Components.tsx). Read one before starting.

**Scope check.** This pattern fits a scoped resource with a flat or `parent_id` tree and CRUD.
It does **not** fit the analytical surfaces (dashboards, alarms, explore, learn): those are
timeseries/graph/board shaped and each needs its own primitive. Do not bend `ListView` onto them.

## The rail (what is generated vs written)

- **Generated, never hand-written:** the OpenAPI (`api/openapi.json`), the typed SPA client
  (`web/src/api/schema.gen.ts`), and the cobra CLI. Run `make gen` after the Go API changes; a
  non-empty diff fails the slice until committed. A route/shape change surfaces as a TS error.
- **Hand-written:** the data layer (`web/src/lib/<entity>.ts`) and the page config
  (`web/src/pages/<Entity>.tsx`). The config is consumed by `ListView`; the genuinely tricky
  derivations live in [`lib/listmodel`](../../../web/src/lib/listmodel.ts) (tested).

## Steps

1. **Data layer** `web/src/lib/<entity>.ts`: thin typed wrappers over the generated `api`
   client (`list/get/create/update/delete`) plus the query KEY. Shapes follow the OpenAPI.
   `update` uses `PATCH` and carries only the mutable fields. Keep it pure-ish: I/O only, so it
   is unit-testable against a mocked client.

2. **Page config** `web/src/pages/<Entity>.tsx`:
   - `useQuery` the entity (and any list it resolves ids against, e.g. systems/locations).
   - Build the node forest in a `createMemo`: `id` = the entity **address (name)**, `display`,
     `children` from `parent_id`. Resolve foreign ids (`system_id`, `location_id`) to readable
     names from their own lists.
   - The `ListConfig`: `entity {name, plural}` (name doubles as the **authz resource**),
     `storageKey`, `nodes` (the memo accessor), `focus: () => params.name`, `columns` /
     `columnKeys` / `defaultCols`, `cellFor`, `filterKeys`, `sortVal`, a shared `detail(n, ctx)`
     used by both `renderDetail` (full page) and `renderBlade`, `FormBody`, `onOpenNode` /
     `onBack` (router navigate), `onDelete`. Optional `widgets` (structure-only, see below).
   - **No page `<Page>` H1.** The top bar labels the section; the full-page detail renders its
     own heading. Wrap the page in `<div class="og-stack flex flex-col">`.

3. **Route** `web/src/index.tsx`: add `/<entity>` and `/<entity>/:name` (same component; the
   `:name` route is the addressable full-page detail via `focus`). Remove `/<entity>` from
   `STUBS`.

4. **Nav** `web/src/lib/nav.ts`: flip the child's `live: true` and set its `resource` to the
   page's authz resource (the same string its server route gates on, e.g. `"node"`). The sidebar
   hides the tab from a principal without `<resource>:read` (`filterNav`). Until the page ships,
   leave it `!live` with no resource; the dimmed "soon" item + shared `Placeholder` cover it (set
   the child's `issue` to link its tracking issue).

5. **Test** (test-first): export a `PageDescriptor` (the static `entity`/`storageKey`/`columns`/
   `columnKeys`/`defaultCols`) and add it to the registry in
   [`descriptors.test.ts`](../../../web/src/pages/descriptors.test.ts) — the page then inherits the
   config-shape conformance matrix (the analogue of the backend `TestAuthzConformance`), no bespoke
   per-page test. Add a data-layer unit test (mock the client, assert envelope unwrap + request
   body + error throw, like [`locations.test.ts`](../../../web/src/lib/locations.test.ts)). The
   ListView derivations are already covered by `listmodel.test.ts`; add tests only for the page's
   own pure helpers, or component-test a bespoke interaction.

6. **Verify live**: `make dev` (or `npm run dev` on :5173 with `/api` proxied to a running
   `omniglass server`). Seed a few rows via the API/CLI, drive the page, screenshot to the
   ticket. The browser-driven e2e gate (full stack) is the remaining tier; until it lands,
   live-verify is by hand/Playwright and is not a committed gate.

## Invariants and gotchas

- **`node.id` is the entity address (name), globally unique.** `/<entity>/:name` focuses it.
- **Facet on a stable unique key, not a display name.** Display names collide; a facet/cross-nav
  keyed on them is ambiguous. Facet on the address (`get` returns it), and set `valueLabel` to the
  display name for the suggestion.
- **Edit form sends only the API-mutable fields** (the PATCH body: usually `display_name` +
  `*_type`). `name`, `parent`, and placement are create-only; show them read-only when editing.
- **Parent `<select>` excludes the node itself and its descendants** (cycle prevention).
- **Authorization** is a UI hint over the server: `ListView` gates create/update/delete by
  `cfg.entity.name` as the resource. Never rely on it for enforcement.
- **Blades are ephemeral (no URL); the full page is the addressable deep link.** Opening a form
  closes any open blade. The blade stack holds ids, not node refs, so it survives a refetch.
- **Live screens show real API fields only.** No invented health/metrics (those arrive with
  `component.state`). Summary widgets for this class are structure/count based; health/alarm
  widgets wait on that backend.

## The five doctrines, here

- **API first:** the client + CLI are generated from the Go; the UI consumes the generated client.
- **Test first:** the data-layer test + `listmodel`/`predicate`/`FilterBar` tests are the gate.
- **Docs with everything:** ship the view's docs in the same PR, in **both** homes: the
  operator how-to (a section in the console **operator guide** under `/guides/`, e.g. how to
  filter, open a blade, create/edit the entity) and the **architecture** page
  (`/architecture/ui`) if the entity changes the model. See
  [docs-with-everything](../../../docs/src/content/docs/contributing/docs-with-everything.md).
- **Primitive first:** consume `ListView` / `FilterBar` / `Drawer` / `Donut`; never fork the shell.
- **Functional and pedagogical:** learning surfaces render the real engine, not static diagrams.
