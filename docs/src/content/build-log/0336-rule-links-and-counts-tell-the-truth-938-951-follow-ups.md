---
title: "Rule links and counts tell the truth (#938, #951 follow-ups)"
date: 2026-10-20
refs: [938, 951]
---

Review follow-ups on #951. The `/rules?rule=<name>` link follows the param as it changes (back and
forward open the rule it names) and clears it when the blade closes; a linked rule's blade says
**Rule not found** only once the list has loaded, and reads no state for a rule that is not there. An
event names its rule by id (`derived_by_rule_id`) while the rule exists, so Activity links a live rule
and shows a deleted one's recorded name as plain text marked deleted. A rule's live counts and state
read conditions in force the way the evaluator does, an open rule alarm with no condition row
included, so open alarms never outnumber conditions in force. ADR-0145 gains its one exception: a
severity turned off and back on reopens an alarm for every condition in force, a hand-cleared one
included.
