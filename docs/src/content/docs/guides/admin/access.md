---
title: Roles, groups, and grants
description: "Giving a user access: the built-in roles, user groups as shared grant anchors, and the grant builder that assigns a role at a scope."
screenshots:
  - id: grant-builder
    path: /web/users
    alt: "A user's edit blade with the grant builder: a role @ scope chip and the field to stage another."
    steps:
      - action: click
        selector: "text=Operator"
      - action: click
        selector: "role=button[name=/edit/i]"
---

Access is a **grant**: a role at a scope. You assign a grant to a [user](/guides/admin/users/)
directly, or to a **group** the user belongs to; the scope decides which slice of the estate the
grant reaches. This page is the three parts of that: the **roles** you can assign, the **groups**
that share a grant across a team, and the **grant builder** that stages and commits both. The
model underneath is [identity and access](/architecture/identity-access/).

## Roles

:::note[Not a system role]
A **role** here is an access role: a capability set you grant to a person. A
**[system role](/guides/admin/standards/#roles-what-a-conforming-system-needs-filled)** is something else
entirely, a slot in a room that a component fills (a table microphone, a main display). The two share the
word and nothing else, and neither one can grant or deny the other.
:::

**Admin > Roles** (with `role:read:admin`) is the catalog of the built-in roles on the same list surface as Users
and Groups: a directory row per role (its id, whether it is **official**, what it inherits, and how many
permissions it confers), ordered least to most powerful (viewer, operator, deploy, admin, owner). Open a row for
its read-only **blade**, which shows the role's permissions as a **net** view against the full set of
capabilities the platform enforces (the **permission universe**). A `Held / Missing / All` toggle switches
between the permissions the role **holds**, the ones it is **missing**, and the whole universe with each lit or
dimmed, listed one per line in alphabetical order. Held is resolved the same way the role acts at runtime
(inheritance, wildcards, and the read floor applied), so `viewer` holds only the reads it can reach (no
`secret:read`, no admin-tier reads), `admin` holds nearly everything, `owner` holds all of it, and an
admin-sensitive permission like `audit:read:admin` is tinted for its `:admin` tier. It is a teaching surface: it
renders the real seeded roles against the real routed capabilities, not a static table. Custom-role creation and
editing are coming; today the built-in roles are read-only.

## Groups

**Admin > Groups** (with `principal_group:read:admin`) is the admin surface for **user groups**: a group holds
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
member and grant subcommands (see the [CLI reference](/reference/cli/)).

## The grant builder

Users and Groups share one control for assigning access. With `principal_grant:create` / `:delete`, the
detail panel's **grant builder** stages a set of changes and applies them only on **Save**, so there are no
accidental edits.

::screenshot{#grant-builder}

Type a role, then Tab
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

From the CLI a direct grant is `omniglass grant create <id>` / `grant delete <id> <grantId>`.
