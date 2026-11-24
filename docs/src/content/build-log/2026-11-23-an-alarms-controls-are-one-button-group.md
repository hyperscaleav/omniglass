---
title: "An alarm's controls are one button group at every site (#2115, #2150, #2441)"
date: 2026-11-23
refs: [2115, 2150, 2441]
---

Acknowledge, Snooze and the timeline clock sat at three different heights on the component page's
Why strip: a text button aligns on its baseline, an icon-only square falls to its bottom edge, and
a Snooze error span sat in the row's flow. A new `AlarmActions` primitive now renders every alarm
control (Acknowledge, Clear, Snooze, omnibot Troubleshoot, the timeline toggle) as one
`inline-flex flex-nowrap items-center` group of `xs` buttons that wraps under the message as a
unit, each member gated on its own permission. The component Why strip, system Why strip, entity
blade, alarms panel (active and cleared rows) and the Alarms page row all render it, and the Alarms
page drawer now offers the same actions as its row. The panel's hand-rolled icon-only eye is gone:
Acknowledge reads as the eye plus its word everywhere. The Snooze error now floats under the group
instead of pushing a sibling. `alarm-actions-guard.test.ts` fails if any file but the primitive
imports `AcknowledgeButton` or `SnoozeControl`. This also stops the alarm message overlapping its rule
on a narrow blade.

The primitive takes `componentId`, the component's uuid, so a name cannot be passed: the Alarms page
had addressed acknowledge, snooze and the history read by the bare component name, which 404s for
any name shared by two components (36 components are named `panel-1` in the dev fleet). A failed
action now reads as one short line with Retry floated under the group, the raw detail in its
tooltip, and a failed history read shows an error with Retry rather than reading forever.
