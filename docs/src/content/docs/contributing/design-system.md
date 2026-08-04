---
title: UI and the design system
description: The SolidJS and daisyUI console, config-driven ListShell pages (with FlatList / TreeList bodies) over a generated typed client.
---

The operator console is a **SolidJS** SPA styled with **daisyUI 5** on **Tailwind CSS 4**. It is
a generated client of the API (typed via `openapi-fetch` off the committed `openapi.json`). The
same surfaces are also the **learning surfaces** (see
[the learning-tool restriction](/contributing/learning-tool/)).

:::note[What shipped]
Styling is **daisyUI 5 component classes + Tailwind utilities** on the `omniglass-dark` brand
theme defined through the daisyUI plugin from the design-system tokens. The console ships
**dark-only for now**: an `omniglass-light` theme is still defined but not reachable (the toggle
was removed while the tokens settle, since a brand teal that reads well as a fill does not as text
on white, making a second theme ongoing churn without enough payoff). It is revivable by restoring
a persisted `setTheme` and the toggle. Bespoke CSS is kept to what daisyUI has no slot for: the domain severity/health colors,
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

- **One inventory shell: `ListShell` with `FlatList` / `TreeList` bodies.** Every inventory page
  (Components, Systems, Locations)
  is a `ListConfig` over the one shell, **never a fork**. The shell owns the faceted filter
  header, the action rail (tree/list toggle, expand/collapse, column visibility + drag reorder,
  the primary create), the tree and flattened body rendering, the stacked detail blades, the full-page
  detail, the create/edit `Drawer`, and an optional summary widget board. Adding an entity of
  this class is a data layer + a config + a route (see the `add-inventory-view` skill).
- **The faceted filter is a tested engine.** `lib/predicate` is the pure matcher: values within a
  chip are OR, chips across keys are AND, clicking an active facet removes it. `FilterBar` is the
  thin staged combobox over it; the genuinely tricky list derivations (index, ancestor paths,
  flatten-vs-tree rows, client-preference parsing) are pure in `lib/listmodel`. Both are unit
  tested; `FilterBar` has a component test.
- **`can(me, resource, action)` from `/auth/me`.** The console reads the principal's flat,
  wildcard-expanded `permissions` once and gates UI affordances with O(1) checks; `ListShell`
  gates create/update/delete by the entity's resource name. The server is the authority; this is
  a hint only.
- **Blades are ephemeral, the full page is addressable.** A row opens a stacked blade (the Azure
  model); Maximize promotes it to the `/<entity>/:name` URL. The blade stack holds node ids, so a
  blade survives a refetch.
- **The shell owns the action rail; the body registers, never draws.** A panel's buttons are
  declared, not laid out: a blade body binds through `lib/blades` (`destructive`, `secondary`,
  `primary`, plus the Edit/Save cycle) and a Drawer form body binds through `lib/formactions`
  (`submitLabel`, `submitIcon`, `submit`, `busy`, `disabled`, `cancel`). `BladeStack` and `Drawer`
  each draw the resulting bar, both through the one `PanelFooter` rail, so spacing and chrome
  cannot drift between them. A body that renders its own button row is a bug, and
  `rail-ownership.test.ts` fails on it. This replaced an opt-in `DrawerFooter` helper that each
  form had to remember to wrap its buttons in: two forms forgot, and stayed wrong for months while
  the helper was copied into six new pages around them. A convention can be forgotten; a slot
  cannot. Full-page create forms still draw their own inline rail and converge when the CRUD form
  primitive lands.
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
| Primary action | `btn-action` | the main action (Save, Create, Edit, New): filled |
| Secondary / quiet | `btn-quiet` | Cancel, icon buttons, low-emphasis actions |
| Destructive | `btn-danger` | revoke, delete |
| State toggle | `btn-warn` / `btn-ok` | a reversible toggle that reads its state (Disable is a warning, Enable a success) |

The **edit flow reads the same everywhere**: Edit is a filled `btn-action` with a pencil, Save is a
filled `btn-action` with a disk icon, Cancel is a `btn-quiet` with an X. Create submits are
`btn-action` with a plus. The icons come from the local `icons` set, so a control looks identical on
every page.

The intents are `@apply`-composed from daisyUI in `app.css`, so they inherit the theme's tokens:
color lives in the tokens, not the markup. A `style-guard` test scans the source and fails the
build on **any** raw daisyUI color/emphasis button class (`btn-primary`, `btn-ghost`, `btn-outline`,
`btn-soft`, `btn-error`, `btn-success`, `btn-warning`, and the rest), so the vocabulary cannot drift
back to one-off styling.

The primary button (and every other primary-filled surface: solid badges, selected chips, avatars)
is the **bright brand teal** with a dark ink foreground, driven by the theme's own `--color-primary`
/ `--color-primary-content`. daisyUI 5's `@apply btn-primary` drops the primary foreground and lets
the filled button inherit the near-white base-content (1.8:1 on the teal, failing WCAG), so a single
unlayered `.btn-action:not(:disabled)` rule restores the theme foreground. The dormant light theme
also needs a darker teal for teal **text** on white (a `[data-theme=light] .text-primary` rule),
since the bright teal is unreadable as small text on white (1.9:1) while it reads 8.3:1 as a fill
with dark ink; that rule is inert while the console is dark-only.

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

