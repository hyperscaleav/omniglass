---
title: "Every alarm audit row carries its before and after (#1442)"
date: 2026-10-29
refs: [1442, 1339]
---

An alarm's audit rows will outlive the alarm once cleared alarms age out (#1339), so each now says on
its own what it changed. An acknowledgement and a clear record the field they moved before and after,
a first snooze records that no deadline was in force, an unsnooze or a snooze's end records the
deadline going away, and every row names the component and the condition it was about. The audit
page's diff drawer shows both sides for each of them.

The rule behind it is the audit table's: a row's reference to its subject is a string, never a foreign
key, and deleting the subject never touches the row (ADR-1442).
