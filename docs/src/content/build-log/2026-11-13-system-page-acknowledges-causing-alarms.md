---
title: "The system page acknowledges its causing alarms (#990)"
date: 2026-11-13
refs: [990, 993, 872, 734]
---

The system health report's role alarms now carry `acknowledged`, `acknowledged_at` and
`acknowledged_by`, with the names and omitempty rules the component alarm list uses. The system
page's Why strip shows who acknowledged each causing alarm and when, and offers the existing
Acknowledge control on the rest for a principal holding `alarm:acknowledge`, so triaging an alarm
storm no longer means opening each affected component. Acknowledging also refreshes the system
health read, so the strip updates in place. Acknowledgement semantics are unchanged (ADR-0109:
orthogonal to cleared, no health recompute).
