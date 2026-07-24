---
title: Standards
description: "The Standards catalog: the blueprint a system conforms to, its variants, the properties it declares, and the roles a conforming system must staff; shipped standards are yours to edit, not read-only seed content."
---

**Catalog > Standards** (`/standards`, with `standard:read`, covered by every viewer's `*:read` floor)
is the directory of **standards**: the blueprints your systems are built against. A **Huddle Room**, a
**Classroom**, an **Auditorium** are standards; the meeting room on the third floor is a
[system](/guides/operator/inventory/) that **conforms** to one. Each row shows the **id**, the
**display name**, its **parent standard** if it is a variant, and its **origin**.

A standard is the **system-side counterpart of a [product](/guides/admin/products/)**. A product says
what a component *is* and what it carries; a standard says what a system is built to be and what it
carries. This is the promotion of the old `system_type` registry, which was only a label: a standard
declares a contract, so it earned its own catalog page and its own `standard:*` permission rather than
sitting under the shared type registry.

- **Conforming is optional.** A system points at a standard through its **Standard** field, and
  leaving it empty is legitimate: a **one-off system** that matches no blueprint carries only its own
  values, exactly as a component with no product does.
- **Variants use parent standard.** A specific blueprint that refines a broader one points at it with
  **parent standard** (a Large Classroom under Classroom). A standard with no parent is a base
  standard.
- **New standard** (with `standard:create`, an admin permission) opens a create drawer: name its **id**
  (a kebab identifier, unique tenant-wide, e.g. `lecture-hall`), give it a **display name**, and
  optionally pick a **parent standard**. An unknown parent is refused (422).
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `standard:update`) edits the
  display name and parent; the id is fixed. **Delete** (with `standard:delete`) removes the row, behind
  a confirm.
- **Delete is refused (409) while a system still conforms to it.** Repoint or remove those systems
  first.

The same operations are `omniglass standard list/get/create/update/delete` from the CLI (see the
[CLI reference](/reference/cli/)).

## The shipped standards are yours

Omniglass ships a starter set (Meeting Room, Huddle Room, Classroom, Auditorium, Digital Signage,
Control Room), and unlike a seeded [vendor](/guides/admin/vendors/) or
[product](/guides/admin/products/) those rows are **not official and not read-only**. They arrive as
ordinary **operator-owned** rows: rename them, re-parent them, add to their contract, delete the ones
you do not run.

This is deliberate. A standard is **forked from a template that lives in the code**, once, with **no
inheritance**: nothing in your estate points back at that template afterwards. So Omniglass can improve
its templates in any release without ever touching your rows, and your edits are never reverted by a
restart, because the boot seed installs a standard **only if it is absent**. The thing the vendor
updates and the thing you own are two different objects, which is the whole point.

