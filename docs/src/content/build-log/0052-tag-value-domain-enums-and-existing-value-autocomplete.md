---
title: "Tag: value-domain enums and existing-value autocomplete"
date: 2026-07-13
refs: []
form: bullet
---

- **Tag: value-domain enums and existing-value autocomplete.** A tag key can now **constrain its values to an enum**,
  answering the enum half of the value-domain open question. A new **`allowed_values`** column on `tag` (empty = free
  text) is the value set a bound value must belong to; `SetTagBinding` enforces it (a non-member is a dedicated 422,
  `ErrTagValueNotAllowed`), so `environment` can be declared as one of `prod` / `staging` / `dev`. The Tags directory
  create and edit forms gain a value-domain control (a checkbox that turns a key into an enum plus a value-list
  editor), and the **TagAdder value stage** renders a **strict dropdown** for an enum key. A **free** key instead
  autocompletes the **distinct values already in use** for it, via a new `GET /tags/{name}:values` read
  (`select distinct value`), so an operator reaches for an existing value without the key declaring a set. Pure
  `web/src/lib/tagdraft` helpers (`isEnumKey`, `valueOptions`, `valueAllowed`) and a pure `internal/tag`
  (`ValidateAllowedValues`, `ValueAllowed`) hold the logic. Proven by unit suites on both pure cores, a storage
  integration test (the enum admits members and rejects non-members, a free key admits anything, narrowing the enum is
  enforced on the next bind, the distinct-values query dedupes and sorts) and an HTTP e2e (the enum 422, the values
  endpoint, the round-trip). A typed `value_type` and input normalization stay deferred ([ADR-0024](/architecture/decisions/#adr-0024-a-tag-key-may-constrain-its-values-to-an-enum)).
