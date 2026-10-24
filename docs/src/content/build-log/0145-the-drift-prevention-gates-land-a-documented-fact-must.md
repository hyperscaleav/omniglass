---
title: "The drift-prevention gates land: a documented fact must exist ([#429](https://github.com/hyperscaleav/omniglass/issues/429))"
date: 2026-08-02
refs: [429]
form: bullet
---

- **The drift-prevention gates land: a documented fact must exist ([#429](https://github.com/hyperscaleav/omniglass/issues/429)).**
  Seven slices close the loop the 2026-07-30 audit opened. `make test` now fails when
  current-tense prose names a route absent from the OpenAPI document (methods checked,
  fence-aware, with an illustrative-marker escape), a permission that gates no route
  (both directions: doc mentions and seeded grants, matched with the real rbac rules
  minus the read floor, handler-enforced admin tiers registered against their call
  sites), a make target, env var, or repo path that does not exist (with the generated
  config registry counting as documentation by construction), or a `table.column` the
  generated schema facts do not carry (seeded dotted registry keys exempt). The CLI
  docs guard now validates documented flags against the cobra tree and walks the
  architecture and contributing trees too. The semantic drift no lint can see gets the
  periodic `/docs-audit` skill, encoding the audit's fan-out, drift definition, and
  issues-not-files landing rule.
