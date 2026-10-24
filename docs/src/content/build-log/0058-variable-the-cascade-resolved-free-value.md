---
title: "Variable: the cascade-resolved free value"
date: 2026-07-12
refs: []
form: bullet
---

- **Variable: the cascade-resolved free value.** The second member of the [config, secrets, and
  variables](/architecture/variables/) trio lands, after the secret. A **variable** is a typed **plaintext**
  value (a macro) owned on the same exclusive arc as a secret (`platform | location | system | component`) and
  resolved most-specific-wins down the [cascade](/architecture/cascade/), but shown in the clear (no crypto, no
  masking, no reveal). Typing is **inline**: a `value_type` of `string` / `int` / `float` / `bool` / `json` plus a
  jsonb `value`, validated against the type in a pure `internal/variable` package (no shape registry, unlike the
  secret). `GET|POST /variables`, `PATCH|DELETE /variables/{id}`, and `GET /components/{name}/effective-variables`
  (the per-component cascade view, **since retired**, [#281](https://github.com/hyperscaleav/omniglass/issues/281)).
  Reads ride the viewer floor; **create and update are granted to operators**
  (`variable:create,update`), delete stays `variable:delete` (admin, owner), the same split the secret got.
  Scoping a variable required wiring `variable` into the ABAC resolver's owner-arc tiers, exactly as the secret
  did. The console adds a **Variables** directory (type in its own column) with a type-aware value editor; a
  per-component **effective-variables** cascade panel also shipped and was **later retired**
  ([#281](https://github.com/hyperscaleav/omniglass/issues/281), the panel-retirement note below). Deferred
  (ADR-0020): the **`template`** owner scope and cascade groups ([#184](https://github.com/hyperscaleav/omniglass/issues/184)),
  a `variable_type` registry (types are inline), the **`$var:`** interpolation consumer, and the secret-flagged
  variable.
  Proven by a `value_type` validation unit test (each scalar's valid and invalid forms), a storage integration test
  (jsonb round-trip per type, full cascade precedence, the owner-scope split), an HTTP e2e (the cascade, the authz
  split incl. the scoped-operator create / update and the viewer 403s), a scope-resolver unit test for the
  owner-arc kinds, and the web data-layer suite.
