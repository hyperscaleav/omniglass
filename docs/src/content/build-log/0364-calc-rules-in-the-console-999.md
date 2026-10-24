---
title: "Calc rules in the console (#999)"
date: 2026-10-22
refs: [999]
---

The fifth slice of epic #525. **Catalog, Actions: Calc rules** (`/calc-rules`, `web/src/pages/CalcRules.tsx`)
is the calc rule registry's page, a sibling of Rules and Severity as each registry in the Actions group is
its own page. The list reads each rule's owner kind, inputs, reduce (a reducer in words, an expression as
code) and output, with live counts the list route now carries (`owners_computed`, `owners_pending`,
`owners_unsupported`, `owners_failed`, over the owners the caller may read, `CalcRuleCounts`). The blade
shows the rule and its state per owner from `GET /calc-rules/{name}/state` (valued owners first, skipped
members grouped by key and counted past four, out-of-scope members only counted, the no-members notice,
pending and due), and the `calc_error` alert. The Drawer form creates, edits and renames (the owner kind
fixed at create), with the selector of its kind, an inputs list, a reducer offered by the input's type or
the shared expression field against the `calc` site (now sending the inputs and owner kind to validate),
the output picker, and an on-demand preview (`POST /calc-rules:preview`, valued owners first, truncation
said). A calculated sample now records its rule's id (`source_rule_id` on `metric` and `property`,
migration `20261021110000`); the effective metric reads carry `source_rule` and `source_rule_id`, and a
component's vitals and a system's data table link a calculated value to its rule (`CalcRuleLink`). The
guide is [calc rules](/guides/admin/calc-rules/).
Review of the preview and state routes (#998), fixed after the console slice: the preview resolves its
selector's names in the caller's scope before the save checks, so a missing name and an out-of-scope
name are the same 422 and a shared name means the one in scope; it skips the calc graph lock (a preview
saves nothing, and held the lock through its computation); it types and compiles the candidate once,
bounds total input reads (`CalcPreviewWorkCap`) beside the owner cap, and previews the components
holding an input, as the tick marks them; the state's pending depth counts only the owners listed, and
the state reads in one read-only snapshot.
