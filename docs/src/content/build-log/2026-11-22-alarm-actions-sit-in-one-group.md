---
title: "An alarm's actions sit in one group (#2288)"
date: 2026-11-22
refs: [2288]
---

In the component page's alarms sidebar the Acknowledge and Clear buttons aligned by baseline while the
Snooze trigger carried its own `self-center`, so they sat at different heights, and in the narrow sidebar
the Omnibot and timeline buttons wrapped individually among the message and metadata text.

Each alarm row, active and recently cleared, is now a text line (severity, message, metadata, snoozed
badge) and a separate `data-alarm-actions` group (`flex shrink-0 items-center gap-1`) that wraps as a
unit. The group owns the vertical centering, so the per-control `self-center` on the snooze trigger is
gone. A regression test pins that the controls render inside one group apart from the text.