The exception is the **canonical vocabulary**: the [property catalog](/guides/admin/properties/) (and
later commands and event types) is the shared namespace a driver maps onto, so it must read the same on
every install. Those entries stay **official** and read-only, and a release can correct them. See
[the seed model](/architecture/core-entities/#the-seed-model-forked-templates-versus-canonical-catalogs)
for the full reasoning.

## Declared properties: the standard's contract

A standard's blade carries a **Declared properties** panel, its **contract**: which
[properties](/guides/admin/properties/) every system conforming to it exposes, and what each one
defaults to. It is the same editor as a product's contract, on the system side.

- **Declare a property** (with `standard:update`) picks a name from the property catalog, optionally
  types a **default**, and optionally marks it **required**. The property must already exist in the
  catalog, since the contract only names it: mint it under
  [Catalog > Properties](/guides/admin/properties/) first. Declaring is **idempotent**, so declaring a
  property already on the contract revises that line in place.
- **The default is typed by the catalog, not here.** Type and validation live on the property, so a
  standard cannot redefine what `room_capacity` means, only what a system conforming to it starts with.
- **Conformance is live, not a copy.** A system does **not** fork its standard. Change a contract
  default and every conforming system that has not overridden that property picks up the new value
  immediately. Only a system's own override survives the change.
- **Required** means a conforming system must resolve the property to a value; the system's Properties
  panel blocks Save while a required property is empty.
- **Withdraw** (with `standard:delete`, behind a confirm) removes a line. Conforming systems **keep**
  any value they set for it; it simply reads as **off contract** from then on.

From the CLI the contract is `omniglass standard property list <id>`,
`omniglass standard property update <id> <property>`, and
`omniglass standard property delete <id> <property>`.

## Roles: what a conforming system needs filled

A contract says what a system **carries**. A **role** says what it **needs filled**: a room microphone, a
main display, a confidence monitor. Declare a role on a standard and **every conforming system inherits
it**, the same live inheritance the contract has, so a standard is not only a shape but a **checklist**
that says which rooms are short a component.

A role in this sense is a **slot in a room**, and it has nothing to do with the
[roles under Admin](/guides/admin/access/) that grant people access. Different word, different namespace,
no overlap.

The standard's blade carries a **Roles** panel beside its contract. Each role has:

- a **name** (its address within the standard, e.g. `room-mic`) and a **display name**;
- a **quorum**, how many components should fill it. Two ceiling mics is **one role with quorum 2**, not
  two roles. The minimum is one, since a role no component need fill is not a role;
- the **capabilities** it requires, picked from the [capability catalog](/guides/admin/capabilities/).
  The list is **all of them, not any of them**: a component must provide **every** capability listed to
  fill the role. Requiring `microphone` and `speaker` means a display cannot fill it no matter what else
  it does;
- an **impact**, what the room loses when this slot is not being filled properly.

**Impact is the health knob, and it belongs to the slot.** The same broken box matters differently
depending on the job it was doing, so the judgement lives on the role rather than on the device:

| impact | means | reach for it when |
|---|---|---|
| **outage** | the room is not working | the room cannot run without this slot |
| **degraded** (the default) | the room works, worse | losing it costs quality, not the meeting |
| **none** | nothing | you want the slot tracked, not depended on |

A dead **confidence monitor** is not a dead **main display**, and this is where you say so. Set the main
display to **outage** and the confidence monitor to **none**, and a failed confidence monitor stops paging
anybody about a room that is running fine. What that impact does downstream is the
[health rollup](/architecture/health/): a role short of its quorum contributes its impact, and the system
takes the worst one.

**Quorum is your redundancy setting**, and it pairs with impact. A role wanting **one** mic with **two**
assigned survives losing either of them, because one working mic still meets the quorum. A role wanting
**two** with two assigned is impaired the moment either one fails. Redundancy is not a separate switch, it
is the gap between what you staffed and what you need.

Declaring is **idempotent**, so saving a role that already exists revises it in place, and the capability
list **replaces** the previous requirement wholesale (drop one by leaving it out). **Withdrawing** a role
(with `standard:delete`) also removes every assignment conforming systems made to it, so withdraw only
when the slot itself is gone, not when one room's component changed. Changing a quorum or an impact
**re-evaluates every conforming system immediately**, so a retune shows up in the estate's health without
waiting for anything to poll.

Two shipped roles come with **Meeting Room**, and they are the worked example: **Room Microphone**
(requires `microphone` and `speaker`, quorum **2**) and **Main Display** (requires
`flat-panel-display`). Like the standards themselves they are **seeded only if absent**, so retuning
quorum to what your rooms actually run survives the next restart.

From the CLI: `omniglass standard role list <id>`,
`omniglass standard role update <id> <role> --display-name <label> --quorum <n> --capabilities <ids>
--impact <outage|degraded|none>`, and `omniglass standard role delete <id> <role>`.

## Staff a system against its standard

Declaring the role is half of it; the other half happens on the system. Open a system from
[Systems](/guides/operator/inventory/) and its detail carries a **Roles** panel showing **every role it
needs filled**: the ones inherited from its standard and any declared on the system itself, each marked
with where it came from.

1. **Conform the system to the standard.** A system's **Standard** field is what makes it inherit;
   a [one-off system](/guides/operator/entities/) that conforms to nothing sees only roles declared
   directly on it.
2. **Assign a component to the role.** Each role lists the components filling it and lets you add
   another. The picker is scoped to what you can see, and assignment is idempotent.
3. **Read the understaffed count.** A role wanting two components with one assigned reads as short by
   one, immediately, without any monitoring running: staffing is a fact about what you have entered. The
   room's **[health](/guides/operator/entities/#health-on-a-system-or-location)** asks the second question,
   of the components that **are** assigned, how many can currently do the job, and routes the answer up
   through the role's impact.
4. **Unassign when a component moves out.** The role goes back to understaffed until something else
   fills it. A component that is currently staffing a role **cannot be deleted**; unassign it first, so
   the system never silently loses a slot.

**A component that cannot fill the role is refused, and told why.** Assigning a display to a role that
requires `microphone` and `speaker` fails with a message naming exactly what is missing:

```
component "panel-1" cannot fill role "table-mic": missing microphone, speaker
```

That is the whole point of declaring capabilities. Your next move is in the message: either the component
really does provide them and its [capability declarations](/guides/admin/capabilities/) need fixing, or it
is the wrong component for the slot.

Roles declared **directly on a system** work identically and are edited from the same panel; use them for
what one room needs and the blueprint does not. A role inherited from the standard is withdrawn on the
**standard**, not on the system that inherits it.

From the CLI: `omniglass system role list <name>`, `omniglass system role update <name> <role>`,
`omniglass system role delete <name> <role>`, `omniglass system role assignment update <name> <role> <component>`,
and `omniglass system role assignment delete <name> <role> <component>`.

Once the roles are declared and staffed, the whole loop (raise an alarm, watch the room go degraded, find
the cause, clear it, read the history) is on
[Work with an entity](/guides/operator/entities/#health-on-a-system-or-location).
