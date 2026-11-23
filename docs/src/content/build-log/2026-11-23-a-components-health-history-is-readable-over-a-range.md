---
title: "A component's health history is readable over a time range (#2305)"
date: 2026-11-23
refs: [2305, 1826]
---

The component's health was recorded (`recordHealth("component", ...)`) but only the roll-ups
could be read, and the system read was fixed at the last 30 days. `GET /components/{name}/health`
now returns the component's own verdict and its recorded transitions in the same envelope
`GET /systems/{name}/health` serves (slots and systems empty), gated by `component:read` with the
Gateway injecting scope and an out-of-scope name a non-disclosing 404.

The component, system and location health reads all take `since` and `until` (RFC 3339), so the
range picker (#2304) can drive them. With neither, the window is the last 30 days measured on the
database's clock as before; `since` after `until` is a 400. The Gateway's `window` argument became
a `HealthRange`, and the generated CLI verb (`omniglass component health list`), the typed client
and the MCP catalog came from `make gen`.
