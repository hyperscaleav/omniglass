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

- Pick a row to open its **blade**: a principal's profile, the **groups** it belongs to (open
  one to stack that group's blade over the user), and its **role grants** (each a role at a scope).
- With `principal:create`, **New user** creates a human with a username and an optional initial
  password. The new user can sign in right away and change that password themselves; a fresh
  account holds no grants (so it can sign in but do nothing) until you assign a role.
- With `principal:update`, the footer **Edit** opens edit mode, where you change a user's display name,
  email, and **username**, or adjust its grants, and **Save** commits the lot; **Disable / Enable** sits in the
  footer's left slot, available without entering edit.
  Renaming is safe: their credentials and grants follow the account (they key on an internal id,
  not the username), so a renamed user keeps their password and access. Only an administrator can
  change a username; the user cannot change their own.
- With `principal_grant:create` / `:delete`, the detail panel's **grant builder** stages a set of
  changes and applies them only on **Save**, so there are no accidental edits. Type a role, then Tab
  or Enter to the scope kind, then (for a non-`all` scope) the specific location, system, or
  component from an indented tree, then the **operator** that says how that entity matches the tree:
  each commit becomes a `role @ operator scope` chip. The operator is one of **at or under** (`≥`,
  the entity and everything beneath it, the default), **under only** (`>`, the descendants but not
  the entity itself, for update and delete), or **just this** (`=`, exactly the one entity, no
  descendants and no adding children under it), so you can grant a field tech everything inside a room
  without letting them rename the room, or lock an operator to a single node. Removing an existing grant marks it (dimmed and
  struck, undoable), staging a new one shows it in green, and a pending-diff line ("+N to grant, -M
  to revoke") previews exactly what **Save** will do. That is how a fresh user gets permissions. A
  scope targets the entity by its internal id, so a grant survives a rename of that entity. One rule
  the server always holds: the **last owner grant cannot be revoked**, so the platform can never be
  locked out of administration.
- With `principal:update`, **Disable** turns off a principal: it can no longer sign in or use a
  token, but its audit history is kept (accounts are disabled, never deleted). **Enable** restores
  access. A disabled account reads **inactive** in the grid. The **last active owner cannot be
  disabled**, the same invariant that protects the last owner grant.

From the CLI the same surface is `omniglass principal list` / `get` / `create` / `update` /
`disable` / `enable`, and `omniglass grant create <id>` / `grant delete <id> <grantId>`.

In the grant builder itself, hovering a role in the picker shows its description and the permissions it
grants, so you can see what you are assigning before you stage it.

## Roles

**Settings > Roles** (with `role:read`) is the catalog of the built-in roles on the same list surface as Users
and Groups: a directory row per role (its id, whether it is **official**, what it inherits, and how many
permissions it confers), ordered least to most powerful (viewer, operator, deploy, admin, owner). Open a row for
its read-only **blade**: the description and its **effective permissions**, the full set it confers once
inheritance, wildcards, and the read floor are resolved (so `owner` reads as `> everything`, while `admin` is
broad but not the superuser, and an admin-sensitive permission like `audit:read:admin` is marked with its
`:admin` tier). It is a teaching surface: it renders the real seeded roles, not a static table. Custom-role
creation and editing are coming; today the built-in roles are read-only.

## Groups

**Settings > Groups** (with `principal_group:read`) is the admin surface for **user groups**: a group holds
`role @ scope` grants, and every member **inherits** them, so you assign access to a team once instead of per
user. Pick a row to open the group's **blade**: its **members** (add any principal, remove one, or open a member to
stack that user's blade over the group) and its **grants**, built with the same grant builder the user detail uses. A grant added to the group takes effect for every member
immediately, and is bounded by the same rule as a direct grant (you cannot grant a role above your own tier).
**New group** creates one (name, display name, description); deleting a group drops the memberships and the
inherited grants, but members keep their own direct grants.

On a **user's** detail, grants split into two: the ones you granted the user **directly** (editable in the grant
builder) and the ones **inherited from a group** (shown read-only, tagged `from <group>`), so it is always clear
where a user's access comes from. To change an inherited grant, edit the group, not the user.

From the CLI the surface is `omniglass principal-group list` / `get` / `create` / `update` / `delete`, plus its
member and grant subcommands.

## Audit

**Settings > Audit** (with `audit:read`, so **administrators and owners** only) is the read-only audit trail:
every privileged action and every sign-in, newest first, each with when it happened, who did it, the action,
and the resource. An action taken while impersonating shows the **real administrator** as the actor, with an
`as <account>` tag naming the principal whose identity they assumed (for example `admin as bob`), so
accountability lands on the human who acted and impersonation never hides them. A read-only user (a viewer) does not
see this page: the audit trail is admin-level information, so a plain "read everything" grant does not open it.
Failed sign-ins on a real account show as **login failed** (and a sign-in to a disabled account as **login
denied**), so you can spot a brute-force attempt; attempts on usernames that do not exist are not recorded.

