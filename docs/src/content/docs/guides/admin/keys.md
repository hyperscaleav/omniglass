---
title: Keys
description: "The Keys catalog: the canonical, typed keyspace a datapoint observes and a field declares, with an official baseline and operator-created custom keys."
---

**Catalog > Keys** (with `key:read`, covered by every viewer's `*:read` floor) is the estate's
**canonical keyspace**: one directory of the typed names that identify what is tracked. A **key**
is a name plus a data type (and an optional label, unit, and validation), and the same key is the
same concept wherever it appears, whether a device **reports** it (an observed datapoint) or an
operator **types** it (a declared field). Registering `serial_number` once means it cannot drift
into `serialNumber` in one place and `serial-number` in another.

The catalog is estate-wide reference data, not a scoped resource, so every key is visible to
every reader; the write gates decide who may change it.

- The directory lists every key sorted by name, each showing its **key** (the canonical name),
  **type** (`string`, `int`, `float`, `bool`, or `json`), **label**, **kind**, and **origin**
  (**official**, seed-owned, or **custom**). **name** filters by the key or its label; **type**
  and **official** narrow the list.
- **Kind** marks a key that is **observed** as telemetry: `metric` (a continuous measure),
  `state` (a discrete condition), or `log` (an event). A key with no kind is a **declared**
  attribute, something an operator sets, like `serial_number`, that is never collected off a
  device.
- **New key** (with `key:create`, granted to operators) opens a create **drawer**: name the
  **key** (lowercase, dot-hierarchied, for example `serial_number` or `interface.reachable`),
  choose its **data type**, and optionally add a **display name**, **description**, **unit**, and
  **kind**. Leave the kind as **declared** for an operator-set attribute. An invalid name (an
  uppercase letter, a hyphen, a leading digit, a stray dot) is refused with a message.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `key:update`) edits
  the label, description, and unit; the data type and kind are fixed at creation, since changing
  a key's type under the values that already use it is unsafe. **Delete** (with `key:delete`)
  removes a custom key, behind a confirm.
- A key can carry a **validation** JSON Schema (for example a `pattern` on a MAC address, an
  `enum` on a state, or `minimum` / `maximum` on a number), shown read-only on the blade. Editing
  the schema in the console is a follow-up; set it through the API for now.
- An **official** (seed-owned) key is always read-only: no Edit, no Delete, and the blade marks
  it "Seed-owned, read-only." The baseline ships the reachability keys (`icmp.reachable`,
  `interface.reachable`, and the round-trip and connect-time metrics) and a starter set of device
  attributes (`serial_number`, `mac_address`, `firmware_version`, `model_number`), so the shared
  vocabulary is the same from install to install.
- A **duplicate** name is refused (409), and an attempt to change an official key is refused too:
  the catalog has exactly one entry per name.

The catalog is also the collection vocabulary: a telemetry datapoint lands only if its name is a
registered key with a `metric`, `state`, or `log` kind (an unregistered name is dropped, not
invented). How a key becomes a field on a specific component type, with a default and a required
flag, is the type-schema, a following slice.
