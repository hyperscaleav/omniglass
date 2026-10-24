---
title: "An alarm's timeline, its events and audit rows (#938, slice #950)"
date: 2026-10-20
refs: [938, 950]
---

The third slice of the event-to-alarm edge. `GET /components/{name}/alarms/{id}:timeline` (`omniglass
component alarm timeline`) reads one alarm's history oldest first: the events that carry it through
`event.alarm_id` (a rule's fire and clear events) merged with its audit rows (the raise, each
acknowledgement, the clear, and a rule edit's severity open or resolve, naming the editor). It is gated
`alarm:read`, resolves the component in the caller's component read scope first (a component it cannot
read, and an alarm not on the component, are the same non-disclosing 404), and costs two bounded
statements: the alarm, then one ordered union of both sources. A new partial `audit_log` index on
alarm rows keeps the audit half a probe rather than a scan. At one instant an event sorts
before the audit row it caused, since a rule's event and its alarm write share the transaction's clock;
an audit row the platform wrote for a rule alarm names that rule. In the console every alarm row (the Why
strip on a component's and a system's page and blade, the component's alarms panel, active and recently
cleared, and a rule's open alarms on the Rules blade) carries a clock toggle that opens the timeline in place under the row, one shared primitive
(`AlarmTimeline` and `createTimelineToggles`), since a drawer is the design system's form surface and a
row's history is read where the row is.
