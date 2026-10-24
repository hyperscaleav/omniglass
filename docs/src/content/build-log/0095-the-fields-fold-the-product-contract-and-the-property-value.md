---
title: "The fields fold: the product contract and the property value store"
date: 2026-07-21
refs: []
form: bullet
---

- **The fields fold: the product contract and the property value store.** The standalone **fields** feature
  **retires**: a field was never a primitive, it was a **property with `declared` provenance**. Two tables take
  its place. **`product_property`** is the product's declared-property **contract** (`product_id`,
  `property_name`, an optional `default_value`, a `required` flag, unique per pair), replacing
  `field_definition` and its per-`component_type` catalog; `data_type` and `validation` are **not** duplicated,
  they stay on the [property catalog](/guides/admin/properties/). **`property_value`** is the value store,
  carrying the **same owner exclusive-arc** as `metric_datapoint` and `event` (`owner_kind` plus
  `component_id` / `system_id` / `location_id` / `node_id`, one-set CHECK) plus an `instance` discriminator, a
  **`provenance`** (`observed` / `calculated` / `intended` / `declared`, default `declared`), and a jsonb
  `value`; its series key is `unique nulls not distinct`, since the arc leaves three owner columns NULL and the
  default NULLS DISTINCT would let duplicates through. The resolver **`EffectiveProperties`** is one SQL UNION:
  the **contract arm** (every `product_property` of the component's product, valued
  `coalesce(the component's declared value, the contract default)`, `from_contract` true) plus the
  **off-contract arm** (declared values the contract does not declare), so a **productless** component still
  resolves, to its off-contract set alone. Six routes ship, regenerated into the OpenAPI document, the cobra
  CLI, and the typed SPA client: `GET /products/{id}/properties` and `PUT` / `DELETE
  /products/{id}/properties/{property}` (gated `product:read` / `:update` / `:delete`, an official product
  read-only 422), and `GET /components/{name}/properties` plus `PUT` / `DELETE
  /components/{name}/properties/{property}` (gated `component:read` / `:update`, ABAC-scoped with a
  non-disclosing 404 out of scope, audited in the same transaction). The CLI reads `omniglass product
  properties|set-property|delete-property` and `omniglass component properties|set-property|clear-property`.
  The console renames the operator word from **Fields** to **Properties**: the component detail gains a
  **Properties** panel (contract rows, a dashed-bordered **off contract** group for the ad-hoc ones, an
  override toggle with an accent dot on an override, a required property blocking Save), and the product detail
  gains a **Declared properties** contract editor (declare, edit, withdraw, read-only for an official product).
  Retired with the feature: `field_value`, `field_definition`, `component.component_type`, and the
  `component_type` table with its `/types/component` routes, its console registry section, and its seed. A
  component's shape now comes from its **product** (optional: a productless component simply has no contract),
  and the category `component_type` used to carry (display, codec) is expressed by the **capabilities** that
  product provides. The seeded products ship a starter contract (`cisco-room-bar` and `samsung-qm55` declare
  `serial_number`, `firmware_version`, and `model_number` with defaults), and `roles.yaml` drops the
  now-unclaimed `field:*` permissions, since `property:*` already covers the tier. Proven by the
  `product_property` and `property_value` storage integration tests (the in-place contract upsert, a nil
  default round-tripping as SQL NULL, both resolver arms, the idempotent re-set on one series row, the
  clear-to-default, and the productless component), the `/products/{id}/properties` and
  `/components/{name}/properties` HTTP e2e (the official-product 422, the unknown-property 422, the
  out-of-scope non-disclosing 404, the withdraw-twice 404, and the clear back to the contract default), and the
  console suites for both new surfaces. This is **PR5** of
  the estate-model shift ([ADR-0047](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)).
  No page badge moves: [core entities](/architecture/core-entities/) and
  [config, secrets, and variables](/architecture/variables/) both stay `Partial` (the cross-owner cascade, the
  non-`declared` provenance producers, and the `standard` / `location_type` contracts are still ahead).