`ListShell` (with its `FlatList` / `TreeList` bodies), `FilterBar`, `Drawer`, `PanelFooter`,
`Donut`, `Badge`, `Fact`, `Page`, `DataTable`, `IdentityCell`,
`CommandPalette`, plus the `Sidebar` / `TopBar` shell. New inventory pages consume these; new
surface *classes* (dashboards, alarms, explore, learn) add their own primitive rather than
bending `ListShell`.

### How an entity's identity reads

Every entity carries the same identity triad: an **id** (a uuid, immutable), a **name** (the
renameable identifier an operator types and the API and CLI address the row by), and an optional
**display name** (a friendly string a human reads). Two of the three are operator-facing.
`IdentityCell` states the rule once, and `identityColumn` is the `FlatList` column every page uses:

- the display name is the primary line;
- the name sits beneath it, in the data face;
- the name is suppressed when it equals the display name, so the same string never renders twice;
- an id is never a list column.

This is the same two-line treatment `TreeList` renders, so a tree and a flat list of the same entity
look like the same product. It replaced sixteen hand-written name columns written in four
incompatible idioms, which is why the header word for one fact used to be "Name" on one page and
"Key" on another.

**Three fields, no synonyms.** The identifier is the **Name**, on every column header and every
form. The friendly string is the **Display name**. The id is never labelled because it is never
shown outside a drill-in. "Technical name" and "Segment" are retired as field labels, and
`identity-vocabulary-guard.test.ts` fails the build on either appearing in label text. The console's
words are the column names, so an operator reading the UI, the CLI reference, and the schema reads
one vocabulary.

**A name is a value; a segment is a position.** A segment is one dot-separated component of an
address, so `boi.17c.rm215a` is three segments and the room's name is the value in the third,
`rm215a`. An entity name is one segment and may not carry a dot; only a keyspace name is a path. That makes "segment"
right in prose about topic structure and wrong on a form, where the operator is typing a value and
not choosing a position.

**There are two name rules on one character set, and one validator.** An entity name is kebab
(`crestron`, `rm215a`). A keyspace name is that same kebab token with an optional dot hierarchy
(`icmp.rtt-avg`). Only a keyspace name may carry the dot. Both go through `storage.ValidateName`,
which picks the rule from the table's declared identity shape rather than from whoever wrote the
call site. There used to be four separate validators and a caller chose between them by hand, which
is how three tables reached production with no name validation at all.

That split is a **validation difference, not a second concept**, so it never surfaces as a second
word. `property_type`, `event_type`, and `command_type` hold dotted names and head their column
"Name" like every other page; the difference reaches the operator as a validation message.
`identityColumn` therefore takes no `label` option at all, and the vocabulary guard scans the source
for anyone passing one, which is the failure mode a per-page test cannot catch.

The write side does split. `createIdentity` derives the name from the display name as an operator
types and stops the moment they edit the name by hand, and an edit form seeds it with the existing
name so relabelling can never rewrite a live address. The keyspace pages (`property_type`,
`event_type`, `command_type`) do not wire it, because a dotted name has no sensible derivation from
prose. `tag`, `variable`, and `secret` read as keyspace because their prose calls them keys, but
none of them carries a dot, so all three are on the entity rule.

**A Save that changed the name is two calls, not one.** The update goes first and the `:rename`
custom method goes last, because the rename is separately gated by `<resource>:rename` and is the
one call that can be refused on its own. Last means a refusal leaves the rest of the edit saved and
the name unchanged, rather than the reverse. On success the page navigates to the entity's new
address, since the old one no longer resolves ([ADR-0076](/architecture/decisions/)).

## Build and embed

The SPA builds with Vite (`npm run build`, into `internal/webui/dist`) and is embedded into the
Go binary under the `web` build tag, served at `/web`. One artifact serves the API and the
console. In dev, `npm run dev` serves the SPA on :5173 with `/api` proxied to a locally-running
`omniglass server`, so the frontend loop needs no rebuild.

## Tests

Component-level tests (Vitest + `@solidjs/testing-library`) cover the interactive widgets and the
pure list/filter logic (`lib/predicate`, `lib/listmodel`, `FilterBar`, the data layers). The
**browser-driven e2e tier** is `make test-e2e` (`web/e2e/run.sh`): it brings up the dev
Postgres, builds the binary with the console embedded, serves it, and runs Playwright against
the real login form and console, driving the surfaces as a user would per the
[test-first doctrine](/contributing/test-driven/).

## How this relates to the UI architecture

This page is the **build and dev guide** for the console: the stack, the generated client, the
`ListShell` and its primitives, and the build-and-embed pipeline. The **architecture** (the
information architecture, the read-side BFF, the live-update model) is [UI](/architecture/ui/) on
the architecture spine. Build mechanics live here; the model lives there.
