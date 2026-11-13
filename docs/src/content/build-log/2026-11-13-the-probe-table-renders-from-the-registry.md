---
title: "The collection probe table renders from the transport registry (#1087)"
date: 2026-11-13
refs: [1087]
---

The collection page's per-transport probe table (rung, `endpoint.params`, and the samples a probe
lands) was hand-written, and it had already drifted: the snmp row was missing, and the ssh probe's
`endpoint-authenticated` was never listed. Each dialed transport's registry entry now declares its
`Params` and the `ProbeKeys` its probe can land, the generator carries them into `seed.json`, and the
page embeds `<SeededSet kind="transports" of="probes" />` in place of the table. A test in
`internal/node` drives every built transport's probe through the node's dispatch against a passing
and a refused fake, and fails when a probe lands a key its entry omits or an entry declares one no
run lands; a transport with no rig fails too. No probe's behaviour or keys changed.
