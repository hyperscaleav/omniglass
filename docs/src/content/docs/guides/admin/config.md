---
title: Secrets and variables
description: "The config and credentials the estate runs on: encrypted secrets and plaintext variables, owned at a scope and resolved most-specific-wins down the cascade."
---

Secrets and variables are the config and credentials the estate runs on: a **secret** is an
encrypted-at-rest value (an SNMP community, a set of basic-auth credentials), a **variable** is a
plaintext free value (a poll interval, a default input). Both are **typed**, **owned at one scope**,
and **resolved down the [cascade](/architecture/cascade/)** so the most-specific owner wins onto a
component. They live in the console's **Settings** area and mirror the [`omniglass secret` and
`variable`](/guides/cli/) commands. The model underneath is [config and credentials](/architecture/variables/).

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
  trail](/guides/admin/audit/).
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
