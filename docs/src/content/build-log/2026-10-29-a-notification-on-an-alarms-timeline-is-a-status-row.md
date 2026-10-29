---
title: "A notification on an alarm's timeline is a status row (#1413)"
date: 2026-10-29
refs: [1413, 1318, 618]
---

Every timeline line read "<what> <target> by <who>": the person on an audit line, the action rule on a
notification. Next to a real "snoozed by alice" audit line, a suppressed notification then read as
"held back from ops-chat by action rule page-ops", as if the rule had done the holding. A notification
line is now a status row: the **notify** chip, its target, a status pill, and the action rule labelled
as **action rule page-ops**, then the reason (a failure's answer and attempts, with **Retry**; a
snooze's reason, muted). Event and audit lines keep their sentences.

The pill is a new primitive, `NotificationStatusPill`, worn by both the timeline and a blade's
**Recent notifications**, which drops its own status table: queued reads **on its way**, sent
**sent**, failed **failed**, and suppressed **snoozed** in the neutral pill, each with a hint that
teaches the status. The blade also stops prefixing a snoozed row's reason with "Retrying:", which only
a queued row is. A row a person's own subscription wrote has no target, so it is addressed by the
person and channel ("alice (email)", or "a person" to a caller who may not see who, whose row also
withholds its rule). The SnoozeButton's tooltips say "recorded, not sent" for what they called held
back. The rendered subject rides as the addressee's hint. No API change.
