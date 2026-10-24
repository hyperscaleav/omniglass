---
title: "Secret: the cascade-resolved encrypted value"
date: 2026-07-10
refs: []
form: bullet
---

- **Secret: the cascade-resolved encrypted value.** The first of the [config, credentials, and
  variables](/architecture/variables/) trio lands, and the prerequisite for the [collection](/architecture/collection/)
  driver's interface inputs ([#155](https://github.com/hyperscaleav/omniglass/issues/155)). A **secret** is a
  typed, encrypted-at-rest value owned on the exclusive arc (`platform | location | system | component`) and
  resolved most-specific-wins down the [cascade](/architecture/cascade/). Its shape is a `secret_type` registry
  (per-field secrecy and origin; `snmp-community` and `basic-auth` seeded); crypto is **envelope AES-256-GCM**
  behind a pluggable KEK provider (env / file / fallback), with `(owner, name, field)` bound as AAD so a
  ciphertext cannot be lifted between rows. The load-bearing new piece is the **cascade resolver**: it walks the
  three owner trees up from a component, tags each owner with a band and depth, and ranks per name (highest tier,
  then deepest), returning the winner and the shadowed candidates. `GET /types/secret`, `/secrets` (all-scope
  list, create, update, delete), `GET /components/{name}/effective-secrets` (the masked cascade view, **since
  retired**, [#281](https://github.com/hyperscaleav/omniglass/issues/281)), and the
  audited decrypts `POST /secrets/{id}:reveal` and `:copy` (a clipboard copy, recorded under a distinct `copy`
  verb). Masked reads ride the viewer floor; **create and update** are gated by `secret:create` / `secret:update`
  and granted to **operators** in their scope; **delete** stays `secret:delete` (admin, owner); **reveal** and
  **copy** by the sensitive `secret:reveal`, which the `*:read` floor does not carry, so only admin (`secret:*`)
  and owner may decrypt. Scoping a secret required wiring `secret` into the ABAC resolver's owner-arc tiers
  (location / system / component), so a scoped grant now confers secret scope over its subtree (before, only an
  all-scope owner could). The console adds a **Secrets** directory (type in its own column) with per-field
  in-place reveal + copy adornments; a per-component **effective-secrets** list also shipped (each resolved
  secret opening a blade that decrypts the value and shows the full cascade top-to-bottom) and was **later
  retired** ([#281](https://github.com/hyperscaleav/omniglass/issues/281), the panel-retirement note below).
  Renamed **credential to secret** (ADR-0017).
  Proven by a real-crypto testcontainer test (seal, jsonb round-trip, unseal; encrypted-at-rest; full cascade
  precedence), an HTTP e2e (the cascade, the authz split incl. the scoped-operator create / update, and the
  reveal / copy round-trips plus their 403s), a scope-resolver unit test for the owner-arc kinds, and the web
  data-layer suite. Deferred: the **variable** and **config** members of the page, the interpolation consumer,
  secret **lifecycle** (oauth2 refresh, rotation), and groups / weighted precedence in the cascade.
