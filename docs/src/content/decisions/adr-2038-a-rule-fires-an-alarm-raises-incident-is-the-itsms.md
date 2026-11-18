---
title: "A rule fires, an alarm raises; incident is the ITSM's"
date: 2026-11-16
status: "Accepted"
summary: "The trouble chain is event, firing, alarm, notification: a rule fires on a component and endpoint, a firing with a severity raises an alarm, and an alarm transition sends a notification. Condition retires (event_rule_condition becomes event_rule_firing); incident is left to the ITSM record an action opens, and a system's Activity section becomes Health history; alert never names an alarm"
refs: [2038, 2028, 606, 2082]
---

- **Date:** 2026-11-16 | **Status:** Accepted | **Pages:** [glossary](/architecture/glossary/),
  [alarms and actions](/architecture/alarms-actions/), [health](/architecture/health/)
- **Decision:** (#2038.) One word per link, each verb owned by one noun.
  - **Event**, **alarm** and **notification** keep their meanings: an event is a point-in-time
    occurrence on the event lane; an alarm is the stateful, severity-bearing row from raise to
    clear, kept after it clears; a notification is one message on one channel (webhook, email or
    inbox).
  - A rule **fires** on a component and endpoint when its fire criteria have held for the sustain
    and **clears** when its clear criteria have; while it has fired and not cleared, that endpoint
    is one of its **firings**. A firing writes one event at each edge and, if the rule has a
    severity, raises an alarm that clears with it. **Condition** retires: `event_rule_condition`
    becomes `event_rule_firing` in a new migration, the seeded generic event types
    `condition-started` and `condition-ended` become `rule-fired` and `rule-cleared`, and the rule
    page's "Conditions in force" becomes "Firing".
  - An operator **snoozes an alarm**. The snooze is stored on the alarm's dedup key
    ([ADR-0075](/architecture/decisions/#adr-0075-an-alarms-condition-identity-is-a-raiser-supplied-dedup-key))
    so it still holds when the same alarm raises again before it ends; that is storage, not a
    second noun.
  - **Incident** is left to the operator's ITSM: it is the record an action opens downstream of
    an alarm, and Omniglass's handle on it is a **ticket**. Omniglass never names its own rows or
    views incident. Where code and docs said incident for an alarm, they say alarm; the system
    Activity section headed "Incidents" becomes **Health history**, each entry still titled by
    its worst verdict (Outage, Degraded, Commissioning) with the alarms that explain it inside.
  - **Alert** never names an alarm. It stays for RFC 5424's `alert` log level and the console's
    alert component; the severity row's "alert importance" becomes urgency.
  - Condition, incident and alert join the denylist with those exemptions. The renames are #2082.
- **Context:** #606 settled the rule and alarm nouns but not the words around them. Counted
  case-insensitive across OpenAPI, non-test Go, the console, docs and migrations: event 132 /
  1,115 / 336 / 1,533 / 176, alarm 216 / 1,049 / 731 / 1,872 / 90, notification 157 / 591 / 295
  / 456 / 32, condition 50 / 280 / 103 / 398 / 28, incident 3 / 18 / 29 / 38 / 2, with no route,
  table or permission named condition or incident beyond `event_rule_condition`. Incident carried
  two meanings: the console's Activity tab used it for a system's stretch away from healthy, a
  status-page sense, while storage comments, the dev seed, the alarms page and the generated
  `/alarms` help ("returns the incidents that came and went") used it for an alarm, and one file
  named its alarm list `incidents` beside its `incidentList` of stretches. Neither fits an
  operator who also runs an ITSM, where an incident is a worked record with an assignee and a
  lifecycle, and the architecture already puts that record downstream (an action opens a ticket
  as the alarm opens). The Activity entries are not alarms: one holds several members' alarms,
  one can hold none (a commissioning gap), and alarms that never moved the system list apart, so
  the section names what it shows, the system's health over time. Condition also carried two
  meanings, the rule's in-force row
  ([ADR-0145](/architecture/decisions/#adr-0145-a-rule-records-its-conditions-as-typed-events-and-its-alarm-is-optional))
  and the alarm's dedup identity that snooze keys on
  ([ADR-1318](/architecture/decisions/#adr-1318-a-snooze-is-a-fact-about-an-alarms-condition-its-notifications-are-written-suppressed-and-its-end-tells-people-again)),
  and an operator meets neither as a thing: they see a rule that is firing and an alarm they
  snooze. Considered and rejected: incident kept for the Activity stretch (the ITSM collision),
  outage for it (degraded and commissioning stretches would read as outages, and outage is a
  verdict), disruption (a disruptive command's word), episode and impairment (a new noun the
  verdict titles already make unnecessary; impaired is a role short of quorum); condition kept
  for the rule's state alone (a noun an operator never acts on, beside the verb fire the rule
  already speaks in its criteria).
