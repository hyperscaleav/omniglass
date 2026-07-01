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

The login screen also has a **"Use a bearer token instead"** toggle: paste a token (for a
service account, or an operator who works from the CLI) and the console authenticates with the
`Authorization` header rather than a password. Either path lands you in the same console.

The first owner is created on the server with
`omniglass bootstrap <username> --password <password>` (see [the CLI guide](/guides/cli/)).

## Your profile

Click your name in the sidebar footer to open **Your profile**. It is self-service: you edit
only your own account, whatever your role.

- **Profile.** Change your display name; it drives how you appear in the console (the sidebar
  label and the initials avatar). Your username and email are set by an administrator, not you,
  and are shown read-only.
- **Change password.** Enter your current password and a new one (at least 8 characters). A
  wrong current password is refused. Your other active sessions stay signed in.
- **Access.** A read-only view of the identity model you operate under: your principal, the
  roles granted to you, and the flattened permissions those roles carry. The server enforces
  these on every request; the console only mirrors them.

From the CLI the same two actions are `omniglass auth update-profile` and
`omniglass auth change-password` (see [the CLI guide](/guides/cli/)).

## Users

**Settings, Users** is the admin directory of every principal, humans and service accounts,
each with the roles granted to it. You see it only if you hold `principal:read`, and because a
principal is not part of any location or system tree, that grant must be **all-scope**: a
location-scoped admin cannot list users.

- Pick a row to see a principal's profile and its **role grants** (each a role at a scope).
- With `principal:create`, **New user** creates a human with a username and an optional initial
  password. The new user can sign in right away and change that password themselves; a fresh
  account holds no grants (so it can sign in but do nothing) until you assign a role.
- With `principal:update`, **Edit** changes a user's display name, email, and **username**.
  Renaming is safe: their credentials and grants follow the account (they key on an internal id,
  not the username), so a renamed user keeps their password and access. Only an administrator can
  change a username; the user cannot change their own.
- With `principal_grant:create` / `:delete`, the detail panel **grants** a role at a scope (pick a
  role, a scope kind, and, for a non-`all` scope, the specific location, system, or component from
  the picker) and revokes one with the **x** on a grant chip. That is how a fresh user gets
  permissions. A scope targets the entity by its internal id, so a grant survives a rename of that
  entity. One rule the server always holds: the **last owner grant cannot be revoked**, so the
  platform can never be locked out of administration.
- With `principal:update`, **Disable** turns off a principal: it can no longer sign in or use a
  token, but its audit history is kept (accounts are disabled, never deleted). **Enable** restores
  access. A disabled account reads **inactive** in the grid. The **last active owner cannot be
  disabled**, the same invariant that protects the last owner grant.

From the CLI the same surface is `omniglass principal list` / `get` / `create` / `update` /
`disable` / `enable`, and `omniglass grant create <id>` / `grant delete <id> <grantId>`.

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
