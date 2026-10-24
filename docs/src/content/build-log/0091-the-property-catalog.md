---
title: "The property catalog"
date: 2026-07-21
refs: []
form: bullet
---

- **The property catalog.** The `datapoint_type` catalog is generalized into the primitive-agnostic
  **`property`** catalog (the physical table; the concept, the `/properties` API, the Go `Property`, and the
  console all read `property`, while a property's identifier stays a `key`): the typed set of signals a
  datapoint **observes** and a field **declares**. The
  `(scope, name)` ladder collapses to a `name` primary key plus an **`official`** boolean (seed-owned
  properties read-only); `value_type` becomes **`data_type`** over `{string, int, float, bool, json}` (`text`
  backfills to `string`, `bool` added); **`kind`** is nullable (a declared-only attribute property has none);
  and **`validation`** is a **JSON Schema** enforced by Huma's own validator with **no new dependency**.
  Value and source tables keep keying by the **name string** (no FK), so the rename is behavior-preserving:
  the ingest registry, the reachability BFF, and the metric/state sinks are unchanged. A new
  **`internal/key`** primitive holds the one canonical name-format rule (lowercase, dot-hierarchied) and
  the typed value validator; tag's key rule folds onto it. The Storage Gateway gains custom-property CRUD
  (gated `property:create` / `:update` / `:delete`, official properties read-only, audited in the same transaction),
  exposed at **`/properties`** with a generated CLI and client, and a new **Catalog > Properties** console page (list,
  blade, create form). An official seed ships the reachability properties plus a starter attribute set
  (`serial_number`, `mac_address`, `firmware_version`, `model_number`). Proven by `internal/key` unit
  tests, the `property` CRUD integration test, the `/properties` HTTP e2e (the `property:create` 403 and official
  read-only), and the console suite. The type-schema (`field_definition.key`, PR-B) and reconciliation are
  deferred ([ADR-0043](/architecture/decisions/#adr-0043-the-property-catalog),
  [#297](https://github.com/hyperscaleav/omniglass/issues/297)). The
  [config, secrets, and variables](/architecture/variables/) page stays `Partial`.
