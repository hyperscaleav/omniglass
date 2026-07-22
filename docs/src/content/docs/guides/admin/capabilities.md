---
title: Capabilities
description: "The Capabilities catalog: the vocabulary of what a component can do, the product default and the component's own additions and suppressions over it, and how a system role requires them."
---

**Catalog > Capabilities** (`/capabilities`, with `capability:read`, covered by every viewer's
`*:read` floor) is the directory of **capabilities**: the flat vocabulary of what a component can
do, on the same flat-registry pattern as [Types](/guides/admin/types/) and [Tags](/guides/admin/tags/).
A capability is a plain name of a function (a microphone, a display, a camera), not a device and
not a measurement. Each row shows the **id**, the **display name**, and its **origin**
(**official**, seed-owned, or **custom**).

A capability is the hinge between two halves of the estate model. On one side, a
[product](/guides/admin/products/) declares the capabilities its instances provide, and a
[component](/guides/operator/entities/) adds to or suppresses that set with its own. On the other, a
[system role](/guides/admin/standards/#roles-what-a-conforming-system-needs-filled) **requires** a set
of them, and a component may fill the role only if it provides every one. Naming a capability once is
what makes those two sides line up.

- **New capability** (with `capability:create`, an admin permission) opens a create drawer: name
  its **id** (a short identifier, unique tenant-wide, e.g. `microphone`) and give it a **display
  name**.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `capability:update`)
  edits the display name; the id is fixed. **Delete** (with `capability:delete`) removes the row,
  behind a confirm.
- An **official** (seed-owned) row is always read-only: no Edit, no Delete, and the blade marks it
  "Seed-owned, read-only." Omniglass ships a starter set of official capabilities (Microphone,
  Speaker, Display, Flat Panel Display, Camera, Codec, Touch Panel), upserted idempotently at boot
  so the shared set cannot drift install to install; add a custom capability for anything else.
- **Delete carries no in-use guard, and it reaches further than it used to.** Every link to a
  capability is `on delete cascade`, so deleting one drops it from every product's set, from every
  component's own declarations, **and from every role that requires it**. A role that loses its last
  requirement admits any component. Removing a custom row is unconditional (still refused for an
  official row, 422); the 409 delete-refused-while-referenced rule the [Types](/guides/admin/types/)
  registry enforces lives instead on `component.product_id` (a product with components cannot be
  deleted).

The same operations are `omniglass capability list/get/create/update/delete` from the CLI (see the
[CLI reference](/reference/cli/)).

## What a component actually provides

A product is the **default** answer to "what can this component do", not the whole answer. Real
estates diverge from the catalog: a unit has a mic pod nobody modeled, a room bar's camera is dead
and the room should stop being offered as a camera, a component has **no product at all** because it
is a one-off. So a component carries its **own** capability facts, layered over its product's.

A component's detail carries a **Capabilities** panel showing the **resolved** set, which is:

> the **product's** capabilities, **plus** the ones this component adds, **minus** the ones this
> component suppresses.

- **Add a capability** the product does not claim, and the component provides it from then on. This
  is also the only way a **productless** component provides anything, and it is why a component
  without a product is still fully staffable.
- **Suppress a capability** the product does claim, and the component stops providing it. Use this
  for a removed or dead function, not for a temporary fault: suppression is a statement about what
  the unit **is**, and it will stop the component from filling any role that needs it.
- **Clear a fact** to fall back to the product. Clearing is not the same as suppressing: cleared
  means "whatever the product says", suppressed means "not this one".
- Both writes are the **component's own** (`component:update`), because a component's capabilities
  are its own data. A component outside your scope is **not found**, not forbidden.

The resolved set is what the [role assignment](/guides/admin/standards/#staff-a-system-against-its-standard)
guard checks, so this panel is where you go when an assignment was refused for a capability you know
the device has: declare it here, then assign.

From the CLI: `omniglass component capability list <name>`,
`omniglass component capability update <name> <capability> --present true|false`, and
`omniglass component capability delete <name> <capability>`.
