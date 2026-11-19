---
title: "An inherited grant names its source group by name, id and label (#1135)"
date: 2026-11-13
refs: [1135, 1133, 522]
---

A grant inherited from a principal group now carries `group` (the group's name), `group_id` and
`group_label`, the identity triad, in place of `group_name`. `group_name` held the label (falling
back to the name), so a client that fed it back into `/principal-groups/{group}` broke on any
group with a label; `group` is the identifier that route takes. `group_label` is absent when the
group has none, and the console shows `group_label ?? group`. Both grant scans (the single-principal
and the batch loader) return the same fields. `group_id` moved from the id-only exemptions into the
paired reference fields of `TestReferencesCarryBothForms`. No alias for `group_name` is kept
(pre-deployment stance).
