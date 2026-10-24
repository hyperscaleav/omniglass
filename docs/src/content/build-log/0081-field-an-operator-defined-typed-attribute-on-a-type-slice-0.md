---
title: "Field: an operator-defined typed attribute on a type (slice 0)"
date: 2026-07-17
refs: []
form: bullet
---

- **Field: an operator-defined typed attribute on a type (slice 0).** The first cut of the
  [field](/architecture/variables/#property-one-typed-name-a-product-contract-a-stored-value) primitive: an operator declares a typed
  field on a `component_type` (a `field_definition`: a `name`, a `data_type` of `string`/`int`/`float`/`bool`/`json`, and an
  optional type-level default validated against the `data_type`, unique per `(component_type, name)`), a component sets a
  **literal** for a field defined on its type (a `field_value`), and the component's **effective** value resolves to the set
  literal or the type default (an `is_set` flag marks the override). The whole vertical shipped: storage (transactional,
  audited), the API (the definition catalog flat and `field:<action>`-gated, the value routes ABAC-scoped to the owning
  component), the generated CLI and typed client, and the UI (define on the component-type blade under
  [Types](/guides/admin/types/), plus an **Effective fields** panel on the component detail that sets a literal and shows
  override-versus-default). Owner is the **component only**: macro interpolation (`$var:`/`$sec:`/`$datapoint:` in a value),
  the cross-type cascade (`product → location → system → component`, deepest-wins), the `sources` model, typed `file` fields,
  and definitions on the non-`component_type` owners are deferred to later slices; the UI cannot yet clear a set value back to
  the type default (the clear route exists on the API and CLI but is not wired into the panel). The [config, secrets, and
  variables](/architecture/variables/) page moves to `Partial` for the field member. Proven by a storage integration test
  (definition CRUD and the default type-match guard on create and update, the effective set-or-default read and the
  not-applicable guard, and value update/delete reverting the component to the default), an HTTP e2e (the effective read, the
  override set path, and the viewer-reads / operator-sets authz split at the `field:create` gate), and a scope-resolver unit
  test for the field arc kinds (a component-scoped operator resolves a subtree scope, a viewer resolves empty).
