---
title: Fields
description: "Operator-defined typed attributes declared on a component type: define the schema on the type, set a literal per component, read the effective value (set or defaulted)."
screenshots:
  - id: fields-schema
    path: /web/types
    alt: "A component-type blade in the Types catalog: the Fields editor, where an operator declares a typed field (a name, a data_type, and an optional default) that every component of that type carries."
    steps:
      - action: click
        selector: '[role="tab"]:has-text("Component")'
      - action: click
        selector: 'tr.cursor-pointer:has-text("Display")'
  - id: fields-effective
    path: /web/components/lobby-display
    alt: "The Effective fields panel on a component's detail: each field its type declares, resolved to the set literal or the type default, an override-versus-default badge, and an inline setter."
---

A **field** is an operator-defined **typed attribute declared on a type**: you add a field once to a
**component type** and every component of that type carries it, then each component sets its own value.
It is the schema layer over [secrets](/guides/admin/secrets/) and [variables](/guides/admin/variables/),
which are single cascaded cells; a field is a named slot on a type. The model underneath is [config,
secrets, and variables](/architecture/variables/#field-an-operator-defined-typed-schema-on-a-type).

This first slice is deliberately small: a field holds a **literal**, resolved on the **component alone**
(the set value, or the type default). Macro interpolation (`$var:` / `$sec:` / `$datapoint:` in a value),
the cross-type cascade, and typed file fields are later slices, described on the architecture page.

## Define a field on a type

**Catalog > Types**, the **Component** tab, is the [type catalog](/guides/admin/types/). Open a
component type to reach its blade; a **Fields** section lists the fields declared on that type (each with
a **name** and a **type badge**, `string` / `int` / `float` / `bool` / `json`, plus its default when one is
set).

::screenshot{#fields-schema}

- **Add a field** (with `field:create`, an **operator** permission) uses the inline add row: name the
  field (unique on that type), pick its **data type**, and optionally type a **default**. The default is
  coerced to the data type (an `int` default is a number, not a string) and applies to every component of
  the type until that component sets its own value.
- The Fields editor is **operator data layered onto the type**, so it is editable even on a **seed-owned
  (official) component type**, which is otherwise read-only. It renders only for the **Component** kind;
  the other type registries do not carry fields this slice.

## Set a field on a component

Open a component from the **Components** inventory. Its detail carries an **Effective fields** panel: one
row per field its type declares, each resolved to the value that applies to this component, the **literal
set on the component** or the **type default** when unset. A badge marks each row **override** (the
component set its own value) or **default** (it inherits the type default).

::screenshot{#fields-effective}

- With `field:create` (an **operator** permission) each row carries an **inline setter**: a type-aware
  input (a number input, a bool toggle, a JSON textarea) seeded from the current value, and a **Set** that
  writes the literal and refreshes the row. Setting a value flips the row to **override**; the value is
  validated against the field's data type, and a bad value is a per-row error, not a lost edit.
- A component in a scope you cannot reach is **not found**, not forbidden: the panel resolves fields only
  for components within your `field` read scope, mirroring secrets and variables.

:::note[Known limitation this slice]
The panel can set and re-set a value but **cannot yet clear** it back to the type default from the UI: the
effective read returns the field's id, not the value id the clear needs, so the clear route exists on the
API and CLI (`omniglass field-value delete <id>`) but is not wired into the panel yet.
:::

From the CLI the same surface is `omniglass field-definition list` / `create` / `update` / `delete` to
manage a type's schema, `omniglass field list <component>` to read a component's effective fields, and
`omniglass field create <component> --field <name> --value <literal>` to set one; `omniglass field-value
update` / `delete` edit a set value by id (see the [CLI reference](/reference/cli/)).
