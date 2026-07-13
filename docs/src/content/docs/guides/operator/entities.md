---
title: Work with an entity
description: "Opening an entity's blade, drilling into its children, and creating, editing, or deleting through the footer action bar."
---

Once you have [found an entity](/guides/operator/inventory/), you open it, read it, and change
it the same way everywhere in the console.

## Open an entity

Click a row to open its **blade**, a panel that slides in from the right with the entity's
details. From a blade you can drill into a child (it stacks another blade behind the first),
step back with the breadcrumb, or **Maximize** to the full detail page. The full page has its
own URL, so it is shareable and bookmarkable; a blade is a quick look that does not change the
URL. Rows are keyboard-operable: Tab to a row and press Enter to open it.

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

- **New** opens a form to create an entity (name, type, placement, and where applicable a
  parent). The name is the entity's permanent address.
- **Edit** (the pencil on a row, or the button in the detail) changes the fields the entity
  allows; the address and placement are fixed after creation.
- **Delete** removes it, with a confirm. These actions appear only if your grants allow them.
