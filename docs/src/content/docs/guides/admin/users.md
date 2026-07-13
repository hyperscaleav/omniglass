---
title: Manage users
description: "The principal directory: create a human, edit and rename, the disable-archive-purge lifecycle, admin password reset, and profile pictures."
screenshots:
  - id: users
    path: /web/users
    alt: "The Users directory: each principal with its avatar, kind badge, and grant count, plus New user and Show archived."
---

**Settings, Users** is the admin directory of every principal, humans and service accounts,
each with the roles granted to it. This page is the account lifecycle: creating, editing,
disabling, and removing a user. Giving a user access, a role at a scope, is the [grant
builder](/guides/admin/access/#the-grant-builder) on the next page. The model underneath is
[identity and access](/architecture/identity-access/).

Every row leads with the principal's avatar, its uploaded picture when it has one and its
initials otherwise. You see the directory only if you hold `principal:read`, and because a
principal is not part of any location or system tree, that grant must be **all-scope**: a
location-scoped admin cannot list users.

::screenshot{#users}

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
  signs the user out of every session** (its API tokens are kept, a token not being tied to the
  password), so it cuts off a compromised or departing account's logins at once. The set password stays copyable so you can hand it over; the user changes it after
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
- With `principal:revoke-session`, another user's blade gains **Sessions** and **API tokens** sections:
  every credential the account holds, listed with its `ogp_` locator, the **device** and **address** that
  created it, when it was **last active**, its expiry, and a token's **description**. **Revoke** any one, or
  use **Revoke all sessions** / **Revoke all tokens** in the blade's kebab, to cut off a lost laptop or
  a leaked token without resetting the account. The revoke is audited with **you** as the actor. As with
  the password reset, an **owner's** credentials cannot be revoked by anyone: their list renders
  read-only (you can see where the account is signed in, not end it), and the affordance is hidden
  unless you hold the capability.

From the CLI the same surface is `omniglass principal list` / `get` / `create` / `update` /
`disable` / `enable` / `archive` / `restore` / `purge`, plus `principal sessions <id>` /
`principal revoke-session <id> <sid>` / `principal revoke-all-sessions <id>` (see the
[CLI reference](/reference/cli/)).
