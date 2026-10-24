---
title: "System-level calc rules (#997)"
date: 2026-10-22
refs: [997]
---

The third slice of epic #525. A system rule's selector names system types (subtypes included) or
systems, resolved to ids, beside a component rule's component types and components. A system's members
are its role-bound components, read through `roleOfSystemSQL`, the role-of-a-system predicate the health
rollup's role resolution now shares. The sample write's probe finds rules of both kinds in one index
probe (`TestCalcProbeUsesTheInputsIndex` holds it to `calc_rule_inputs_idx`), and one more statement
marks each enabled system rule reading a sampled key on the selected systems the sampled component
staffs. Role assignment and withdrawal, role declarations, and a system's create, retype or new
standard mark its rules before the health recompute; a system leaving a rule's selector drops that
rule's state there. Each input binds as the list of the members' values (a named reducer folds every
member's per-instance values), a member with no value is named in the new `calc_rule_state.skipped`
(migration `20261021090000`), and the output is a system-owned calculated sample with the rule's
lineage. Event rules do not judge it yet (#1002). The calc tick now logs at Info only when it served
marks or the backlog reaches `calcBacklogInfo`.
Review tightened it before merge: a system expression reads `members`, one record per member (name,
then id) with `value.<as>` and `has.<as>` per input, so a member's inputs stay aligned (the expression
engine now lists a list of records' fields as `members[].<field>` bindings); `skipped` and stale entries name
components by id; a system with no members keeps its last value and its state records
`no_members_since`; and a retype out of a selector deletes the deselected marks first, skipping one a
serve holds, while the serve rechecks the selection and drops its own work, so neither waits on the
other and no stray state or output survives.
