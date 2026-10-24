---
title: "A calc rule's output is bound visibly (#1019)"
date: 2026-10-22
refs: [1019]
---

Folded into the calc rules rollup by the architect after the walkthrough hid the rule-to-output
binding. The rule form's **Writes** offers **New metric** and **New property**: the output is registered
from the drawer through the existing create routes (`POST /metric-types`, `POST /property-types`), a
refusal shown at the inline field it names, and selected. The form and the blade state the binding,
"Writes `<key>` on each component|system. No other rule or collector may write it.", with the why a tip
away. A metric or property type a calc rule writes carries `written_by` (`rule`, `rule_id`) on its list
and get, read from the rule's output reference in the same query (`TestKeyWrittenByCalcRule`), and the
Metrics and Properties pages name the rule in the row and on the blade, linked by id. Leaving the rule's
name field now flags the name alone, rather than every field at once, which had moved the form under the
pointer. The calc walkthrough is one flow: the rule, its output created inline, the expression, preview,
save, then a component whose vitals show the derived value linked to the rule.
