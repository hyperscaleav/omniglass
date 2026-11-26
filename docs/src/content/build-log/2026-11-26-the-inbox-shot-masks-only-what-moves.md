---
title: "The inbox shots mask only what moves, and the empty state is gated (#2292)"
date: 2026-11-26
refs: [2292, 2168]
---

The `notifications-inbox` shot used to mask the whole content region, so a broken inbox page passed the freshness gate.
The capture now waits for the dev user's inbox to hold exactly the outpost-silent entry (matched by its alarm's subject,
with a count of one, so another platform alarm cannot end the wait early), the entry's message text and meta line are the
only masked boxes (they carry the raise time and the relative age), and the page's title, counts, buttons and subject are
in the baseline. A second shot, `notifications-inbox-empty`, is the fleet viewer's inbox, which no action rule fills: the
capture signs the viewer in before any shot and the shot carries that session (`as: viewer`), so the empty state's text and
its Subscribe to alarms button are gated too.
