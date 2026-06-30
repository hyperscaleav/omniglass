---
title: The console
description: "Operator guide to the Omniglass web console: signing in, the inventory, filtering, blades, and editing."
---

The console is the web operator surface, served by the binary at `/web`. This guide is the
how-to for using it. How it is built is the [UI architecture](/architecture/ui/); how to add
to it is the [design system](/contributing/design-system/).

## Signing in

Sign in with your username and password. On success the server sets an httpOnly session
cookie (the browser never exposes a token to scripts), and the cookie rides on every request
for the rest of the session. Sign out from the menu in the sidebar footer, which revokes the
session and clears the cookie.

The first owner is created on the server with
`omniglass bootstrap <username> --password <password>` (see [the CLI guide](/guides/cli/)).
Service accounts and the CLI still authenticate with a bearer token in the `Authorization`
header.

## The layout

- The **sidebar** is the information architecture: sections grouped into Inventory, Catalog,
  and Settings. A live section is full strength; a section whose backend has not landed yet
  is dimmed with a **soon** tag (still clickable, with a short note on what it will do).
- The **top bar** shows the current section, a **Search (⌘K)** button, and the light/dark
  toggle.
- You only see what you can use: a tab you have no read grant for is **hidden**, and an
  action you cannot perform (create, edit, delete) does not render. The server is the
  authority on every request; the console only hides what it knows you cannot reach.

## Jump anywhere: ⌘K

Press **⌘K** (or Ctrl-K) to open the command palette and jump to any section by name. Arrow
keys move the selection, Enter navigates, Esc closes. This is a global jump, distinct from a
page's own filter.

## The inventory

Systems, Components, and Locations are the live inventory pages. They share one shape, so
once you know one you know all three.

### Filter

The bar at the top of the table is a **chip filter**. Type a field name, then an operator,
then a value; each commit becomes a chip:

- Within one chip, multiple values are **OR** (match any). Across chips, the filters are
  **AND** (match all).
- Click a chip's operator to cycle it; click its value to re-edit; the **x** removes it.
  Clicking an active summary facet (below) toggles the same chip.
- A summary widget or a count card is just a one-click shortcut to a filter chip.

### Tree, list, columns

- Tree entities (Locations, and Systems/Components where they nest) show as a **tree**; use
  the expand/collapse controls, or switch to **list** view. Filtering also flattens to a
  list, with each row's place in the tree shown above its name.
- The default list order is the **tree compressed to a flat list** (nesting preserved); click
  a column header to sort by it instead.
- The **columns** menu shows or hides columns and lets you **drag to reorder** them. The
  layout is remembered per browser.
- On Locations, a **summary board** at the top breaks the estate down by place type (a donut
  plus count cards); click any segment or card to filter to it.

### Open an entity

Click a row to open its **blade**, a panel that slides in from the right with the entity's
details. From a blade you can drill into a child (it stacks another blade behind the first),
step back with the breadcrumb, or **Maximize** to the full detail page. The full page has its
own URL, so it is shareable and bookmarkable; a blade is a quick look that does not change the
URL. Rows are keyboard-operable: Tab to a row and press Enter to open it.

### Create, edit, delete

- **New** opens a form to create an entity (name, type, placement, and where applicable a
  parent). The name is the entity's permanent address.
- **Edit** (the pencil on a row, or the button in the detail) changes the fields the entity
  allows; the address and placement are fixed after creation.
- **Delete** removes it, with a confirm. These actions appear only if your grants allow them.

## What you see is your scope

The data is filtered to your scope on the server: a campus-scoped operator sees only that
campus's subtree, everywhere, automatically. You do not configure this; it follows your
grants. (Surfacing your current scope in the UI is a later addition.)
