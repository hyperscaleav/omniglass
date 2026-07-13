---
title: Variables
description: "Plaintext typed free values owned at one scope and resolved down the cascade: the directory, create, edit, and the effective view on a component."
screenshots:
  - id: variables
    path: /web/variables
    alt: "The Variables directory: type badges and the value shown in the clear, the plaintext sibling of Secrets."
---

A **variable** is the plaintext sibling of a [secret](/guides/admin/secrets/): a typed free
value (a macro), owned at one scope and resolved down the [cascade](/architecture/cascade/) the
same way, but shown in the clear because it is not sensitive. The model underneath is [config
and credentials](/architecture/variables/).

**Settings > Variables** (with `variable:read`) is the directory of every
[variable](/architecture/variables/). Each row shows its name, a **type badge** (`string`,
`int`, `float`, `bool`, `json`), an owner label, and the **value in the clear** (no mask, no
reveal).

::screenshot{#variables}

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
`omniglass effective-variable list <component>` (see the [CLI reference](/reference/cli/)).
