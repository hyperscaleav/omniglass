---
title: "A refused simulator route leaves state untouched (#1487)"
date: 2026-11-23
refs: [1487, 1445, 895]
---

`applyMutations` kept applying a route's remaining mutations after one was refused, so a request
answered 400 still wrote state (a refused `vga` route on the Meridian switcher wrote `<no value>`
into `status.switcher.signal`; a refused vroom `dial` left `status.call.active` set).

Request routes now apply through `applyRouteMutations`: if any required mutation fails (render,
validation or write), state is restored to exactly what it was and no diffs are published. An
optional mutation that fails is still skipped and logged while the rest apply. Behavior ticks,
overrides and command mutations keep the per-mutation semantics of `applyMutations`. Regression
tests: `TestApplyRouteMutations_*` and `TestServeHTTP_VroomDialRefusedLeavesStateUntouched` in
`sim`.
