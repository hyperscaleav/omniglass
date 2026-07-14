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

- **New** opens a **draft** at the entity's own `/create` address (a form for name, type,
  placement, and where applicable a parent). The name is the entity's address: lowercase
  letters, digits, and hyphens (it can be changed later, see Edit). **Create** commits it and
  drops you straight into the new entity's detail in **edit mode**, so you can tag it and finish
  configuring in place instead of hunting for it back in the list. Bindings like tags need the
  entity to exist, so they unlock the moment it is created.
- **Edit** (the pencil on a row, or the button in the detail) flips that same detail into edit
  mode: the fields become inputs and the tag editor goes live. The **technical name** (the
  address) is editable here too, with an inline **Check** button that reports whether a proposed
  name is a valid slug and still free before you save; renaming changes the entity's URL, and
  existing links to the old name stop resolving. Placement stays fixed. **Save** commits the
  changes, **Cancel** discards them. In **view** the detail is read-only, so tags and other
  bindings are shown but not editable until you enter edit.
- **Delete** removes it, with a confirm. These actions appear only if your grants allow them.
