---
title: "A rule fires, an alarm raises, and incident is the ITSM's (#2082)"
date: 2026-11-18
refs: [2082, 2038, 2028]
---

The trouble chain follows ADR-2038. A rule **fires** and **clears** on a component, and each stretch
between the two is a **firing**: one migration renames `event_rule_condition` to `event_rule_firing`
with its constraints and indexes, rewrites the seeded `condition-started` and `condition-ended` event
types to `rule-fired` and `rule-cleared` along with every rule and event that names them, and renames
the alarm's one-open-row index to `alarm_open_identity_key`, since it is keyed on the alarm's identity
(component and dedup key) rather than a condition. `internal/storage/event_rule_firing_migration_test.go`
plants rows of every shape under the old names, migrates forward over them, and rolls back. A snooze
belongs to the alarm's identity, the rule's blade lists its **Firing** components, and a system's
Activity tab calls its unhealthy stretches its **health history**: an incident is the ITSM's record,
whose Omniglass handle is a ticket. The vocabulary table gains the firing term and retires the rule's
sense of condition, incident, and alert for an alarm, so a stray one fails the lint.
