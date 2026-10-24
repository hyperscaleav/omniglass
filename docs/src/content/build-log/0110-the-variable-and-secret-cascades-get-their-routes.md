---
title: "The variable and secret cascades get their routes"
date: 2026-07-22
refs: []
form: bullet
---

- **The variable and secret cascades get their routes.** `GET /components/{name}/effective-variables`
  and `/effective-secrets` resolve the same cascade the tag route resolves, returning the winner **and**
  the shadowed candidates with the tier each came from. The gateway resolvers had been written and
  tested since the cascade slice and were **unreachable**: no route, so no CLI command and no console
  read, while the CLI guide taught `omniglass effective-secret list` as though it worked
  ([#359](https://github.com/hyperscaleav/omniglass/issues/359), found by the docs-command guard).
  The two are gated differently on purpose: a variable read rides `variable:read`, a secret read rides
  **`secret:read`**, which the viewer floor deliberately does not carry, and returns **masked** fields.
  The effective read answers which secret applies and where it comes from, never what it contains;
  plaintext stays behind the audited reveal, proven by a test that fails if the seeded plaintext ever
  appears. The CLI commands generated with no generator change (`component effective-variable list`,
  `component effective-secret list`), which is the parent-qualified naming rule paying off. **Thin cut:**
  the console's resolution panel still shows tags only ([#365](https://github.com/hyperscaleav/omniglass/issues/365)).
