---
title: "An event rule's name is renameable, its references stay by uuid (#977)"
date: 2026-10-21
refs: [977]
---

Event rules shipped with create-only names (#918), against ADR-0062 and ADR-0076; the architect folded
the fix into #524's rollup. `POST /event-rules/{name}:rename` (gated `rule:update` on an all-scope
grant, like every rule write; `omniglass event-rule rename`) moves an operator rule's name through the
shared validator, audited (`rename`, keyed on the uuid) and answered with the rule and its `id`; an
official rule is a 409, as every official write is, and a taken name a 409. A rename is not a material
change: no version bump, streaks keep their clock, conditions stay in force. The one reference that
held the name was the alarm's dedup key, which is now `rule:<uuid>[:<instance>]` everywhere it is built
or read (the raise and clear, the in-force probe, the candidates and in-force reads of the reconcile
pass and the live state, the state's instance parse); a one-time data migration
(`20261021050000_alarm_rule_dedup_key_by_uuid`) rewrote every rule-raised alarm's key, open and
cleared, through `raised_by_rule_id`, leaving a deleted rule's alarms as they were, with a tested down
and a second run that changes nothing. The #949 condition backfill still parses names, which is right:
it runs before the rewrite. An event's `source_rule` stays the historical name beside
`derived_by_rule_id`, so an episode spanning a rename reads its fire event under the old name and its
clear under the new. The console renames from the rule's Edit drawer, on the entity forms' save model
(the PATCH first, the rename last, so a refused rename leaves the rest saved), and an event's rule link
now carries the rule's uuid, which the Rules page resolves, so a link written before a rename still
opens the rule. The rules guide, alarms and actions, the API page and ADR-0144 (a dated amendment to
its point 5) say so, and the `write-a-rule-as-an-expression` walkthrough's name caption no longer
claims a name cannot change.

A review of #977 closed four gaps. A replica from before #977 still raises a rule's alarm under the
name key during a rolling upgrade, after the migration ran, and the new binary matched only the uuid
key, so such an alarm was never cleared (or was opened twice, or stranded once the condition row
went). Every rule-alarm match now finds the alarm by `raised_by_rule_id` plus the instance parsed from
its key after the rule segment (`ruleKeyInstance`, mirrored in SQL), under either segment; a new raise
always writes the uuid key. A hand raise may no longer take a `rule:` key (422), which could have stood
in for a rule's alarm. The rule form judges a name only when it changed, by the shared entity name
rule the server applies (`names.ts`), so a stored name like `too-hot-` stays saveable; and a stale
comment that still called the name create-only is gone.
