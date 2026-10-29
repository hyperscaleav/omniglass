---
title: "A snoozed condition's notifications are written suppressed (#1381)"
date: 2026-10-29
refs: [1381, 1318]
---

The second slice of #1318 builds the primitive a snooze acts through. `notification.status` gains a
fourth, terminal value, `suppressed`, the reason in `last_error` (`the condition is snoozed until
<deadline> UTC by <who>`). In `queueNotifications`, the one seam every raise and clear passes, one
lookup of the alarm's condition (two primary-key reads) runs once some rule fires, and while the
condition is snoozed every row the firing rules would write, webhook, email, inbox, a group's people
and a personal rule's owner alike, is written `suppressed` instead of `queued` or `sent`; a rule whose
`when` fails still writes `failed`. The delivery tick judges a due row's condition again before it
sends, so a row queued before the snooze and waiting out its backoff is marked `suppressed` instead
of sent. Nothing changed in the inbox, the drain index or `:retry`: each already reads only `sent`,
`queued` or `failed` rows, so a suppressed row is never shown, drained or retried. `GET
/notifications?status=suppressed` lists them under the caller's component scope, and the timeline
carries them as `suppressed` with the snooze audit lines. The first slice's review findings are fixed
here too: writes to one condition's snooze serialize on an advisory lock, so two first snoozes audit
the replacement, and unsnoozing a snooze already past its deadline records nothing.
