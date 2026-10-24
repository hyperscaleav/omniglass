---
title: "The interface's registry field is renamed `type` to `interface_type`"
date: 2026-07-23
refs: []
form: bullet
---

- **The interface's registry field is renamed `type` to `interface_type`** (with `interface_type_id`),
  matching `location_type` and `secret_type` so the interface reference reads uniformly and can join the
  guard's reverse set (a bare `type` could not, since it also names an RFC-9457 error type and a secret
  field's data type). The create flag becomes `--interface-type`. The diagnostic reachability row keeps its
  `interface_type` name-only (a display value never fed back to a write), recorded as an explicit
  reverse-check exemption.
