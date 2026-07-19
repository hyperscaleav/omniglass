---
title: Variables
description: "Plaintext typed free values owned at one scope and resolved down the cascade: the directory, create, and edit."
screenshots:
  - id: variables
    path: /web/variables
    alt: "The Variables directory: type badges and the value shown in the clear, the plaintext sibling of Secrets."
---

A **variable** is the plaintext sibling of a [secret](/guides/admin/secrets/): a typed free
value (a macro), owned at one scope and resolved down the [cascade](/architecture/cascade/) the
same way, but shown in the clear because it is not sensitive. The model underneath is [config
and credentials](/architecture/variables/).

**Values > Variables** (with `variable:read`) is the directory of every
[variable](/architecture/variables/). Each row shows its name, a **type badge** (`string`,
`int`, `float`, `bool`, `json`), a **scope** label (Global, or the location / system / component it
attaches to), and the **value in the clear** (no mask, no reveal).

::screenshot{#variables}

- **New variable** (with `variable:create`) opens a create **drawer**: name the key, pick a **type** and
  a **scope**, choose the entity from the shared tree picker, then enter the value in a
  **type-aware editor** (a number input, a toggle for a bool, a textarea for json). A global variable
  needs an all-scope grant. `variable:create` is on the **operator** role.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `variable:update`, also an
  operator permission) opens the type-aware value editor; **Delete** (with `variable:delete`, admin and
  owner) sits behind a confirm.

From the CLI the same surface is `omniglass variable list` / `create` / `update` / `delete` (see the
[CLI reference](/reference/cli/)).
