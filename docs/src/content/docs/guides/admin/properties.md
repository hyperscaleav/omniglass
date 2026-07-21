---
title: Properties
description: "The Properties catalog and the values behind it: the canonical typed names, the contract a product declares, and the value a component sets."
---

**Catalog > Properties** (with `property:read`, covered by every viewer's `*:read` floor) is the
estate's **signal catalog**: one directory of the typed names that identify what is tracked. A
**property** is a name plus a data type (and an optional label, unit, and validation), identified by
its **key** (its canonical name), and the same property is the same concept wherever it appears,
whether a device **reports** it (an observed value) or an operator **types** it (a declared value).
Registering `serial_number` once means it cannot drift into `serialNumber` in one place and
`serial-number` in another.

A property is used in three moves, and this page walks all three: the **catalog** names it, a
**product declares** it (with a default, and whether it is required), and a **component sets** it.

The catalog is estate-wide reference data, not a scoped resource, so every property is visible to
every reader; the write gates decide who may change it.

- The directory lists every property sorted by name, each showing its **key** (the canonical name),
  **type** (`string`, `int`, `float`, `bool`, or `json`), **label**, **kind**, and **origin**
  (**official**, seed-owned, or **custom**). **name** filters by the key or its label; **type**
  and **official** narrow the list.
- **Kind** marks a property that is **observed** as telemetry: `metric` (a continuous measure),
  `state` (a discrete condition), or `log` (an event). A property with no kind is a **declared**
  attribute, something an operator sets, like `serial_number`, that is never collected off a
  device.
- **New property** (with `property:create`, granted to operators) opens a create **drawer**: name
  the **key** (lowercase, dot-hierarchied, for example `serial_number` or `interface.reachable`),
  choose its **data type**, and optionally add a **display name**, **description**, **unit**, and
  **kind**. Leave the kind as **declared** for an operator-set attribute. An invalid key (an
  uppercase letter, a hyphen, a leading digit, a stray dot) is refused with a message.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `property:update`) edits
  the label, description, and unit; the data type and kind are fixed at creation, since changing
  a property's type under the values that already use it is unsafe. **Delete** (with
  `property:delete`) removes a custom property, behind a confirm.
- A property can carry a **validation** JSON Schema (for example a `pattern` on a MAC address, an
  `enum` on a state, or `minimum` / `maximum` on a number), shown read-only on the blade. Editing
  the schema in the console is a follow-up; set it through the API for now.
- An **official** (seed-owned) property is always read-only: no Edit, no Delete, and the blade marks
  it "Seed-owned, read-only." The baseline ships the reachability properties (`icmp.reachable`,
  `interface.reachable`, and the round-trip and connect-time metrics) and a starter set of device
  attributes (`serial_number`, `mac_address`, `firmware_version`, `model_number`), so the shared
  vocabulary is the same from install to install.
- A **duplicate** name is refused (409), and an attempt to change an official property is refused
  too: the catalog has exactly one entry per name.

The catalog is also the collection vocabulary: a telemetry datapoint lands only if its name is a
registered property with a `metric`, `state`, or `log` kind (an unregistered name is dropped, not
invented).

## Declare a property on a product

The catalog says a property **exists**; a **product** says which properties its instances **have**.
That declaration is the product's **contract**, edited in the **Declared properties** panel on a
product's detail blade in **Catalog > [Products](/guides/admin/products/)**. Each line names a
catalog property, optionally gives it a **default**, and optionally marks it **required**.

Declaring `serial_number`, `firmware_version`, and `model_number` on a Samsung QM55 is what makes
every QM55 in the estate carry those three, with the same names and the same types, without touching
a component. Type and validation are **not** repeated in the contract, they stay on the catalog
entry, so a property means one thing everywhere.

## Set a property on a component

Open a component from the **Components** inventory. Its detail carries a **Properties** panel: one
row per property its product declares, resolved to the value that applies here, the **value set on
the component** or the **contract default** when nothing is set. An override reads with an accent
**dot on its name** and its value in the accent colour, while an inherited (defaulted) property stays
muted.

Property edits are **batched with the component's edit**, not saved per property. Click **Edit** and
each property becomes a **stacked cell**: a name row with a right-aligned **Override** switch, and a
value row below it. The blade's **Save changes** commits every property you touched alongside the
rest of the component detail, and **Cancel** discards them.

- **The Override switch is the choice.** With the switch **off** the property inherits the resolved
  value (the contract default), shown muted with no editable input. Flip it **on** and a type-aware
  input appears, seeded from that value, and the row now reads as your own. **Revert is the switch
  off**: there is no separate clear. Both directions are the component's own write
  (`component:update`, an **operator** permission), and setting is an **idempotent upsert**, so
  overriding an already-set property patches it in place rather than failing on a second write.
- **A bool reads as a word, overrides as a toggle.** Inherited, a bool shows the resolved word
  (`true` / `false`) muted, not a switch you appear to have set; override on gives a real editable
  toggle.
- **A required property must be filled.** A property the contract marks **required** carries a red
  **`*`** by its name, stays overridden, and cannot be switched off until it holds a value. The red
  input box and a "This value is required" label appear only after a **Save** attempt leaves it
  empty, and **Save is blocked** while any required property is unfilled.
- **Off contract is legal.** A property the contract does not declare can still be set directly on
  one component (a one-off asset tag on a single unit). Those rows group under a dashed-bordered
  **Off contract** heading, so the shared shape and the local exception never blur together. Clearing
  an off-contract property removes it outright, since nothing declares it. A **productless** component
  has no contract at all, so everything it carries is off contract.
- A component in a scope you cannot reach is **not found**, not forbidden: the panel resolves
  properties only for components within your `component` read scope, mirroring secrets and variables.

From the CLI the same surface is `omniglass product properties|set-property|delete-property` for a
product's contract and `omniglass component properties|set-property|clear-property` for a component's
values (see the [CLI reference](/reference/cli/)).
