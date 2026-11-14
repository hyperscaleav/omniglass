---
title: "An endpoint whose driver stopped emitting a key no longer holds its last reading live (#1217)"
date: 2026-11-14
refs: [1217, 456]
---

An owner-level read reduces over the live observed instances of a key, and an instance named an
existing endpoint was always live, so an endpoint whose driver spec dropped a key (a renamed emit, a
seeded upgrade) kept its last reading in reconciliation's `is` and drift, and in an owner-level
command's settlement, indefinitely. The one liveness predicate now also asks the endpoint's current
driver spec whether a poll or a listener still emits the key, at every read, so a spec update takes
effect with nothing to re-derive. A native probe key stays live on its own endpoint, and an
endpoint with no driver has no spec to have dropped a key. The emit test is its own fragment
(`driverEmitsSQL`), so the per-port `name[instance]` form (#430) extends it by matching the instance
as well. Storage tests cover reconciliation before and after the drop and after the emit returns,
a pushed instance staying live, a native metric staying live, and an owner-level command failing
on the live instance rather than settling on the frozen one.
