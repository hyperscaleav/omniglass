---
title: "Calc rule state and preview (#998)"
date: 2026-10-22
refs: [998]
---

The fourth slice of epic #525. `GET /calc-rules/{name}/state` reads `calc_rule_state` and `calc_pending`
for one rule, fenced in SQL to the owners the component and system read scopes admit, the owner named at
read time; skipped members and stale instances, stored by component id since #997's review, are named
only when the caller may read that component and otherwise counted (`skipped_hidden`, `stale_hidden`);
pending owners and a deferred mark's due time ride along, with the rule's fleet-wide pending depth.
`POST /calc-rules:preview` runs a candidate through the save's own checks (`prepareCalcRule`: compile,
cycle, one writer, collected output, health) inside a read-only transaction, selects the owners in
scope (a selector naming an owner out of scope is refused as missing), and computes each with the
tick's `reduceCalc`, the candidate compiled afresh rather than cached; it answers per owner the value it
would write, the current output and whether they differ, up to `CalcPreviewOwnerCap` (200) owners.
Proven: a scoped viewer sees only its owners and no out-of-scope member's name, on the gateway and over
HTTP; the preview's value is what the tick then writes; truncation at the cap; row counts of the sample
tables, `calc_pending`, `calc_rule_state`, `audit_log` and `calc_rule` unchanged by a preview; and a
second writer, a broken reduce and a cycle refused by the preview as by a save.
