---
title: "Root exclusion lives on the grant, not a new scope kind"
date: 2026-07-06
status: "Superseded by [ADR-0011](#adr-0011-grant-scope-is-an-operator-not-a-boolean-modifier)"
summary: "The deploy \"act on the subtree but not the root\" capability is an `exclude_root` grant modifier, not a new scope kind"
---

- **Date:** 2026-07-06 | **Status:** Accepted | **Pages:** [identity and access](/architecture/identity-access/)
- **Decision:** The "act on the subtree but not the root" capability (the deploy / integrator case, issue
  [#87](https://github.com/hyperscaleav/omniglass/issues/87)) is a boolean `exclude_root` modifier on
  `principal_grant`, not a new `scope_kind` (e.g. `location_descendants`) and not a role-level flag. It narrows
  only the **modify** actions (update, delete) to the root's descendants; read and create-placement keep the
  root. An inclusive grant on the same root wins over an excluding one.
- **Context:** A new scope_kind would fork the kind handling three ways (location / system / component) and
  grow the scope vocabulary; a role-level flag could not vary per grant (the same deploy role granted
  root-inclusive in one place and root-excluded in another). The grant modifier composes with the
  additive-grant model and confines the change to one predicate (`inScopeTree`) shared by all three tree
  entities. Keeping read and create-placement inclusive means a `PATCH` on the root is the existing
  readable-but-out-of-write-scope 403, so `exclude_root` reuses the three-way status split rather than adding a
  fourth case. Shipped with a new `deploy` official role (create + update on the three tree tiers, read via the
  viewer floor). The grant-builder toggle to set it from the console is a fast-follow ([#99](https://github.com/hyperscaleav/omniglass/issues/99)).
- **Closes the gap:** issue [#87](https://github.com/hyperscaleav/omniglass/issues/87).
