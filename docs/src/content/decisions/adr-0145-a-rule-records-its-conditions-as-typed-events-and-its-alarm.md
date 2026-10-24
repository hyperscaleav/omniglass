---
title: "A rule records its conditions as typed events, and its alarm is optional"
date: 2026-10-20
status: "Accepted"
summary: "An event rule's alarm is optional (an informational rule raises none); a rule names the event types it writes (a required fire type, an optional clear type); its in-force state is its own condition row; it writes one event per edge, carrying the alarm the edge moved, and a severity toggled on or off moves alarms with no condition event; a hand clear holds while the condition persists"
---

- **Date:** 2026-10-20 | **Status:** Accepted | **Pages:** [alarms and actions](/architecture/alarms-actions/), [events](/architecture/events/), [calculations](/architecture/calculations/), [glossary](/architecture/glossary/)
- **Decision:** The event-to-alarm edge of ADR-0050's design is built (epic #938) on ADR-0144's rules,
  on six points:
  1. **The alarm is optional** (ruled 2026-10-19: optional was always the intent; some events are
     informational only). `event_rule.severity` is nullable; a rule with no severity is
     **informational**, recording its events and raising no alarm.
  2. **A rule names its event types** from the `event_type` registry: a required fire type and an
     optional clear type, referenced by id, so a type a rule names cannot be deleted. Two official types
     serve rules that need no semantic type of their own, `condition-started` and `condition-ended`,
     and the official `reachability-lost` rule uses them.
  3. **In force is the rule's own condition row**, `event_rule_condition`, one per (rule, component,
     instance) while the condition holds, for every rule, informational or not; an alarm-raising rule
     opens and resolves its alarm beside it. An alarm the rule holds open with no row behind it (an
     older binary's, mid-upgrade) also counts as in force, so it is resolved on recovery rather than
     stranded or adopted. A one-time backfill gave every rule-raised alarm open at upgrade its row.
  4. **One event per edge, carrying its alarm.** The fire event is written once when the condition
     comes into force (after the fire sustain) and the clear event once when it leaves (after the clear
     sustain, when the rule has a clear type), `origin = derived`, naming the rule, with `event.alarm_id`
     set to the alarm the same transaction opened or resolved. Every path that judges a rule (ingest,
     the sweep, priming, the reconcile pass, a disable, delete or narrowing) speaks the same edges.
  5. **A severity toggled on or off moves alarms, not the condition** (refined during #949). Turning a
     severity on opens alarms for the conditions in force; turning it off resolves them and keeps the
     conditions. Neither writes a condition event, so an episode carries exactly one started and one
     ended event; the alarm's audit rows, naming the editor, record the open or resolve, and the alarm's
     timeline reads both sources. A material edit that turns a severity on primes first.
  6. **A hand clear holds while the condition persists.** Since in force is the condition, an operator
     clearing a rule's alarm leaves the condition and its events untouched, and the next alarm opens only
     on the next new edge, the same manual close a Zabbix trigger allows. One exception: turning the
     rule's severity off and back on reopens an alarm for every condition still in force, a hand-cleared
     one included, since a severity turned on raises for each condition the rule holds (point 5).
- **Context:** ADR-0144 built the alarm half, and in force was the open alarm: a rule could not exist
  without paging, and a hand clear on a polled input reopened on the next sample. Making the condition
  the rule's own state lets a rule record what happened without paging anyone, lets every path agree on
  one edge (the reconcile pass no longer sees a held informational condition as drift), and gives an
  alarm a record of its own (the events that carry it and its audit rows). Writing condition events on a
  severity toggle was built first and dropped in review: it put a second start or end into an episode
  whose condition had not moved. Hand-raised alarms write no events (audit rows only), a thin cut of the
  epic.
