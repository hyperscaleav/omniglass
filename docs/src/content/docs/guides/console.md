---
title: The console
description: "Operator guide to the Omniglass web console: signing in, the inventory, filtering, blades, and editing."
---

The console is the web operator surface, served by the binary at `/web`. This guide is the
how-to for **operating** it: signing in, and working the inventory of locations, systems, and
components. The platform-administration surfaces in the console's **Settings** area (Users, Roles,
Groups, Audit, Secrets, Variables) are the [admin guide](/guides/admin/). How the console is built
is the [UI architecture](/architecture/ui/); how to add to it is the [design
system](/contributing/design-system/).

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
- **Profile picture.** The avatar at the top of the panel shows your picture when you have one and
  your initials when you do not. **Upload** picks an image file (JPEG, PNG, or WebP); the server crops
  and re-encodes it to a small square, so it reads the same everywhere you appear (the sidebar and the
  Users directory). **Remove** clears it and falls back to initials. Like the rest of the page it is
  self-service: you manage only your own picture.
- **Change password.** Enter your current password and a new one. The new password must meet the
  **policy** (at least 12 characters, not a common password, and not containing your username); the
  field validates as you type, and **Generate** fills a strong random one you can **Copy**. A wrong
  current password is refused. Changing it **signs out your other sessions and tokens** (the one you
  are using stays), so the change takes effect everywhere at once.
- **Access.** A read-only view of the identity model you operate under: your principal, the
  roles granted to you, and the flattened permissions those roles carry. The server enforces
  these on every request; the console only mirrors them.

From the CLI the same actions are `omniglass auth update-profile`, `omniglass auth change-password`,
and `omniglass me setAvatar` / `omniglass me removeAvatar` for the picture (see
[the CLI guide](/guides/cli/)).

### After an administrator resets your password

If an administrator resets your password, you sign in with the password they gave you and the
console immediately gates you to a **Set a new password** screen: your account is on hold and every
other page is refused (by the server, not just the console) until you choose a new password. Enter
the temporary password as the current one and set a new policy-compliant password; once it is saved
the hold clears and you land in the console. Signing out is the only other way off the screen.

## Settings: administering the platform

The console's **Settings** area holds the platform-administration surfaces: **Users**, **Roles**,
**Groups**, **Audit**, **Secrets**, and **Variables**. Those are covered in the
[admin guide](/guides/admin/):

- [Users, roles, and groups](/guides/admin/identity/) for the principal directory, the built-in
  roles, user groups, and the grant builder.
- [The audit trail](/guides/admin/audit/) for the record of privileged actions and sign-ins.
- [Secrets and variables](/guides/admin/config/) for the cascade-resolved config and credentials.

You only see a Settings tab you hold the read grant for; the rest of this guide is the operator
inventory that everyone with a scope can reach.

## The layout

- The **sidebar** is the information architecture: sections grouped into Inventory, Catalog,
  and Settings. A live section is full strength; a section whose backend has not landed yet
  is dimmed with a **soon** tag (still clickable, with a short note on what it will do).
- The **top bar** shows the current section, a **Search (⌘K)** button, and the light/dark
  toggle.
- You only see what you can use: a tab you have no read grant for is **hidden**, and an
  action you cannot perform (create, edit, delete) does not render. The same permission
  map also **guards the route**, so a hidden tab is an unreachable URL: typing or
  bookmarking a page you cannot read redirects you to Home. The server is the authority on
  every request (it refuses regardless); the console hides what it knows you cannot reach so
  it never paints a page you cannot use.

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
- On Locations, each row wears its **type's icon** as a leading glyph (a campus, building,
  floor, and room each read differently at a glance), tinted the same hue as the type badge.
- On Locations, a **summary board** at the top breaks the estate down by place type (a donut
  plus count cards); click any segment or card to filter to it.

### Open an entity

Click a row to open its **blade**, a panel that slides in from the right with the entity's
details. From a blade you can drill into a child (it stacks another blade behind the first),
step back with the breadcrumb, or **Maximize** to the full detail page. The full page has its
own URL, so it is shareable and bookmarkable; a blade is a quick look that does not change the
URL. Rows are keyboard-operable: Tab to a row and press Enter to open it.

The identity pages (Users, Groups, and Roles) use the same blade, and there drilling crosses entities: from a
user you open a group's blade over it, and from a group you open a member's user blade, each stacking so you can
trace where access comes from without leaving the page. Each page roots one entity and drills one direction (a
user's groups, a group's members), so the stack stays shallow and the reverse relation on the far blade is a
read-only reference.

A detail blade opens **read-only**, and every entity is edited the same way through the **footer action bar**.
The blade header is chrome only (back, full-page, close); the actions live in the bar at the foot of the blade.
**Edit** (right) opens edit mode: the profile becomes inputs, the members and grants go live, and the right
cluster swaps to **Cancel** and **Save**. Changes stage locally so you can check your work first; **Save**
commits them together, **Cancel** discards them. The **destructive** action sits on the **left** and is always
available, with no need to enter edit mode: a red **Delete** for a group (a user is disabled, never deleted, so a
user's is **Disable / Enable**), each behind a confirm. Secondary actions like **Impersonate** fold into a
**⋯** menu. Edit appears only if your grants allow it, and a read-only blade (a role) shows no bar at all.

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
