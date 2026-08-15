---
title: Operator guide
description: "Operating an Omniglass fleet day to day: signing in, finding things, working with entities, and the scope that decides what you see."
---

This is the how-to for **operating the fleet**: reading and running the inventory of locations,
systems, and components you are responsible for. It is a different job from **administering the
platform** (managing who can sign in and what they can do), which is the [admin
guide](/guides/admin/), and from **standing the platform up**, which is
[deployment](/guides/deployment/).

There are two ways to operate, and they are the same API with the same checks behind them:

- **The web console**, served by the binary at `/web`, is the point-and-click surface these
  pages walk through, one task at a time.
- **The [CLI](/guides/cli/)** is the `omniglass` binary as a client of a running server, for
  scripting and terminal work. Every command is in the [CLI reference](/reference/cli/).

## In this guide

- **[Sign in and your profile](/guides/operator/sign-in/)**: getting in with a password or a
  bearer token, and managing your own label, picture, and password.
- **[Explore your fleet](/guides/operator/fleet/)**: every system as a cluster of dots on
  one page, grouped by location, with zoom pages down to a single component.
- **[Find things in your fleet](/guides/operator/inventory/)**: the inventory pages, the chip
  filter, and the tree, list, and column controls.
- **[Work with an entity](/guides/operator/entities/)**: opening a blade, drilling into
  children, and creating, editing, or deleting.
- **[Nodes and reachability](/guides/operator/collection/)**: enrolling a collection node,
  adding a protocol-named interface to a component, and reading its reachability and events.

## Getting around

- The **sidebar** is the information architecture: sections grouped into Inventory, Values,
  Catalog, and Admin. **Catalog is a single entry** opening the catalog area: a subrail on the
  left groups the registries by the fleet noun they serve (Telemetry, Actions, Components,
  Systems, Locations, Metadata), and clicking an entry opens that registry's own page beside it,
  at its own URL. A guide path names the group and the entry: **Catalog, under Locations: Types**.
  A live entry is full strength with its live row count; one whose backend has not landed yet is
  dimmed with a **soon** tag (clickable when its stub page exists, with a short note on what it
  will do and, where one exists, the issue tracking it). The subrail's first entry, **Overview** (`/catalog`), is the
  landing: one card per group, teaching what it holds, with live registry counts linking to each
  page; you see only the groups and entries you can enter.
- The **top bar** shows the current section and a **Search (⌘K)** button. The console is
  dark-only today; the theme is an [Admin > Settings](/guides/admin/) value resolved by the
  settings engine, not a per-session toggle.
- Press **⌘K** (or Ctrl-K) to open the command palette and jump to any section by name. Arrow
  keys move the selection, Enter navigates, Esc closes. This is a global jump, distinct from a
  page's own [filter](/guides/operator/inventory/#filter). It searches every destination you can
  reach: the sidebar's own sections and, though the sidebar shows Catalog as one entry, each
  catalog registry by its own name, tagged with its group (typing `products` jumps straight to
  **Catalog, under Components: Products**). Typing a group name finds everything under it, so
  `telemetry` lists Metrics, Properties, and Events. The palette offers only what you may read,
  the same rule that hides a sidebar tab.
- You only see what you can use: a tab you have no read grant for is **hidden**, and an
  action you cannot perform does not render. The same permission map also **guards the route**,
  so a hidden tab is an unreachable URL: typing or bookmarking a page you cannot read redirects
  you to Home. The server is the authority on every request; the console only hides what it
  knows you cannot reach.

## What you see is your scope

The data is filtered to **your scope** on the server: a campus-scoped operator sees only that
campus's subtree, everywhere, automatically. You do not configure this; it follows your
[grants](/guides/admin/access/). The console hides a surface you cannot read and the server
refuses it regardless, so what you can see is exactly what you are allowed to act on. The model
behind that is [identity and access](/architecture/identity-access/). (Surfacing your current
scope in the UI is a later addition.)

How the console is built is the [UI architecture](/architecture/ui/); how to add to it is the
[design system](/contributing/design-system/).
