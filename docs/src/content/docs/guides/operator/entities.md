---
title: Work with an entity
description: "Opening an entity's blade, drilling into its children, and creating, editing, or deleting through the footer action bar."
screenshots:
  - id: entity-blade
    path: /web/locations
    alt: "A location's blade slides in from the right with its details and a footer action bar."
    steps:
      - action: click
        selector: "text=East Campus"
---

Once you have [found an entity](/guides/operator/inventory/), you open it, read it, and change
it the same way everywhere in the console.

## Open an entity

Click a row to open its **blade**, a panel that slides in from the right with the entity's
details. From a blade you can drill into a child (it stacks another blade behind the first),
step back with the breadcrumb, or **Maximize** to the full detail page. The full page has its
own URL, so it is shareable and bookmarkable; a blade is a quick look that does not change the
URL. Rows are keyboard-operable: Tab to a row and press Enter to open it.

::screenshot{#entity-blade}

The identity pages (Users, Groups, and Roles) use the same blade, and there drilling crosses entities: from a
user you open a group's blade over it, and from a group you open a member's user blade, each stacking so you can
trace where access comes from without leaving the page. Each page roots one entity and drills one direction (a
user's groups, a group's members), so the stack stays shallow and the reverse relation on the far blade is a
read-only reference.

## Edit through the footer action bar

A detail blade opens **read-only**, and every entity is edited the same way through the **footer action bar**.
The blade header is chrome only (back, full-page, close); the actions live in the bar at the foot of the blade.
**Edit** (right) opens edit mode: the profile becomes inputs, the members and grants go live, and the right
cluster swaps to **Cancel** and **Save**. Changes stage locally so you can check your work first; **Save**
commits them together, **Cancel** discards them. The **destructive** action sits on the **left** and is always
available, with no need to enter edit mode: a red **Delete** for a group (a user is disabled, never deleted, so a
user's is **Disable / Enable**), each behind a confirm. Secondary actions like **Impersonate** fold into a
**⋯** menu. Edit appears only if your grants allow it, and a read-only blade (a role) shows no bar at all.

## Create, edit, delete

- **New** opens a **draft** at the entity's own `/create` address (a form for name, classifier,
  placement, and where applicable a parent). The classifier is the entity's shape: a component
  picks its [product](/guides/admin/products/), a system the
  [standard](/guides/admin/standards/) it conforms to, a location its
  [type](/guides/admin/types/). On a component and a system the classifier is **optional**, so a
  one-off unit or a system that matches no blueprint is legitimate; a location's type is
  required, since for a location the type is the only shape-definer. The name is the entity's address: lowercase
  letters, digits, and hyphens (it can be changed later, see Edit). **Create** commits it and
  drops you straight into the new entity's detail in **edit mode**, so you can tag it and finish
  configuring in place instead of hunting for it back in the list. Bindings like tags need the
  entity to exist, so they unlock the moment it is created. On a location, the type you pick may
  restrict which parent types it can sit under (or require no parent at all); a placement outside
  that set is refused with a message naming both types, right on the create form.
- **Edit** (the pencil on a row, or the button in the detail) flips that same detail into edit
  mode: the fields become inputs and the tag editor goes live. The **technical name** (the
  address) is editable here too, with an inline **Check** button that reports whether a proposed
  name is a valid slug and still free before you save; renaming changes the entity's URL, and
  existing links to the old name stop resolving. **Save** commits the changes, **Cancel** discards
  them. In **view** the detail is read-only, so tags and other bindings are shown but not editable
  until you enter edit.
- A **location**'s edit mode also makes its **Parent** editable: the Placement section swaps its
  read-only fact for a picker narrowed to the location type's allowed parents (or, when
  unconstrained, every location), excluding the location's own subtree. Moving back to root is
  not offered; a move a stale picker still lets through is refused the same way as create, inline,
  naming both types.
- **Delete** removes it, with a confirm. These actions appear only if your grants allow them.

## Properties on the detail

A component, a system, and a location each carry a **Properties** panel on their detail: one row per
property their classifier declares, resolved to the value set here or the classifier's default.
Overrides are staged with the rest of the edit and committed by the same **Save changes**. It is one
surface over one resolver, so the panel reads the same on all three; the full walkthrough is in the
[Properties guide](/guides/admin/properties/#set-a-property-on-an-instance).

## Roles on a system

A **system** carries one more panel: **Roles**, the slots it needs filled. A role is a slot (a room
microphone, a main display), not a component, so the room can say what it needs before anything is
assigned and an **empty slot stays visible**. These are slots in a room, not the
[roles that grant people access](/guides/admin/access/); the two share only the word.

Each row is one role with **where it came from**, **who fills it**, and **how many more it wants**:

- **Inherited or declared here.** A role marked as coming from the standard is declared on the
  [standard](/guides/admin/standards/) this system conforms to, and every conforming system has it.
  A role declared on this system is this room's own. A **one-off system** (conforming to no standard)
  has only its own.
- **Assigned and understaffed.** A role has a **quorum**, how many components should fill it. Two
  assigned against a quorum of two reads as staffed; one reads as short by one. That is true the
  moment you enter it, with nothing collecting: staffing is a fact about your model, not a
  measurement.
- **Assign** picks a component to fill the role; **unassign** takes it out and the role goes back to
  understaffed. Assigning the same component twice changes nothing.
- **A component staffing a role cannot be deleted.** Unassign it first. The refusal is deliberate: a
  delete that silently emptied a slot would leave the room quietly wrong.

**An assignment can be refused, and the refusal tells you why.** A role requires a set of
[capabilities](/guides/admin/capabilities/), and a component must provide **every** one of them.
Assign one that does not and you get the gap by name (`missing microphone, speaker`), which is either
a fix on the component's **Capabilities** panel or a sign that it is the wrong component for the slot.

Declaring the roles is on the [Standards guide](/guides/admin/standards/#roles-what-a-conforming-system-needs-filled);
this panel is where they get staffed.

## Capabilities on a component

A **component** carries a **Capabilities** panel: what it actually provides, resolved from its
[product](/guides/admin/products/) plus what this unit adds and minus what it suppresses. It is the set
every role assignment is checked against, and it is how a component with **no product** provides
anything at all. The walkthrough is in the
[Capabilities guide](/guides/admin/capabilities/#what-a-component-actually-provides).
