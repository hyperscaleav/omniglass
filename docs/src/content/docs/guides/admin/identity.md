---
title: Users, roles, and groups
description: "The admin surfaces for identity and access: the principal directory, the built-in roles, user groups as shared grant anchors, and the grant builder."
---

This is the how-to for managing **who can sign in and what they can do**. It lives in the
console's **Settings** area (Users, Roles, Groups) and mirrors the [`omniglass principal`
and `principal-group`](/guides/cli/) commands on the CLI. The model underneath, principals,
roles, grants, and the two enforcement layers, is [identity and access](/architecture/identity-access/).

Access is a **grant**: a role at a scope. You assign roles to a user directly, or to a group
the user belongs to; the scope decides which slice of the estate the grant reaches. The
[grant builder](#the-grant-builder) is the shared control for both.

## Users

**Settings, Users** is the admin directory of every principal, humans and service accounts,
each with the roles granted to it. Every row leads with the principal's avatar, its uploaded
picture when it has one and its initials otherwise. You see it only if you hold `principal:read`, and because a
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
  changes and applies them only on **Save**, so there are no accidental edits (see
  [The grant builder](#the-grant-builder)). That is how a fresh user gets permissions.
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
- With `principal:set-avatar` (an all-scope capability), a user's **Edit** blade gains an **Upload /
  Remove** picture panel: **Upload** sets that user's profile picture from an image file (JPEG, PNG, or
  WebP, normalized server-side to a small square), **Remove** clears it, and the change is audited with
  **you** as the actor. Without the capability the panel does not render, though the user's picture still
  shows in the blade header and the directory. This is a console path for `omniglass principal setAvatar
  <id>` / `removeAvatar <id>` on the CLI.

From the CLI the same surface is `omniglass principal list` / `get` / `create` / `update` /
`disable` / `enable` / `archive` / `restore` / `purge`, and `omniglass grant create <id>` /
`grant delete <id> <grantId>`.

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

## The grant builder

Users and Groups share one control for assigning access. With `principal_grant:create` / `:delete`, the
detail panel's **grant builder** stages a set of changes and applies them only on **Save**, so there are no
accidental edits. Type a role, then Tab
or Enter to the scope kind, then (for a non-`all` scope) the specific location, system, or
component from an indented tree, then the **operator** that says how that entity matches the tree:
each commit becomes a `role @ operator scope` chip. The operator is one of **at or under** (`≥`,
the entity and everything beneath it, the default), **under only** (`>`, the descendants but not
the entity itself, for update and delete), or **just this** (`=`, exactly the one entity, no
descendants and no adding children under it), so you can grant a field tech everything inside a room
without letting them rename the room, or lock an operator to a single node. Removing an existing grant marks it (dimmed and
struck, undoable), staging a new one shows it in green, and a pending-diff line ("+N to grant, -M
to revoke") previews exactly what **Save** will do. A
scope targets the entity by its internal id, so a grant survives a rename of that entity. One rule
the server always holds: the **last owner grant cannot be revoked**, so the platform can never be
locked out of administration.

Hovering a role in the picker shows its description and the permissions it grants, so you can see what
you are assigning before you stage it.
