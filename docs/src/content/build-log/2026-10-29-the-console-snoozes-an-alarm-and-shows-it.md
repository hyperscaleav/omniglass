---
title: "The console snoozes an alarm and shows it (#1383)"
date: 2026-10-29
refs: [1383, 1318]
---

The last slice of #1318 puts snooze in the console. `SnoozeButton` sits beside the acknowledgement
wherever an alarm offers one: the component page's and the component blade's **Why** strip and the
alarms panel, where a recently cleared alarm offers it too, since silencing a condition that just
cleared is how a flapping one is quieted. Unsnoozed, it opens a Kobalte popover of four durations
(15 minutes, 1, 4 and 24 hours); snoozed, it is **Unsnooze**. It is gated on `alarm:snooze` alone,
outside edit mode as acknowledging is, and its tooltip teaches how a snooze differs from
acknowledging, opting out and pausing a rule. A snoozed alarm wears a **snoozed until** chip on the
`badge-snoozed` token, its tooltip naming who set it, which a viewer sees without a control. The
alarm timeline words the snooze's audit lines (snoozed, unsnoozed, snooze ended) and a suppressed
notification as **held back from** its target with the reason, muted rather than in the error hue.
The operator guide gains a section on snoozing and the admin guide's delivery section reads a
held-back row. A rule's open-alarms list, which offers no acknowledgement and carries no snooze
state, is left as it was.
