---
title: "The command wire actuates"
date: 2026-10-17
refs: []
form: bullet
---

- **The command wire actuates.** The subject the grammar reserved becomes real: a node pulls
  `og.v1.command.<node>` (request-reply mirroring the worklist, the same reply-inbox confinement
  and per-node grant) and receives the commands whose owning component has a driver-attached
  endpoint placed on it and whose driver binds the command's type, each with its request rendered
  server-side at dispatch (the intended value into `${value}`, issue params into `${arg.key}`,
  references validated at spec write, an unrenderable binding recorded terminal instead of
  redelivered forever). The command row grows the execution arc beside settlement:
  `dispatched_at` stamped by the pull, redelivery after silence, a delivery TTL past which a
  command no node picked up falls to settlement's timed-out, and `executed_at` stamped once by
  the node's report on `og.v1.commandstatus.<node>` under placement confinement, with execution
  idempotent per command id at the node (a redelivery repeats the report, never the actuation).
  Any device answer counts as actuated; whether the device DID it stays settlement's judgment,
  which now closes with real observed values from the driver's polls. Proven end to end in one
  integration test: issue set-input, the node pulls and actuates a real line server over a real
  socket, the report stamps, the observed value lands, the verdict reads settled past the window,
  and the other node's pull dies inside the denied publish (ADR-0136, #815, #603).
