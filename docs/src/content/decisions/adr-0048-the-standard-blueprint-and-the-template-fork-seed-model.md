---
title: "The `standard` blueprint and the template-fork seed model"
date: 2026-07-21
status: "Accepted"
summary: "`system_type` is promoted to **`standard`**, the blueprint a system conforms to and the system-side counterpart of `product`: it gains `parent_standard_id` (variants), a declared-property contract, and its own `standard:*` Catalog resource, and `system.standard_id` becomes **optional**. `standard_property` and `location_type_property` join `product_property`, and one **owner-generic** `EffectiveProperties(ownerKind, ownerID)` resolves component, system, location, and node off a single parameterized template. A standard and a location type are created by **forking an in-code template** (one-time, no inheritance), so a shipped row is **operator-owned** (`official: false`, seeded **if absent**), while a system **conforms** to its standard with **live** inheritance; only the canonical catalogs keep the authoritative upsert. PR6 of the estate-model shift"
---

- **Date:** 2026-07-21 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [API](/architecture/api/), [identity and access](/architecture/identity-access/),
  [storage](/architecture/storage/), [Standards guide](/guides/admin/standards/),
  [Types guide](/guides/admin/types/), [Properties guide](/guides/admin/properties/)
- **Decision:** Three moves land together, because each one only makes sense with the others.

  **1. `system_type` is promoted to `standard`.** A **standard** is the **blueprint a system conforms to**
  (huddle room, classroom, auditorium): the system-side counterpart of
  [`product`](#adr-0045-the-product-catalog), not a label hung off a system. The table is renamed and gains
  **`parent_standard_id`** (a variant points at its base, mirroring `product.parent_product_id`) and a
  declared-property **contract**. `system.system_type` becomes **`system.standard_id`** and is now
  **optional**, exactly like `component.product_id`: a **one-off system that conforms to no standard is
  first-class** and carries only its own ad-hoc values. The seeded rows carry over unchanged. Because a
  standard now owns a contract, it leaves the shared `type:*` registry permission and takes its own
  **`standard:read` / `:create` / `:update` / `:delete`** Catalog resource (read on the viewer `*:read` floor,
  the writes at the admin tier, exactly like `product:*`), and its routes move from `/types/system` to
  **`/standards`**.

  **2. Two more contract tables, and one owner-generic resolver.** **`standard_property`** and
  **`location_type_property`** join `product_property` on the identical shape (`<classifier>_id`,
  `property_name`, an optional `default_value`, a `required` flag, unique per pair). `data_type` and
  `validation` are **never** duplicated onto a contract; they stay in the [`property`
  catalog](#adr-0043-the-property-catalog). The resolver then generalizes:
  **`EffectiveProperties(ctx, ownerKind, ownerID, read)`** resolves **component, system, location, and node**
  off **one** parameterized SQL template driven by an **`ownerContract`** table (instance table, classifier
  column, contract table, contract key, arc column). Component reads its contract through
  `component.product_id`, system through `system.standard_id`, location through `location.location_type`; a
  **node has no classifier**, so it resolves ad-hoc values only. The query shape is unchanged from
  [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value): a contract arm
  (`coalesce(the instance's value, the contract default)`, `from_contract=true`) UNION an ad-hoc arm. This is
  the primitive-first move: three classifier/instance pairs, one resolver, so they cannot drift. Alongside it,
  `guardOwnerScope` now scope-checks **every** owner arc on a value write (it previously returned nil for
  everything but the component arc), so an out-of-scope system or location is a non-disclosing 404 on the
  write path as well as the read.

  **3. The seed model: templates live in code, not the database.** A standard (and a location type) is
  created by **forking an in-code template**. The fork is **one-time, with no inheritance**, so nothing in any
  tenant ever points back at a template and templates can be improved in any release. That dissolves the
  shipped-defaults-versus-local-edits problem at the root: the thing the vendor updates (the template) and the
  thing the operator owns (the row) are **never the same object**. Four consequences follow.

  - **Forking applies to template -> standard, not standard -> system.** A system does not fork its standard,
    it **conforms** to it, with **live inheritance**: the standard's contract default resolves for every
    conforming system until that system overrides it, and revising the default moves every system that has
    not.
  - **Therefore a shipped standard or location type is operator-owned, not official.** Both seed with
    **`official: false`** through **seed-if-absent** paths (`SeedStandard` / `SeedLocationType`, `ON CONFLICT
    DO NOTHING`), never the authoritative `Upsert*`. They are freely editable and deletable from the moment
    they land.
  - **An authoritative upsert here would be a bug, not a policy.** `ON CONFLICT DO UPDATE` would silently
    revert an operator's edit on the next boot, which is the exact failure this model avoids. A regression
    test edits a seeded standard, re-runs the seed, and asserts the edit survived.
  - **The canonical catalogs are the exception** and keep the authoritative upsert with `official: true`:
    **`property`** (and later `command` and `event_type`) is the **shared vocabulary a driver maps onto**, so
    a release must be able to correct it. The classification catalogs (`vendor`, `driver`, `capability`,
    `product`, `interface_type`, `secret_type`) and `role` stay on that same authoritative path for now.

  Four route groups carry the contracts and the values, all regenerated into the OpenAPI document, the cobra
  CLI, and the typed client: `GET /standards/{id}/properties` plus
  `PUT` / `DELETE /standards/{id}/properties/{property}` (gated `standard:read` / `:update` / `:delete`);
  `GET /location-types/{id}/properties` plus `PUT` / `DELETE .../{property}` (gated `type:*`, since the
  location type registry is still a `type` registry); and the value sides
  `GET /systems/{name}/properties` plus `PUT` / `DELETE .../{property}` (gated `system:read` / `:update`) and
  the same for `/locations/{name}/properties` (gated `location:read` / `:update`). The value routes are
  **scope-injected**, so an out-of-scope system or location is a non-disclosing **404**.
- **Context:** [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value) deferred
  `standard_property` and `location_type_property` because neither owner was ready: `system_type` was a bare
  label registry with no contract to hang anything on. Building that contract forced the promotion, since a
  registry that declares what its instances expose **is** a blueprint, and a blueprint is the system-side
  `product`. Making `standard_id` optional followed immediately: a productless component was already
  first-class, and a system that matches no blueprint has the same claim. The seed question surfaced during
  the build and is the harder half. Shipping a room standard as an authoritative `official` row would make it
  **read-only** (an operator could not tune "Huddle Room" to their own estate) and would **revert local edits
  on every boot** if it were writable. Both failures come from one mistake: treating example content and
  canonical vocabulary as the same kind of thing. Splitting them (a template in code that is forked once,
  versus a catalog row that is upserted authoritatively) lets the release improve its examples forever without
  ever touching an estate's data, and keeps the one thing that genuinely must stay identical install to
  install, the property vocabulary, under release control.
- **Deferred:** the **in-code template mechanism** itself and its create-from-template console affordance
  ([#317](https://github.com/hyperscaleav/omniglass/issues/317)); this slice ships the seed-if-absent behavior
  and the operator-owned rows that the mechanism will produce, with the shipped starter set still declared as
  seed YAML. The **official / community / private catalog tiering** for `product` / `driver` / `property` /
  `event_type` plus a **disable flag** ([#318](https://github.com/hyperscaleav/omniglass/issues/318)). Also
  still deferred: a standard's **role set** and health composition, the non-`declared` provenance producers,
  the cross-owner cascade over `property_value`, and the `datapoint` -> `sample` rename.
- **Supersedes:** the **`system_type`-as-label** notion (a system's blueprint is a first-class Catalog entity
  with its own contract and its own permission, and it is optional), completing on the system side what
  [ADR-0045](#adr-0045-the-product-catalog) and
  [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value) did on the component side. Also
  supersedes the assumption running through [ADR-0044](#adr-0044-the-component-classification-catalogs),
  [ADR-0045](#adr-0045-the-product-catalog), and
  [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value) that **everything the seed ships
  is `official` and read-only**. That now holds **only for the canonical catalogs**: the shipped standards and
  the four shipped location types (`campus` / `building` / `floor` / `room`) are `official: false` and fully
  editable, so any prose promising a read-only seed-owned row for those two registries is stale.
- **Tracked under** epic [#266](https://github.com/hyperscaleav/omniglass/issues/266). This is **PR6** of the
  estate-model shift toward property / event / command plus vendor / product / driver / capability / standard /
  role / health.
