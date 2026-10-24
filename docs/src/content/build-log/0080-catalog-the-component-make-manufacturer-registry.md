---
title: "Catalog: the `component_make` manufacturer registry"
date: 2026-07-14
refs: []
form: bullet
---

- **Catalog: the `component_make` manufacturer registry.** The first landed slice of a larger
  make/model catalog: a flat, seed-and-custom registry (`id`, `official`, `display_name`, `icon`,
  `support_phone`, `website`) naming who makes a component, on the same official-row-read-only
  pattern as the `*_type` registries. Full CRUD (`POST/GET/PATCH/DELETE/list /api/v1/component-makes`)
  gated by a new, capability-only, unscoped **`make`** resource (`make:read` sits in the viewer
  `*:read` floor, `make:create,update,delete` at the admin tier, mirroring `type:*`); eight
  official makes (Crestron, Biamp, QSC, Shure, Cisco, Extron, Sony, Samsung) are upserted
  idempotently at boot. The `website` field is validated to an `http`/`https` URL on create and
  update, both client and server side (a non-browser caller is refused with a 422), closing a
  stored-XSS path a `javascript:`/`data:` value would otherwise open when the console rendered it
  as a live link; a value that still fails the check renders as plain text, never a dead or unsafe
  anchor. The console ships a **[Makes](/guides/admin/vendors/)** catalog page (`FlatList` + blade,
  an official row read-only, same shape as [Types](/guides/admin/types/)) and the generated CLI
  (`omniglass component-make list/get/create/update/delete`). Proven by a storage round-trip
  (official read-only, duplicate id, alphabetical list), a seed idempotency test, a real-binary API
  test (the CRUD lifecycle, the website-scheme 422, the capability gate), and web unit and
  component tests covering the safe-URL render guard. Deliberate thin cuts: no in-use delete guard
  (nothing references a make yet, so a custom row deletes unconditionally; the referential 409
  lands with `component_model`), no `component_type` genus tree, no `component_model`, and no
  picker wiring a component to a make. Deferred: the rest of the make/model catalog
  ([epic #254](https://github.com/hyperscaleav/omniglass/issues/254),
  [#255](https://github.com/hyperscaleav/omniglass/issues/255)).
