---
title: Secrets
description: "Encrypted-at-rest credentials owned at one scope and resolved down the cascade: the directory, create, reveal, rotate, and the effective view on a component."
screenshots:
  - id: secrets
    path: /web/secrets
    alt: "The Secrets directory: type badges, owner scope, and masked field previews, never a plaintext value."
---

A **secret** is an encrypted-at-rest value (an SNMP community, a set of basic-auth
credentials): typed, owned at one scope, and resolved down the [cascade](/architecture/cascade/)
so the most-specific owner wins onto a component. Its plaintext sibling is a
[variable](/guides/admin/variables/). The model underneath is [config and
credentials](/architecture/variables/).

**Settings > Secrets** (with `secret:read`) is the directory of every [secret](/architecture/variables/):
a typed, encrypted-at-rest value owned at one scope and resolved down the cascade. Each row shows its
name, a **type badge**, an owner label, and a **masked** field preview (`••••••`, never a value). The
same chip filter as the inventory narrows the list.

::screenshot{#secrets}

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
`omniglass secret-type list`, and `omniglass effective-secret list <component>` (see the [CLI
reference](/reference/cli/)).
