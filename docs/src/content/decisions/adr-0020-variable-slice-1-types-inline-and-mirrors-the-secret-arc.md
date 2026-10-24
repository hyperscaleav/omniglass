---
title: "`variable` slice 1 types inline and mirrors the secret arc"
date: 2026-07-11
status: "Accepted"
summary: "The variable member ships plaintext, typed inline against a `value_type` enum (no `variable_type` registry), on the secret owner arc; template scope, groups, the `$var:` consumer deferred"
---

- **Date:** 2026-07-11 | **Status:** Accepted | **Pages:** [config, secrets, and variables](/architecture/variables/)
- **Decision:** The **variable** member of the trio ships its first slice: a typed, cascade-resolved **plaintext**
  value owned on the exclusive arc and resolved most-specific-wins down the [cascade](/architecture/cascade/), with a
  Variables directory and a per-component effective-variables panel, mirroring the [secret](#adr-0017-credential-is-renamed-secret-the-cascade-is-the-reuse-mechanism)
  member minus crypto, masking, and the reveal. `variable:create,update` is granted to **operators** (delete stays
  admin and owner), the same split secret got. Three parts of the written design are deferred to keep the slice one
  vertical cut. First, **typing is inline**: a `value_type` enum (`string | int | float | bool | json`) on the row
  plus a jsonb `value` validated against it in a pure `internal/variable` package, **not** a `variable_type` shape
  registry. A scalar needs no governed vocabulary, and the page itself calls variables the "operator-defined, not
  curated" member, so a registry would contradict the model. Second, the **`template` owner scope** (the design's
  `global -> template -> instance`) is out: slice 1 mirrors the secret arc (`global | location | system | component`),
  and template scope plus cascade groups land together in [#184](https://github.com/hyperscaleav/omniglass/issues/184),
  because they touch the shared resolver once for both members. Third, the **`$var:` consumer** and the
  **secret-flagged** variable are deferred (the consumer has no live interpolation site yet, as with `$sec:`).
- **Context:** The written [variables](/architecture/variables/) page sketched a `variable_type` registry and a
  shared config/variable cell carrying `observed_value` and `reconcile`. Building the member showed those belong to
  **config** (the declared-vs-observed member), not the free macro: a variable has no observed side. So `variable`
  shipped as its own single table, typed inline, and the page's Storage section is corrected to match. This diverges
  from the page's `variable_type`-registry and shared-cell sketch; the `config` member stays `Design`.
- **Closes:** issue [#183](https://github.com/hyperscaleav/omniglass/issues/183) (variable slice 1).
