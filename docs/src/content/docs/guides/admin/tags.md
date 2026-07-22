---
title: Tags
description: "The Tags directory: mint the governed key vocabulary, set what each key applies to and whether it cascades, and edit or delete a key."
---

**Catalog > Tags** (with `tag:read`) is the directory of the governed [tag](/architecture/tags/) key
vocabulary: the tenant-wide set of `key: value` label names an operator binds onto the estate. Each row
shows the **key**, its **Applies to** (the entity kinds it may bind to, or **Any**), and its **Binding**
(**cascades** to descendants, or **flat**, a per-entity label).

- **New tag key** (with `tag:create`, an admin permission) opens a create **drawer**: name the key (a
  normalized lowercase identifier, unique tenant-wide), check the entity kinds it **applies to** (leave
  all unchecked for any), toggle whether its bindings **cascade**, and set its **value domain**, either free
  text or **constrained to a fixed set** (an enum, like `environment` being one of `prod`, `staging`, `dev`).
  An enum is enforced on every bind and shown as a strict dropdown; a free key autocompletes the values already
  in use. Minting the vocabulary is deliberately admin-gated; *setting a value* on a key is the ordinary entity
  write, done on that entity's own page.
- Pick a row to open its **detail blade**. The footer **Edit** pencil (with `tag:update`) edits the
  governance fields (applies_to, propagates); the key name is fixed. **Delete** (with `tag:delete`)
  removes the key and, with it, every binding across the estate, behind a confirm.

Minting a **key** is admin-gated here; **setting a value** on a key is the ordinary entity write. Open a
component, system, or location, and its detail blade carries a **Tags** panel: type a key (the picker
offers the registry keys that apply to that entity kind, and with `tag:create` a **Create key** shortcut
opens this same create form), give it a value, and it binds on **Add**; the **x** on a chip removes it.
Each write is gated by that entity's own `:update`, so an operator tags what it may already edit. The
estate directories then **show** each row's effective tags in a colored [Tags column](/guides/operator/inventory/)
(the resolved cascade, keys unioning and values overriding most-specific-wins). The same operations are
`omniglass component setTag` / `system setTag` / `location setTag` and `omniglass component component effective-tag list
<component>` from the CLI.
