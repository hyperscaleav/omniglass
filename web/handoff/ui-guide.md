# Omniglass Console — UI Development Guide

This is the source-of-truth for how the Omniglass operator console is built. It
codifies the patterns we proved in the design prototype so new pages and
components stay consistent. Drop it in the repo (e.g. as `CLAUDE.md` context or
`docs/ui-guide.md`) so Claude Code follows the same contracts.

> **Prototype vs. repo.** The design prototype (`Omniglass Console.html` + the
> `*.jsx` files) is the **behavioral reference** — it's React via `h()` for fast
> iteration. The real app is **SolidJS + Kobalte (headless/accessible) + daisyUI
> + Tailwind 4**. When porting, keep the *contracts and conventions* below; build
> the interactive bits (drawer, menu, combobox, dialog, tabs) on Kobalte and style
> with daisyUI — never hand-roll open/close, focus-trap, or ARIA.

---

## 1. Design tokens & theming

All color/spacing/radius come from CSS variables — never hardcode hex in
components. Two daisyUI themes (`omniglass-dark` default, `omniglass-light`) plus
two `data-` levers on `<html>`: `data-density` (comfortable|compact) and
`data-type` (mixed|mono).

**Surfaces (dark):** ground `--ground` #080c16 · raised `--raised` (cards/rail)
· elevated `--elevated` (popovers) · lines `--line` / `--line-strong`.
**Brand:** `--primary` teal #21cab9 · `--secondary` magenta · `--accent`.
**Severity / health language** (use these, don't invent):
- `--up` teal = healthy/up · `--warn` amber = degraded/warning · `--high` red =
  down/high/error · `--unknown` slate · plus `--info`, `--violet` (snoozed).
- `OG.healthColor[health]` maps `up|degraded|down|unknown` → the token.
**Type:** IBM Plex Sans (UI) + JetBrains Mono (data/IDs/counts). Use `.mono` for
identifiers, `.tnum` for tabular numbers. `--font-ui`, `--font-mono`.
**Radii:** `--r-field` (controls) · `--r-box` (cards) · `--r-selector`.

Text ramp: `--text` → `--text-soft` → `--text-dim` → `--text-faint`. The
`.eyebrow` class is the standard small uppercase label.

---

## 2. App shell, navigation & routing

- **Collapsible nav rail** (left): icon + label per top-level item; Inventory /
  Catalog / Settings are collapsible sections. Persisted collapse state.
- **Top bar:** section label · global command search (⌘K — *jump/search the
  whole app*, distinct from a page's own filter) · theme toggle · Tweaks.
- **Scroll container** is `#scroll-main` — sticky headers stick to *its* top.
- **Router:** `nav(routeName, params)` sets `{ name, params }`. Pages receive
  `{ nav, params }`. Deep links pass params (see cross-links below).
- **IA:** Home · Dashboards · Alarms · **Inventory** (Systems, Components,
  Locations, Interfaces, Nodes, Tasks) · Catalog · Explore · Learn · Settings.

**Domain model (get this right):**
- **Location** = a physical place: Site › Campus › Building › Floor › Room.
- **Component** = a single device (display, mic, codec, touch panel, switch) or
  a virtual/app instance (a control system app, a Zoom Room from the API).
- **System** = a logical business unit = a collection of components; usually a
  room (Boardroom Type A, Training Room, …). A System lives in a Location; its
  components inherit that location. A Room (location) is paired 1:1 with a System
  — the Room has no health of its own; health belongs to the paired System.

---

## 3. The ListView pattern — every inventory page

**An inventory page = a thin wrapper that passes a config to the generic
`ListView`.** The wrapper supplies *data + entity specifics*; `ListView` owns all
shared behavior. Locations (tree), Systems (flat), Components (flat) are all the
same component with different config. **Do not fork the shell.**

`ListView` owns: the filter header (chip-search with the action rail flowing in
the same wrap row), the action rail, columns (show/hide + drag-reorder +
3-state sort in list view), tree/list view toggle, expand/collapse-all, sticky
header, sibling row drag-reorder, the optional summary widget board, and the
detail shells (stacked blades + deep-linkable full page + create/edit form).

### Config contract

```
ListView({
  nav,                         // router
  entity: { name, plural },    // "system" / "Systems"
  storageKey,                  // localStorage prefix, e.g. "og-sys"
  primaryKind,                 // blade kind for a row ("system")
  flat: true|false,            // true = flat list (hides tree/list toggle + expand-all)
  tree,                        // array of nodes: { id, display, slug, type, health, children, ...domain }
  filterPlaceholder,

  // columns (Name is fixed first, Actions fixed last; these are the middle ones)
  columns:     { key: { label, width } },
  columnKeys:  [ ...all available... ],
  defaultCols: [ ...visible by default... ],
  cellFor(key, node),          // -> render node for a middle cell
  nameWeight(node),            // -> fontWeight for the name

  // filtering / faceting
  filterKeys,                  // FilterBar key specs (autocomplete)
  matchNode(node, chip),       // -> bool, ONE chip predicate (within-key OR, cross-key AND)
  sortVal(node, key),          // -> sortable value (list view only)

  // summary widget board — OMIT entirely for no summary rail
  widgets:        { id: { title, badge(ctx), tile(ctx) } },
  allWidgets:     [...],        // catalog (Customize menu)
  defaultWidgets: [...],        // shown by default

  // detail
  renderDetail(node, ctx),     // primary-kind blade + full-page body
  renderBlade(entry, ctx),     // -> { title, headerExtra, body } per blade kind
  FormBody,                    // component: ({ form, close, ctx }) — create/edit
  canAddChild(node),           // tree only

  // deep links (cross-entity)
  initialFocus,                // node id -> open its full page on mount
  initialChips,                // seed filter chips on mount
})
```

`ctx` (passed to config hooks) exposes the shared helpers: `crumbs(path, go)`,
`fact(label, value)`, `field(label, control, hint)`, `index` (byId/parentOf/opts),
`pathOf`, `facetActive`, `toggleFacet`, `openNode`, `pushBlade`, `popBlade`,
`goAncestor`, `closeBlades`, `setFullPage`, `openCreate`, `openEdit`, `nav`.

### Faceted filter contract (non-negotiable)
Within one key = **OR** (additive); across keys = **AND**; click an active facet
again to remove it. Summary badges/cards, donut legends, and metric cards are all
facets that write a removable chip into the filter. (Two `type` chips must show
*both* kinds, never zero.)

### Tree vs. list
- **Tree** (Locations): nested rows with caret + indent; sibling drag-reorder;
  not sortable (structure-ordered).
- **List** (Systems, Components, or any page in List mode): flat rows, **sortable
  columns** (3-state: off → asc → desc), two-line cell (breadcrumb path over name).
- The **view toggle** lets the user switch (persisted). **Mobile and any active
  filter force List** (indentation wastes narrow width; drilling a filtered tree
  is unhelpful). `flat: true` pages have no tree at all.

---

## 4. Interaction conventions

**Two ways to open a thing — distinct, consistent affordances:**
- **`›` chevron** = open in a **stacked blade** (inspect in place; Azure-blades
  model). Used on child rows and 1:1 links you peek at.
- **Maximize (⛶ corners icon)** = **expand this same entity to its full page**
  (blade header + a hover button on every row).
- **`↗` arrow-up-right** = **go to a different section/entity** (e.g. a system's
  ↗ → the Systems page). Reserve ↗ for "elsewhere," never for "expand."

**Blades (stacked detail panels):**
- Row click opens the primary blade; the caret expands/collapses the tree.
- Drilling **down** (a child, a paired entity) **pushes** a blade; navigating
  **up** (breadcrumb / parent link) **reconciles** to that ancestor (pop to it if
  already in the stack, else reset) — never stack a shallow node over a deeper one.
- Back (‹) pops one; **Esc** pops one; **clicking the open scrim closes all**;
  ✕ closes all. Earlier blades peek ~40px on desktop; **full-screen on mobile**.

**Detail = one shared component, two shells.** Blade and full page render the
*same* body (`renderDetail` / `renderBlade.body`) and the *same* interactive
breadcrumb (`crumbs`). Never build a second divergent breadcrumb/title.

**Cross-links open the specific detail, not the list.** 1:1 link → `nav(target,
{ focus: targetNodeId })` opens that entity's full page. 1:many → `nav(target,
{ chips: [...] })` lands on the target list pre-filtered. Node ids must be
globally unique (qualify when a child name repeats, e.g. `cmp:<system>:<name>`).

**Action rail (right side of the filter row):** VIEW-adjusting controls on the
left (tree/list toggle, expand/collapse-all, Columns), a thin separator, then
DO-STUFF on the right (export, **primary "New …" far-right**). The filter header
and action rail share one wrap row; the rail bumps to a new line when chips fill
the row, and the filter reclaims full width.

**Detail action row:** primary (**Edit**) far-right (matching where the form
drawer's confirm sits, so muscle memory lands there); **Delete** de-emphasized
ghost-red on the far left, away from the primary cluster.

**Filters:** chip-search is always visible; **"Clear" lives at the end of the
filter line** (only when chips exist). No manual **refresh** button — the Solid
app streams live.

**Customization is personal & quiet.** Summary widgets and table columns are
show/hide + reorder via a small "Customize"/"Columns" menu, persisted per user in
`localStorage` under the page's `storageKey`. Fixed, opinionated catalog — not a
free-for-all.

---

## 5. Reusable components (don't re-implement)

- **FilterBar** — keyboard-driven chip search; type-aware operators; props
  `bare`, `clearable`, `trailing` (action rail flows in the same wrap row).
- **Drawer** — right slide-over (Kobalte Dialog in the app); `headerExtra` slot.
- **Donut** — ring chart; `segments`, `size`, `thickness`, `onSelect`, `active`,
  `center`.
- **Badge / HealthBadge / AlarmBadge** — palette-driven status chips.
- **Icons** — Lucide-style stroke set on `window.Icons`. Key ones: `Maximize`,
  `ArrowUpRight`, `ChevronRight/Down/Left`, `GripVertical`, `Columns`,
  `ListTree`/`Rows`, `ChevronsUpDown`/`ChevronsDownUp`, `Plus/Pencil/Trash`.
- **Page** — content scaffold (breadcrumb slot + title + actions). Note: inventory
  pages built on `ListView` drop the big H1 (the top bar already labels the page).

---

## 6. General UX principles

- **Page-appropriate, opinionated widgets.** A page's summary shows what *that*
  operator watches: Locations = inventory rollups (coverage, type mix, sites);
  Systems/Components = health-first (donut, up/degraded/down). Inventory pages
  aren't monitoring dashboards.
- **No data slop.** No "x of y rows," no decorative counts/icons. Every element
  earns its place. A subtitle earns its slot only as data (count, path, status,
  timestamp) — never a description of what the page obviously is.
- **Collapsed ⇄ expanded is the same data at two densities** — badges are the
  collapsed form of a tile; never show both at once.
- **Mobile:** trees flatten to breadcrumb rows; blades go full-screen; the action
  rail wraps. Hit targets ≥ 44px.
- **Density & type** are global `data-` levers; respect `--row-h`, `--pad-card`.
- **Animations** are entrance-only and gated on reduced-motion; never strand
  content at `opacity:0` if the timeline doesn't tick.

---

## 7. Recipe — add a new inventory page

1. Create `<entity>.jsx` (repo: a Solid component) that builds a `ListView`
   config and renders it. Copy `systems.jsx` (flat) or `locations.jsx` (tree) as
   the starting point.
2. Map your data to **nodes**: `{ id (globally unique), display, slug, type,
   health, children (or [] for flat), ...domain }`.
3. Define `columns` / `columnKeys` / `defaultCols` + `cellFor`. Keep Name as the
   identity column; put the technical id in its own optional column (not inline).
4. Define `filterKeys` + `matchNode` + `sortVal`. Decide the page's facets.
5. (Optional) Define `widgets` + `defaultWidgets` for the summary rail — or omit
   for none. Make every widget a facet where it makes sense.
6. Write `renderDetail` (body) + `renderBlade` (per kind, incl. any drilled
   sub-entity like a system→components) + `FormBody`. Reuse `ctx.crumbs/fact/field`.
7. Wire 1:1 cross-links with `nav(target, { focus: id })`; 1:many with
   `nav(target, { chips })`.
8. Register: add the script/import, destructure in the app, add the `case` in the
   router, pass `{ nav, params: p }`.

If the page is a single record (not a list), build it as a full-page detail using
the same `crumbs` + `fact` helpers and the same action-row convention.
