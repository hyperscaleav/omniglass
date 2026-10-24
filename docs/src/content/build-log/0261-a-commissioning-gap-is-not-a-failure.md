---
title: "A commissioning gap is not a failure"
date: 2026-08-19
refs: []
form: bullet
---

- **A commissioning gap is not a failure**
  ([#631](https://github.com/hyperscaleav/omniglass/issues/631)). The verdict domain gains a
  fourth value: a role short because its hardware was never installed reads `incomplete`, ranked
  between healthy and degraded, and its declared `impact` describes failure only. Without it the
  fleet canvas would paint a half-commissioned fleet entirely red, which is the single thing that
  would make the medium useless on the fleets it serves. A pure enum insertion in
  `internal/health` plus the seeded property enum; the console's badge gained the fourth look and
  `--og-incomplete` in both themes ([ADR-0122](/architecture/decisions/)).
