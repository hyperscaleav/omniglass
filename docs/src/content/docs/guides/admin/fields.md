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
    alt: "The Effective fields panel on a component's detail: each field its type declares, resolved to the set literal or the type default, an override-versus-default badge, an inline setter, and a revert-to-default control on an overridden field."
    steps:
      - action: hover
        selector: 'text=diagonal_inches'
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
- With `field:delete` (an **admin** permission) each **override** row also carries a **revert** control that
  clears the component's value and returns the field to its type default. The effective read returns the
  `field_value` id (`value_id`) next to the literal, so the panel knows which value the revert deletes.
- A component in a scope you cannot reach is **not found**, not forbidden: the panel resolves fields only
  for components within your `field` read scope, mirroring secrets and variables.

From the CLI the same surface is `omniglass field-definition list` / `create` / `update` / `delete` to
manage a type's schema, `omniglass field list <component>` to read a component's effective fields, and
`omniglass field create <component> --field <name> --value <literal>` to set one; `omniglass field-value
update` / `delete` edit a set value by id (see the [CLI reference](/reference/cli/)).
