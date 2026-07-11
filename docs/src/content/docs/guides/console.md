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
- **Change password.** Enter your current password and a new one. The new password must meet the
  **policy** (at least 12 characters, not a common password, and not containing your username); the
  field validates as you type, and **Generate** fills a strong random one you can **Copy**. A wrong
  current password is refused. Changing it **signs out your other sessions and tokens** (the one you
  are using stays), so the change takes effect everywhere at once.
- **Access.** A read-only view of the identity model you operate under: your principal, the
  roles granted to you, and the flattened permissions those roles carry. The server enforces
  these on every request; the console only mirrors them.

From the CLI the same two actions are `omniglass auth update-profile` and
`omniglass auth change-password` (see [the CLI guide](/guides/cli/)).

### After an administrator resets your password

If an administrator resets your password, you sign in with the password they gave you and the
console immediately gates you to a **Set a new password** screen: your account is on hold and every
other page is refused (by the server, not just the console) until you choose a new password. Enter
the temporary password as the current one and set a new policy-compliant password; once it is saved
the hold clears and you land in the console. Signing out is the only other way off the screen.

## Users

**Settings, Users** is the admin directory of every principal, humans and service accounts,
each with the roles granted to it. You see it only if you hold `principal:read`, and because a
principal is not part of any location or system tree, that grant must be **all-scope**: a
location-scoped admin cannot list users.

- Pick a row to open its **blade**: a principal's profile, the **groups** it belongs to (open
  one to stack that group's blade over the user), and its **role grants** (each a role at a scope).
- With `principal:create`, **New user** creates a human with a username and an optional initial
  password (which must meet the **password policy**: at least 12 characters, not a common password,
  and not containing the username). **Generate** fills a strong random password, kept masked, with a
  **Copy** button to hand it over (or reveal it with the show/hide toggle). The new user can sign in right away and change that
  password themselves; a fresh account holds no grants (so it can sign in but do nothing) until you
  assign a role. The form
  validates as you type: a **username** is a lowercase handle (letters, digits, and `. _ -`, no
  capitals or spaces) and an **email** must be well formed, so an invalid field shows an inline
  error and blocks the submit before the round-trip (the same rules the server enforces). The
  same handle rule and inline check apply when you rename a user in edit mode. The new user's blade
  opens **directly in edit mode**, so you assign its roles right away and one **Save** commits them.
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
- A user has a **lifecycle** in the blade footer, escalating from reversible to permanent, and reads
  pause to remove to destroy. The left slot is the reversible toggle: **Disable** (`principal:update`)
  suspends sign-in (the row reads **inactive**), **Enable** restores it. The kebab holds the stronger,
  red steps: **Archive** (`principal:archive`) soft-deletes a user (hidden from the directory, cannot
  sign in, reversibly), and **Purge** (`principal:purge`, admin-sensitive so admin and owner only)
  permanently deletes an archived user and its grants and memberships, with a confirm. The audit trail
  is kept through a purge. An archived user shows **Restore** in the left slot; the **Show archived**
  toggle above the directory surfaces hidden accounts so you can re-find one to restore or purge. The
  **last active owner** cannot be disabled or archived, the same invariant that protects the last
  owner grant.
- With `principal:reset-password`, the kebab on **another user's** blade holds **Reset password**: it
  opens an inline panel with a password field (the same **Generate** and inline policy check as the New
  user form) and sets a new password for that user without their current one. The reset **immediately
  signs the user out of every session and token**, so it doubles as a way to cut off a compromised or
  departing account. The set password stays copyable so you can hand it over; the user changes it after
  signing in. The reset is audited with **you** as the actor. It is refused on your **own** account (change your own password from **Your
  profile**, which verifies your current one) and on an **owner** (owners cannot be reset by anyone).
  This is a console path for what the CLI does with `omniglass set-password`; unlike that trusted
  direct-DB lane, the console reset enforces the password policy and the takeover guard.

From the CLI the same surface is `omniglass principal list` / `get` / `create` / `update` /
`disable` / `enable` / `archive` / `restore` / `purge`, and `omniglass grant create <id>` /
`grant delete <id> <grantId>`.

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
**New group** creates one (name, display name, description); the **name** is a lowercase handle (the same
rule as a username, validated inline), while the display name is free text. The new group then opens
**directly in edit mode**, so you add its members and grants right away and one **Save** commits them (they
are attached to the group once it exists, so creating and populating stay one flow). Deleting a group drops
the memberships and the inherited grants, but members keep their own direct grants.

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

## Secrets

**Settings > Secrets** (with `secret:read`) is the directory of every [secret](/architecture/variables/):
a typed, encrypted-at-rest value owned at one scope and resolved down the cascade. Each row shows its
name, a **type badge**, an owner label, and a **masked** field preview (`••••••`, never a value). The
same chip filter as the inventory narrows the list.

- **New secret** (with `secret:create`) opens a create **drawer**: pick a **type** (the shape,
  `snmp-community` or `basic-auth`), an **owner scope** (global, location, system, or component), then
  the owner itself from the shared indented **tree picker**, and finally the type's operator fields (a
  password input for a secret field). A global secret needs an all-scope grant.
- Pick a row to open its **detail blade** on the shared action rail. Fields render masked; **Reveal
  secret values** (with `secret:reveal`) runs the audited decrypt and shows the plaintext with a
  per-field **Copy**. Because reveal is not part of the read floor, a plain read-everything operator
  sees only masks: only an admin or owner reveals, and every reveal is written to the [audit
  trail](#audit).
- The footer **Edit** pencil (with `secret:update`) opens inline field edit; a **blank** secret field
  keeps its stored value, so you rotate one field without re-entering the rest. **Delete** (with
  `secret:delete`) sits in the footer behind a confirm.

**Effective secrets on a component.** A component's detail carries an **Effective secrets** list: the
secrets that resolve onto it through the cascade. Click one to open a nested blade showing the resolved
(revealable) value and the **full cascade**, the winning tier and the candidates it shadowed, read as
**most-specific wins: component > system > location > global**. It is the teaching view for why a given
secret is the one in effect.

From the CLI the same surface is `omniglass secret list` / `create` / `update` / `reveal` / `delete`,
`omniglass secret-type list`, and `omniglass effective-secret list <component>` (see [the CLI
guide](/guides/cli/)).

## Variables

**Settings > Variables** (with `variable:read`) is the plaintext sibling of Secrets: the directory of
every [variable](/architecture/variables/), a typed free value (a macro) owned at one scope and resolved
down the cascade. Each row shows its name, a **type badge** (`string`, `int`, `float`, `bool`, `json`),
an owner label, and the **value in the clear** (no mask, no reveal, since a variable is not sensitive).

- **New variable** (with `variable:create`) opens a create **drawer**: name the key, pick a **type** and
  an **owner scope**, choose the owner from the shared tree picker, then enter the value in a
  **type-aware editor** (a number input, a toggle for a bool, a textarea for json). A global variable
  needs an all-scope grant. `variable:create` is on the **operator** role.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `variable:update`, also an
  operator permission) opens the type-aware value editor; **Delete** (with `variable:delete`, admin and
  owner) sits behind a confirm.

**Effective variables on a component.** A component's detail carries an **Effective variables** list,
below Effective secrets: the variables that resolve onto it through the cascade. Click one to open a
nested blade showing the resolved value and the **full cascade**, the winning tier and the shadowed
candidates, read **most-specific wins: component > system > location > global**.

From the CLI the same surface is `omniglass variable list` / `create` / `update` / `delete` and
`omniglass effective-variable list <component>`.

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
