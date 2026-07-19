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
    alt: "The Fields panel on a component's detail: each field its type declares, rendered as a slim row through the shared FieldControl primitive; an override reads with an accent dot on its key and its value in the accent colour, while an inherited (defaulted) field stays muted."
    steps:
      - action: hover
        selector: 'text=Diagonal inches'
  - id: fields-drilldown
    path: /web/components/lobby-display
    alt: "The field resolution drill-in: clicking a field opens its resolution, showing the raw key, the type, and the type-default to component chain with the effective value marked."
    steps:
      - action: click
        selector: 'text=Diagonal inches'
  - id: fields-edit
    path: /web/components/lobby-display
    alt: "The Fields panel in edit mode: each field is a stacked cell with a right-aligned Override switch. Off inherits the resolved value (the type default) shown muted; on reveals a type-aware input seeded from that value, and revert is the switch off. There is no per-field save; the blade's Save changes commits every touched field."
    steps:
      - action: click
        selector: 'button:has-text("Edit")'
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
  field (unique on that type), optionally give it a **display name** (a human label), pick its **data
  type**, and optionally type a **default**. The raw name stays the unique key and the interpolation
  handle; the display name is presentation only, and the console shows it wherever it is set, falling back
  to the name when it is not. The default is coerced to the data type (an `int` default is a number, not a
  string) and applies to every component of the type until that component sets its own value.
- The Fields editor is **operator data layered onto the type**, so it is editable even on a **seed-owned
  (official) component type**, which is otherwise read-only. It renders only for the **Component** kind;
  the other type registries do not carry fields this slice.

## Set a field on a component

Open a component from the **Components** inventory. Its detail carries a **Fields** panel: one row per
field its type declares, each resolved to the value that applies to this component, the **literal set on
the component** or the **type default** when nothing is set. In read mode each field renders through the
shared **FieldControl** primitive as a slim row: an **override** reads with an accent **dot on its key**
and its value in the **accent colour**, while an inherited (defaulted) field stays muted.

::screenshot{#fields-effective}

Field edits are **batched with the component's edit**, not saved per field. Click **Edit** on the
component and each field becomes a **stacked cell**: a key row with a right-aligned **Override** switch,
and a value row below it. The blade's **Save changes** commits every field you touched alongside the rest
of the component detail, and **Cancel** discards them.

- **The Override switch is the choice.** With the switch **off** the field inherits the resolved value
  (the type default this slice), shown muted with no editable input. Flip it **on** and a type-aware input
  appears, seeded from that resolved value, and the row now reads as your own value. **Revert is the switch
  off**: there is no separate clear. Overriding a field (the switch on) is `field:create`, an **operator**
  permission, and setting a value stays an **idempotent upsert**, so overriding an already-set field
  patches it in place rather than failing on a second write; reverting (the switch off on a set field,
  which deletes the stored value) is `field:delete`, an **admin** permission.
- **A bool reads as a word, overrides as a toggle.** Inherited, a bool shows the resolved word (`true` /
  `false`) muted, not a switch you appear to have set; override on gives you a real editable toggle. This
  is the case the override model exists to fix.
- **A required field must be filled.** A field its definition marks `required` (the new
  `field_definition.required`) carries a red **`*`** by its key, stays overridden, and cannot be switched
  off until it holds a value. The red input box and a "This value is required" label appear only after a
  **Save** attempt leaves it empty (standard form behaviour); **Save is blocked** while any required field
  is unfilled.
- A component in a scope you cannot reach is **not found**, not forbidden: the panel resolves fields only
  for components within your `field` read scope, mirroring secrets and variables.

Sourcing a field's value from a variable, secret, or file (a **`$` picker** on the field's edge that lists
those sources, and a sourced value shown as a symbol plus a name rather than the stored `$sec:name`) is
drawn in the control but **wired in a later slice**; this slice sets literals only.

From the CLI the same surface is `omniglass field-definition list` / `create` / `update` / `delete` to
manage a type's schema, `omniglass field list <component>` to read a component's effective fields, and
`omniglass field create <component> --field <name> --value <literal>` to set one; `omniglass field-value
update` / `delete` edit a set value by id (see the [CLI reference](/reference/cli/)).
