---
title: "Config, secrets, and variables: the per-component effective-secrets and effective-variables panels retire"
date: 2026-07-19
refs: []
form: bullet
---

- **Config, secrets, and variables: the per-component effective-secrets and effective-variables panels retire.**
  The standalone **Effective secrets** and **Effective variables** panels on the component detail, and their
  `GET /components/{name}/effective-secrets` / `GET /components/{name}/effective-variables` routes (with the
  generated `omniglass effective-secret list` / `effective-variable list` commands and the matching typed-client
  methods), are **removed** ([#281](https://github.com/hyperscaleav/omniglass/issues/281), under the
  [field](/architecture/variables/#property-one-typed-name-a-product-contract-a-stored-value) epic
  [#266](https://github.com/hyperscaleav/omniglass/issues/266)). The panels listed **every** cascade-resolving cell
  that reached a component, mostly inherited noise; the **field** primitive is the schema-over-cells consumer, so a
  component's values are now its **fields** (override versus type-default, the **Effective fields** panel), and a
  secret or variable reaches a component by being **sourced into a field** (the deferred field `sources` model) or
  **bound to a collection interface input**, not through a per-component cascade-browse panel. **Kept:** the storage
  cascade **resolvers** (`ResolveSecrets` / `ResolveVariables`) as the internal primitive the future `$sec:` /
  `$var:` interpolation consumer will call, and the **Secrets** and **Variables** directories (browse, create,
  edit, reveal) with all their routes and CLI. The [config, secrets, and variables](/architecture/variables/) page
  drops the retired-panel claims and the [decision log](/architecture/decisions/) records the call. No schema
  change: the value cells, the resolvers, and the directories are all unchanged.
