---
title: "A number input takes a number (#901)"
date: 2026-11-12
refs: [901, 1187]
---

A driver input declared `kind: number` now takes a JSON number on endpoint create and update:
`"port": 10161` attaches `snmp-generic` where it was a 422 `expected string`. The body's inputs are a
map of string-or-number scalars (a `oneOf` in the generated OpenAPI, so the typed client and the MCP
tool schemas say so too), and a number is stored as its decimal string (`"10161"`), so
`endpoint.inputs` and the driver's number check are unchanged. A string still works. A boolean, object
or array is a 422 naming the input. The collection operator guide's attach example shows a numeric
port.
