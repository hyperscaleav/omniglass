---
title: "The fields fold: `product_property` and `property_value`"
date: 2026-07-21
status: "Accepted; superseded in part by [ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus) (the `component_type` registry returns, above the product)"
summary: "The standalone **fields** feature retires and folds into the estate model: a field was only ever a **property with `declared` provenance**, never a primitive of its own. **`product_property`** is the product's declared-property **contract** (`product_id`, `property_name`, `default_value`, `required`), replacing `field_definition`; **`property_value`** is the value store, carrying the **same owner exclusive-arc** as `metric_datapoint` / `event` plus `instance` and `provenance`, replacing `field_value`. `EffectiveProperties` unions the contract arm (`coalesce(set value, contract default)`) with the off-contract arm, so a productless component still resolves. `field_definition`, `field_value`, `component.component_type`, and the whole `component_type` registry retire. PR5 of the estate-model shift"
---

- **Date:** 2026-07-21 | **Status:** Accepted; superseded in part by
  [ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus) (the
  `component_type` registry returns, reshaped above the product) | **Pages:** [core entities](/architecture/core-entities/),
  [config, secrets, and variables](/architecture/variables/), [API](/architecture/api/),
  [Properties guide](/guides/admin/properties/), [Products guide](/guides/admin/products/)
- **Decision:** The standalone **fields** feature is **retired** and folded into the estate model, because a
  **field was never a primitive**: it was a **property with `declared` provenance**, and the same is true of
  **config** (a property with `intended` provenance). Two tables replace it. **`product_property`** is the
  product's declared-property **contract**: `(product_id -> product, property_name -> property,
  default_value jsonb, required bool)`, unique per `(product, property)`, so what a product's instances expose
  is data on the SKU rather than a catalog hung off a genus type. `data_type` and `validation` are
  **not duplicated** here; they stay in the [`property` catalog](#adr-0043-the-property-catalog), the single
  source. **`property_value`** is the value store: it carries the **same owner exclusive-arc** as
  `metric_datapoint` and [`event`](#adr-0046-the-event-log-kind-sink) (`owner_kind` plus
  `component_id` / `system_id` / `location_id` / `node_id`, one-set CHECK), plus `property_name`, an
  `instance` discriminator, a **`provenance`** (`observed` / `calculated` / `intended` / `declared`, default
  `declared`), and a jsonb `value`. Its series key is `unique nulls not distinct`, since the arc leaves three
  owner columns NULL and Postgres's default NULLS DISTINCT would let duplicate rows through. This slice writes
  only `owner_kind=component` with `provenance=declared`; the rest of the arc and the other three provenances
  are the seats later producers sit in. The resolver is **`EffectiveProperties(component, scope)`**, one SQL
  UNION of two arms: the **contract arm** (every `product_property` of the component's product, value =
  `coalesce(the component's declared value, the contract default)`, `from_contract=true`) and the **ad-hoc
  arm** (declared values the contract does not declare, `from_contract=false`), so a **productless component
  still resolves**, to its ad-hoc set alone. Six routes carry it: `GET /products/{id}/properties` and
  `PUT` / `DELETE /products/{id}/properties/{property}` (gated `product:read` / `:update` / `:delete`, an
  official product read-only 422), and `GET /components/{name}/properties` plus
  `PUT` / `DELETE /components/{name}/properties/{property}` (gated `component:read` / `:update`, ABAC-scoped
  with a non-disclosing 404 out of scope, audited). The console renames the operator word from **Fields** to
  **Properties**: a **Properties** panel on the component detail (contract rows, plus a dashed-bordered
  **off contract** group for the ad-hoc ones, an override toggle with an accent dot, a required property
  blocking Save) and a **Declared properties** contract editor on the product detail (declare, edit, withdraw,
  read-only for an official product). Retired with the feature: **`field_value`**, **`field_definition`**,
  **`component.component_type`**, and the **`component_type`** table itself with its routes
  (`/types/component`), its console registry section, and its seed. A component's shape now comes from its
  **product** ([ADR-0045](#adr-0045-the-product-catalog)), still optional: a productless component simply has
  no contract, and the category `component_type` used to carry (display, codec) is expressed by the
  **capabilities** that product provides. The seeded products ship a starter contract (`cisco-room-bar` and
  `samsung-qm55` declare `serial_number`, `firmware_version`, and `model_number` with defaults), and
  `roles.yaml` drops the now-unclaimed `field:*` permissions, since `property:*` already covers the tier.
- **Context:** [ADR-0043](#adr-0043-the-property-catalog) made the catalog primitive-agnostic and deferred the
  one binding it needed, `field_definition.key`, so a field could draw its type from the catalog. Building
  that binding forced the realization that the binding was the wrong shape: once a field's name, type, and
  validation all come from a property, a "field" is a property the operator **declares** rather than the
  device **observes**, and the only thing left that was field-specific was **where the schema hangs**. The
  answer was already on the table: [ADR-0045](#adr-0045-the-product-catalog) made the **product** the source
  of a component's shape, so the per-type field catalog becomes the per-product **contract** over the property
  catalog, and the field value becomes an arc-owned, provenance-tagged **property value** beside the samples
  and occurrences it sits next to. Folding is primitive-first: one value store the cascade, reconciliation,
  and the current-value read can all be built on, rather than three parallel ones (`field_value` for declared,
  the datapoint tables for observed, an unbuilt `config` table for intended).
- **Deferred:** **`standard_property`** and **`location_type_property`** (the other contract owners, each
  waiting on its owner entity, `standard` and a `location_type` schema); the **driver access/mode column** on
  `product_property` (whether a driver can get, set, or only declare a property, which lands with the driver
  slice); the **non-declared provenance producers** (`intended` config writing a desired value,
  `observed` materializing a current value out of the datapoint stream, `calculated` from a rule), which the
  provenance column seats but nothing writes yet; the multi-owner arc on `property_value` (only the component
  arm is written and scope-injected today); and the **`datapoint` -> `sample`** rename, still a later cleanup.
- **Supersedes:** [ADR-0043](#adr-0043-the-property-catalog)'s deferred **`field_definition.key`** property
  binding. This **is** that binding, done differently: rather than a field definition gaining a key reference,
  the field catalog itself became the **product contract over the property catalog**, and `field_definition`
  retires. Also completes [ADR-0045](#adr-0045-the-product-catalog)'s partial supersession of the
  **`component_type`-as-shape** notion: that ADR repointed shape at the product but left the table standing;
  this one drops `component.component_type` and the `component_type` registry outright.
- **Tracked under** epic [#266](https://github.com/hyperscaleav/omniglass/issues/266). This is **PR5** of the
  estate-model shift toward property / event / command plus vendor / product / driver / capability / standard /
  role / health.
- **Amended by [ADR-0057](#adr-0057-the-cascades-least-specific-tier-is-platform-and-a-default-is-not-a-tier):**
  the contract default is unchanged, its vocabulary is. `coalesce(the instance's set value, the contract
  default)` is the fall-through to a **declaration**, not the bottom rung of a cascade, so
  `product_property.default_value` (and its two siblings) is the shipped instance of the off-axis default
  rather than a tier under `platform`.
- **Partially reversed by [ADR-0085](#adr-0085-the-component_type-registry-returns-as-the-device-class-genus):**
  this ADR's retirement of `component.component_type` and the `component_type` registry stands; naming
  and rendering (a generated component name needs a device-class stem the product's SKU cannot supply)
  forced the registry's return, deliberately reshaped: **above the product**
  (`product.component_type_id`), not beside the component and not a second classifier the component
  itself carries. What this ADR actually decided about **`product_property`** and **`property_value`**
  is untouched; only the "and the whole `component_type` registry retire" clause is reversed.
