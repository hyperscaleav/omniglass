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
`Donut`, `Badge`, `Page`, `DataTable`, `IdentityCell`, `KVStacked` / `KVRow` / `FieldRow` / `BladeField`,
`CommandPalette`, plus the `Sidebar` / `TopBar` shell. New inventory pages consume these; new
surface *classes* (dashboards, alarms, explore, learn) add their own primitive rather than
bending `ListShell`.

### A field, a fact, and what read-only looks like

Three primitives cover every labelled thing on a detail surface, and nothing else may render one.

- **`KVStacked`** is a **fact**: an eyebrow label above a value. It is what a detail grid cell is,
  and it is also the read state of a field.
- **`FieldRow`** is a **form field**: the same eyebrow label above a control, with an optional
  `(i)` tooltip beside the label and a hint below. It generates the control's id and points
  `<label for>` at it, keeping the tooltip trigger outside the `<label>` so a labelable button
  never steals the control's accessible name.
- **`BladeTitle`** is the **heading** of a blade: the row's display name, tracked reactively.
- **`BladeField`** is a **blade field**: a fact when the blade is being read, a `FieldRow` when it
  is being edited, with the switch made once rather than per field. It takes the edit slot from
  `BladeEditContext`, or from an explicit `edit` prop for a detail body that also renders outside a
  provider (the tree pages render one body in both places).

**A read-only field renders as a fact, never as a box.** A bordered input that rejects typing reads
as broken rather than as read-only, and an official-row blade rendered five of them at once, directly
below three plain facts, so the same read-only state had two appearances on one panel
([ADR-0078](/architecture/decisions/)). A blade the operator cannot edit now contains nothing shaped
like a control. Both states label with the eyebrow, so the label does not change style when the
pencil is clicked.

**`BladeTitle` is the heading.** The display name of the row the operator clicked, falling back to
the identifier in the data face. It reads its row accessor inside the JSX, which is the whole of the
rule: eight pages wrote this heading by hand and all eight read the accessor once in the component
body, where a Solid read subscribes to nothing, so the heading kept the old words after a rename
until the blade was closed and reopened. `identity-vocabulary-guard.test.ts` now carries three checks over headings, one per bug that got
through: a heading must resolve its row (it rendered the raw id), must not snapshot it (it went
stale after a rename), and must render through `BladeTitle` or be named in the test's exception
list with a reason (it resolved and tracked correctly and read the wrong field, the name where
its list showed the display name). The exceptions are the entities that carry no display name at
all, where the name is the only operator-facing string: a secret, a variable, a tag, an interface.

**Free text declares itself.** `multiline` reads wrapped with its newlines preserved and edits in a
`textarea`. It is a prop rather than a second component because a component means every new page
re-decides which one to reach for.

These exist because the blade *shell* was a primitive and the blade *contents* were not. Eleven
pages defined a byte-identical local `Field`, four more went through positional `ctx.field(...)`
helpers, and the read-only box was hand-rolled 24 times, so every blade defect was an N-place
defect: a description that would not wrap was one bug in 24 fields.

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

**Who chose the label decides the second line.** On component, system and location a display name
can be one the platform rendered from a **label rule**
([ADR-0098](/architecture/decisions/#adr-0098-a-label-rule-reads-what-an-entity-is-never-where-it-sits)),
and the row says which through the pen `display_name_generated`. So the cell reads three states, not
two: a row with no label shows its name once in the data face; a row an operator labelled shows the
label with the name beneath it; a row the platform labelled shows the label, marked `Generated`, with
no second line. The rule the marker states is that typing a label claims it and clearing the field
hands it back, which is also the set of rows a rule edit would rewrite.

The three predicates live in `lib/entities` and nowhere else: `labelIsName` (which face), `hasDisplayName`
(did a human choose this) and `labelGenerated` (did the platform). The middle one used to be the
string comparison `entityLabel(e) !== e.name`, and that was the same question only while a label was
only ever operator-typed; unchanged, a generated label would have put a second identifier line under
every row in the estate.

**One renderer, pinned by a source guard.** `entityLabel` is the only place `display_name || name` is
written. `one-label-renderer.test.ts` scans every non-test source file for a hand-rolled fallback and
for the raw column interpolated into a string, and fails on either outside a short, line-precise
allowlist of rules that are genuinely not this one (a principal's name, which has no `name` column;
a picker that renders both facts as `name (Label)`). Both directions are asserted, so an allowlist
entry that stops describing anything fails too. The scan catches what no page test can: a facet
that spelled its haystack `` `${r.name} ${r.display_name}` `` searched the literal text "undefined"
on every unlabelled row.

**Three fields, no synonyms.** The identifier is the **Name**, on every column header and every
form. The friendly string is the **Display name**. The id is never labelled because it is never
shown outside a drill-in. "Technical name" and "Segment" are retired as field labels, and
`identity-vocabulary-guard.test.ts` fails the build on either appearing in label text. Neither of
the two live words is typed on a page at all: a field or a fact that shows one of them says which
fact it is bound to (`<BladeField bind="display_name">`) and takes its label from `IDENTITY_LABELS`
in `lib/entities`, with `label` refused alongside `bind` at the type level. The pairing used to be
checked by a regex over four call forms with an eight-line lookahead, after eleven blades shipped
showing two fields both called "Name"; it is now a type, and what remains of that check is a
backstop for the one failure a type cannot catch, a page bypassing the components and hand-typing
one of the words. The console's
words are the column names, so an operator reading the UI, the CLI reference, and the schema reads
one vocabulary.

**A name is a value; a segment is a position.** A segment is one dot-separated component of an
address, so `boi.17c.rm215a` is three segments and the room's name is the value in the third,
`rm215a`. A name is one segment and may not carry a dot; only an address is a path. That makes "segment"
right in prose about topic structure and wrong on a form, where the operator is typing a value and
not choosing a position.

**There is one name rule on one character set, and one validator.** A name is a kebab token
(`crestron`, `rm215a`, `icmp-rtt-avg`), capped at 100 characters, and it never carries a dot.
Every name goes through `storage.ValidateName`, which reads the table's declared identity shape to
settle whether the table bears an operator-typed name at all rather than trusting whoever wrote the
call site. There used to be four separate validators and a caller chose between them by hand, which
is how three tables reached production with no name validation at all; the last split to go was the
dotted keyspace rule, retired with its 128 character ceiling (#586).

**One name concept gets one word for it.** `property_type`, `event_type`, and `command_type` carry
the same kebab name as every other table and head their column "Name" like every other page.
`identityColumn` therefore takes no `label` option at all, and the vocabulary guard scans the source
for anyone passing one, which is the failure mode a per-page test cannot catch.

The write side does differ, page by page. `createIdentity` derives the name from the display name as
an operator types and stops the moment they edit the name by hand, and an edit form seeds it with
the existing name so relabelling can never rewrite a live address. That path belongs to the
registries, whose names have no generator and stay globally unique.

The three estate entities do not wire it. A component, a system, and a location get their names
from the platform, minted from the resolved type stem and the placement bucket, so deriving a name
from whatever prose an operator typed would claim the pen on their behalf the moment they typed a
label. Their create forms ask what and where first, then show the shape the name will take
(`display-n`), and leave the name field empty to mean "the platform names this one". The three
signal registries (`property_type`, `event_type`, `command_type`) do not wire it either, because a
signal name is chosen to match what an interface reports rather than derived from prose somebody
typed, and that was as true when those names carried dots as it is now they are single tokens. `tag`, `variable`, and `secret`
invite an exception because their prose calls them keys; they get none, and take the one rule like
everything else.

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
