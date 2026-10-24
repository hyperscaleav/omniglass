---
title: "retire the standalone effective-secrets and effective-variables per-component panels; fields become the component value surface"
date: 2026-07-16
status: "Accepted"
summary: "retire the standalone effective-secrets and effective-variables per-component panels; fields become the component value surface"
---

- **Date:** 2026-07-16 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/), [identity and access](/architecture/identity-access/), [API](/architecture/api/)
- **Decision:** The standalone per-component **Effective secrets** and **Effective variables** panels are removed,
  along with their `GET /components/{name}/effective-secrets` and `GET /components/{name}/effective-variables` routes
  (and the generated `omniglass effective-secret list` / `effective-variable list` commands and the matching
  typed-client methods). A component's value surface is the **field** primitive: a component's values are its
  **fields**, each resolving override-versus-type-default and shown in the **Effective fields** panel. A secret or a
  variable reaches a component by being **sourced into a field** (the deferred field `sources` model) or **bound to a
  collection interface input**, not through a per-component cascade-browse panel. **Kept** unchanged: the storage
  cascade **resolvers** (`ResolveSecrets` / `ResolveVariables`) as the internal primitive the future `$sec:` /
  `$var:` interpolation consumer will call, and the **Secrets** and **Variables** directories (browse, create, edit,
  reveal) with all their routes and CLI.
- **Context:** The per-component effective-* panels predated the field primitive and listed **every**
  cascade-resolving cell that reached a component, which at any real depth is mostly inherited noise (a global SNMP
  community, a location poll interval) rather than anything set on that component. The
  [field](/architecture/variables/#property-one-typed-name-a-product-contract-a-stored-value) primitive
  ([#266](https://github.com/hyperscaleav/omniglass/issues/266)) is the schema-over-cells consumer the design always
  intended: a component carries a typed set of fields, each resolving to a set literal or its type default, and the
  intended `sources` model lets a field draw its value from a variable, a secret, a datapoint, or a file. Once fields
  are the value surface, a second per-component cascade browser over the raw cells is redundant and misleading (it
  reads as though the cells attach to the component when they only resolve onto it). Retiring the panels narrows the
  component detail to its fields and keeps the cells' own management on the Secrets and Variables directories, where
  the cascade is authored. The resolvers stay because the interpolation consumer (`$sec:` / `$var:`) still needs
  them; only the browse-panel surface retires.
- **Closes:** issue [#281](https://github.com/hyperscaleav/omniglass/issues/281) (retire the per-component
  effective-secrets / effective-variables panels), under the field epic
  [#266](https://github.com/hyperscaleav/omniglass/issues/266).
