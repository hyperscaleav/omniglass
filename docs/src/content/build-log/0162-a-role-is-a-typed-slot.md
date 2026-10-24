---
title: "A role is a typed slot"
date: 2026-08-08
refs: [626]
form: bullet
---

- **A role is a typed slot** ([#626](https://github.com/hyperscaleav/omniglass/issues/626),
  `c006c62`). The role assignment guard swaps its capability comparison for a typed-slot one: a
  component fills a role only when its product's `component_type` falls within the subtree of a
  type the role accepts (any type, if none are declared), and, if the role pins specific
  products, only when its product is one of them. Two new join tables,
  `system_role_type` and `system_role_product`, carry the accepted set and the pin; the refusal
  names both parties in operator vocabulary rather than a bare capability gap. The capability
  registry itself and the health rollup's alarm-impact reading are untouched here, staged for
  retirement in the next slice.
