---
title: "The `standard` blueprint, the owner-generic resolver, and the template-fork seed model"
date: 2026-07-21
refs: []
form: bullet
---

- **The `standard` blueprint, the owner-generic resolver, and the template-fork seed model.** `system_type` is
  **promoted to `standard`**: the blueprint a system conforms to, the system-side counterpart of `product`. The
  renamed table gains **`parent_standard_id`** (variants, mirroring `product.parent_product_id`) and a declared
  property contract, and `system.system_type` becomes **`system.standard_id`**, now **optional**, so a **one-off
  system that conforms to no standard** is first-class exactly like a productless component. The seeded rows carry
  over. Since a standard now owns a contract it is a **Catalog entity, not a bare type registry**: it leaves the
  shared `type:*` permission for its own **`standard:read` / `:create` / `:update` / `:delete`** (read on the
  viewer `*:read` floor, the writes admin-tier, exactly like `product:*`) and its routes move from `/types/system`
  to **`/standards`**. Two contract tables join `product_property` on the identical shape:
  **`standard_property`** and **`location_type_property`** (`<classifier>_id`, `property_name`, an optional
  `default_value`, a `required` flag, unique per pair); `data_type` and `validation` are **never** duplicated onto
  a contract, they stay in the [property catalog](/guides/admin/properties/). The resolver then generalizes to
  **`EffectiveProperties(ctx, ownerKind, ownerID, read)`**, resolving **component, system, location, and node**
  off **one** parameterized SQL template driven by an **`ownerContract`** table (instance table, classifier
  column, contract table, contract key, arc column): component reads through `component.product_id`, system
  through `system.standard_id`, location through `location.location_type`, and a node, having no classifier,
  resolves ad-hoc values only. The two-arm shape is unchanged (contract arm
  `coalesce(the instance's value, the contract default)` with `from_contract` true, UNION the ad-hoc arm), so
  three classifier/instance pairs cannot drift apart. `guardOwnerScope` now scope-checks **every** owner arc on a
  value write, not just the component one. The routes ship regenerated into the OpenAPI document, the cobra
  CLI, and the typed SPA client: the `/standards` CRUD; `GET /standards/{id}/properties` plus
  `PUT` / `DELETE .../{property}` (gated `standard:read|update|delete`); `GET /location-types/{id}/properties`
  plus `PUT` / `DELETE .../{property}` (gated `type:*`, the registry CRUD staying at `/types/location`); and the
  value sides `GET /systems/{name}/properties` and `GET /locations/{name}/properties` plus their
  `PUT` / `DELETE .../{property}` (gated `system:*` / `location:*`, ABAC-scoped with a non-disclosing 404 out of
  scope, audited in the same transaction). The CLI reads `omniglass standard properties|set-property|delete-property`,
  `omniglass location-type properties|set-property|delete-property`, and
  `omniglass system|location properties|set-property|clear-property`. The console gains a **Standards** catalog
  page with a **Declared properties** contract editor (the Products pattern), a contract editor on the location
  type blade, and a **Properties** panel on the system and location details (the component pattern: contract
  rows, a dashed-bordered **off contract** group, an override toggle, a required property blocking Save); the
  **Types** page drops its System tab. The **seed model** is the conceptual half of the slice: a standard and a
  location type are created by **forking an in-code template**, a **one-time fork with no inheritance**, so a
  template can be improved in any release because nothing in any tenant points at it. What lands is therefore
  **operator-owned**: shipped standards and the four shipped location types seed **`official: false`** through
  **seed-if-absent** paths (`SeedStandard` / `SeedLocationType`, `ON CONFLICT DO NOTHING`), never the
  authoritative `Upsert*`, whose `ON CONFLICT DO UPDATE` would silently revert an operator's edit on the next
  boot. Forking applies **template -> standard**, never **standard -> system**: a system **conforms** with
  **live** inheritance. The **canonical catalogs are the exception** and keep the authoritative upsert with
  `official: true`, `property` above all, since it is the shared vocabulary a driver maps onto and a release must
  be able to correct it. Proven by the `standard`, `standard_property`, and `location_type_property` storage
  integration tests, the owner-generic resolver tests across all four owner kinds (including the classifier-less
  node and one-off system), the seed regression test that **edits a seeded standard, re-runs the seed, and
  asserts the edit survived**, the `/standards` and four property-route HTTP e2e suites (the official read-only
  422, the unknown-property 422, the out-of-scope non-disclosing 404 on both the read and the write, the
  withdraw-twice 404, and the clear back to the contract default), and the console suites for the new surfaces.
  This is **PR6** of the estate-model shift
  ([ADR-0048](/architecture/decisions/#adr-0048-the-standard-blueprint-and-the-template-fork-seed-model)). No page
  badge moves: [core entities](/architecture/core-entities/) stays `Partial` (the cross-owner cascade, the
  non-`declared` provenance producers, `system_member` composition, and template pinning are still ahead), and
  [API](/architecture/api/) stays `Partial`.
