---
title: "A calc rule's property output passes the value check (#1014)"
date: 2026-11-13
refs: [1014, 525]
---

A calc rule's property output was written without the check ingest applies, so a result the property
could not hold (a value outside an enum, one that fails a pattern or a JSON schema) was stored as a
calculated sample and judged by event rules. The output now passes `key.ValidateValue` against the
property's data type and validation before it is written. A refusal is the rule's outcome, not a
database fault: nothing is written, the mark is not deferred for a retry that cannot succeed, and
`calc_rule_state.error` names the output key, the value and the validator's reason. The authoring check
the issue offered is not needed: property types are `string`, `bool` or `json` now, and a text result
into a `bool` output is already a type error when the rule is saved.
