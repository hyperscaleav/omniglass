---
title: "Estate: the `type` capability resource gates the location/system/component type registries"
date: 2026-07-14
refs: []
form: bullet
---

- **Estate: the `type` capability resource gates the location/system/component type registries.** The three
  classifier registries (`location_type`, `system_type`, `component_type`) gain full CRUD behind one new,
  capability-only, unscoped **`type`** resource, replacing the borrowed `location:read` gate on
  `list-location-types`. `type:read` needs no new grant (already covered by `viewer`'s `*:read` floor);
  `type:create,update,delete` is granted to `admin`. A shared storage primitive (`typeregistry.go`) carries the
  sentinels and the delete guard: an `official` (seed-owned) row is read-only (422 on update/delete), and a row
  still referenced by a location, system, or component is refused on delete (409, a Gateway pre-count backstopped
  by the parent foreign key's `NO ACTION`). `system_type` and `component_type` also gain their first `list` route
  (previously list-only for `location_type`, absent for the other two), retiring the console's fake-from-existing-
  rows type picker workaround. `secret_type` is untouched (list-only; its fields-schema editor is a separate
  follow-up). Proven by a storage round-trip per registry (create, duplicate id 409, update, official read-only,
  in-use delete refused) and a real-binary API test per registry (the CRUD lifecycle, 422 official, 409 in-use,
  auto-discovered 403 without `type:*` by the route-gating guard). The generated CLI and web client ship in the
  same slice (`make gen`). Deferred: the Types catalog console page (segmented tab per kind, CRUD drawer
  over the three writable kinds plus a read-only view of `secret_type`) is a follow-up slice.
