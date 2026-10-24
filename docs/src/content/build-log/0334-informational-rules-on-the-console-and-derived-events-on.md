---
title: "Informational rules on the console, and derived events on Activity (#938, slice #951)"
date: 2026-10-20
refs: [938, 951]
---

The last slice of the event-to-alarm edge. The rule form's **Severity** gains **None (informational)**;
a new rule still starts at the default level until its author picks one (None included), and an edit
never fills one in. **Fire event type** (required) and **Clear event type** (optional, **No clear
event**) pick from the event type registry, judged in TypeScript. The rules list shows an informational
rule with a quiet neutral chip in place of a level, and its live column counts **conditions in force**
beside open alarms and pending streaks: the list, a get and the state route carry `conditions_in_force`,
and the state lists the conditions themselves, each fenced to the reader's component scope like the
other counts. A rule's blade gains **Writes** (its event types) and **Conditions in force**. Activity on
a component and a system names the rule that derived each event, linking to the rule's blade
(`/rules?rule=<name>` now opens it), and an event that carries an alarm opens that alarm's timeline in
place; the system event read names each row's owner by uuid (`owner_id`) so the timeline opens on the
right component. The docs roll up: [ADR-0145](/architecture/decisions/#adr-0145-a-rule-records-its-conditions-as-typed-events-and-its-alarm-is-optional)
records the optional alarm, the event types on a rule, the condition row as in-force state, one event per
edge with severity toggles writing no condition events, and a hand clear holding while the condition
persists; the rules guide teaches conditions and both behaviors; the glossary gains **condition** and
**informational rule**. Alarms and actions and events stay **Partial** (actions, log derivation, the
scheduled origin and the CDC publisher are still design). The watch-a-rule-raise-an-alarm walkthrough
is unchanged: this branch predates the 2s simulator check-in (#945), so showing the derived event on
the component's Activity would not fit its 60s budget.
