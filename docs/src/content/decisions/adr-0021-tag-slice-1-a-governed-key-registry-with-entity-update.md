---
title: "`tag` slice 1, a governed key registry with entity-update-gated bindings"
date: 2026-07-12
status: "Accepted"
summary: "The tag primitive ships its first slice (governed key registry, per-entity bindings, cascade); minting a key is admin `tag:create`, setting a value is the entity's own `update`"
---

- **Date:** 2026-07-12 | **Status:** Accepted | **Pages:** [tags](/architecture/tags/), [config, secrets, and variables](/architecture/variables/)
- **Decision:** The **tag** primitive ships its first slice on its own [tags](/architecture/tags/) page: the governed
  **`tag`** key vocabulary, the per-entity **`tag_binding`** value cell owned on the exclusive arc
  (`global | location | system | component`), and a resolver that unions keys and overrides values most-specific-wins
  down the [cascade](/architecture/cascade/). Two permissions, not one: **minting a key** is a tenant-wide governance
  action gated by an all-scope **`tag:create`** (broadened to `tag:*` for admin, covering update and delete of keys),
  while **setting a value** is the owner's ordinary write (`component:update` and friends), so an operator who may edit
  an entity may tag it with no new grant; a global binding, having no owning entity, is gated by `tag:update`. A key
  carries **`applies_to`** (an entity-kind allow-list, empty = universal, checked on bind) and **`propagates`** (a flag
  that toggles cascade inheritance versus a flat per-entity set, the shape a [file](/architecture/files/) will reuse).
  Key names are validated as lowercase identifiers in a pure `internal/tag` package, keeping the vocabulary normalized.
  Four parts of the written design are deferred to keep the slice one vertical cut. First, the **operator console
  surface** (a Tags directory and a per-entity tag editor) is out; the slice ships over the API and the generated CLI,
  matching the files-first ordering the estate chose. Second, binding through **[groups](/architecture/groups/)** and a
  **`template`**-scoped default are out, landing with the shared-resolver work in
  [#184](https://github.com/hyperscaleav/omniglass/issues/184) that the variable member also waits on. Third,
  **value-domain governance** (a key constraining or normalizing its values) stays the page's open question; slice 1
  ships free-text values. Fourth, binding a tag onto a **[file](/architecture/files/)** waits on the files primitive.
- **Context:** The tag design lived inside the [config, secrets, and variables](/architecture/variables/) page as the
  fourth cascade user. Building it earned tags a page of its own, because its **governance model is distinct**: unlike a
  variable (one free value, one `variable:*` permission), a tag splits a curated key vocabulary (admin-minted) from
  routine value binding (operator-open via the entity's own write), and it resolves with a **union-on-key** combinator
  rather than a single value. The exclusive-arc scope and the cascade walk are shared with the variable and secret
  resolvers; the combinator and the two-permission split are what make it its own primitive. This diverges from the
  variables page's single-table sketch (the binding is its own `tag_binding` cell) and its "bindable via groups"
  note (deferred); the variables page's tag section now frames the shared cascade and points at the tags page.
- **Closes:** issue [#188](https://github.com/hyperscaleav/omniglass/issues/188) (tag slice 1). The deferrals are
  filed: the console surface [#189](https://github.com/hyperscaleav/omniglass/issues/189), value-domain governance
  [#190](https://github.com/hyperscaleav/omniglass/issues/190), and binding onto a file
  [#191](https://github.com/hyperscaleav/omniglass/issues/191); groups and template scope ride
  [#184](https://github.com/hyperscaleav/omniglass/issues/184).