The page uses the same faceted search as the inventory lists: filter by **who**, **action**, **resource**, or
**id** (type a term for a quick actor search, or `action:login` to pin a facet), and combine chips to narrow.
Filtering runs over the rows already loaded; **Load older** pages further back in time, so a search that comes
up short is a cue to load older and look deeper.

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

## Nodes

**Inventory > Nodes** (with `node:read`, which must be **all-scope**, since a node is
estate-wide, a location-scoped operator cannot list nodes) is the collection-daemon
inventory. Each row shows the node's name, a **liveness pill** (up, down, or never, derived
from its last heartbeat against the server's down window), and when it last checked in.
Click a row to open its detail, where an **Enroll / Re-enroll** action lives.

Enrollment is a day-one handshake, and the **token is shown once**:

- With `node:create` and `node:enroll`, **New node** registers a node (the name is its
  permanent address, no dots or whitespace; a description is optional) and immediately mints
  its enrollment token. A modal reveals the token **once**, with a copy button and a clear
  "shown once, cannot be retrieved again" warning. Copy it now and hand it to the node
  deployment; the node presents it to claim its credential. The server stores only a hash of
  the token and never logs it.
- From a node's detail, **Enroll** (or **Re-enroll**, if it is already enrolled) re-mints the
  token and shows the new one in the same once-only modal. A re-enroll **invalidates the
  previous token**, so it is both the recovery path when a token is lost and a rotation.
- The detail also shows whether the node is enrolled and when it last sent a heartbeat.

## Interfaces

**Inventory > Interfaces** (with `interface:read`) lists the **connection endpoints** on
components. Each row shows the interface name (its address), its type (`icmp` or `tcp`), its
owning component (or **server-hosted**), its node placement, and its probed target. A row
opens the detail.

- With `interface:create`, **New interface** creates one: a name, a type (the built types
  `icmp` and `tcp`), an owning component (or server-hosted, which needs an all-scope grant),
  a node placement, and a target (host:port for tcp, host for icmp).
- With `interface:update`, **Edit** changes only the **node placement** and the **target**;
  the name, type, and owning component are fixed at creation, and a left-blank field is left
  unchanged.
- With `interface:delete`, **Delete** removes it, refused while a task still references it.

Because an interface belongs to a component, it inherits that component's scope: an interface
on a component outside your scope is not listed, and its URL is a plain not-found.

## Tasks

**Inventory > Tasks** (with `task:read`) lists the **collection work** scheduled over
interfaces. Each row shows the task (its display name, or its id), its interface, its mode
(`poll` or `listen`), an **enabled** pill, and its node placement. A row opens the detail.

- With `task:create`, **New task** schedules work over an interface (chosen from the
  interfaces list), a mode (**poll** runs it on a cadence; **listen** waits for the device to
  push), an optional display name, and an enabled toggle (whether it is on the worklist).
- With `task:update`, **Edit** changes only the **display name** and the **enabled** toggle;
  the interface and mode form the task's content-addressed identity and are fixed after
  creation.
- With `task:delete`, **Delete** removes it.

## Reachability

Every component's detail carries a **Reachability** panel: is each of its interfaces
reachable, and why. One row per interface shows the interface and its endpoint, an
**availability strip** built from the verdict's recent up/down history (with an "N% up"
hint), and a **verdict pill**: **responding** (green, up and fresh), **down** (red), **stale**
(yellow, a verdict older than the freshness window of about two and a half minutes), or
**unknown** (no check yet).

Expand a row for the **layered gate**: one line per probe layer (ping at L3, port at L4) with
its signal and timing detail, then the composed verdict (the interface is up only when every
applicable probe passed). A down interface also shows a plain-language **why** line: a host
that answers ping but refuses the port is a service fault on a live box, while a host that
fails ping is unreachable on the network outright. The rows are read-only, and every value is
derived from real collected datapoints, so the panel teaches the concept it operates on.

**Add check** in the panel header (with both `interface:create` and `task:create`) authors a
reachability check the way a node runs one: pick a **type** (the transport: `tcp`, `icmp`,
`ssh`, or `http`), a **name** (the protocol the interface speaks, like `web` or `qrc`, unique
on this component and defaulted to the type), a target (host:port for tcp/ssh/http, host for
icmp), and a node, and it creates an interface owned by this component **and** a poll task over
it in one step. If the task cannot be scheduled after the interface is already created, the form
says so and offers to **retry** just the task, rather than hiding the partial state.

## What you see is your scope

The data is filtered to your scope on the server: a campus-scoped operator sees only that
campus's subtree, everywhere, automatically. You do not configure this; it follows your
grants. (Surfacing your current scope in the UI is a later addition.)
