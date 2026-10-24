---
title: "An alarm's timeline reads every id form and stays fresh (#938, #950 follow-ups)"
date: 2026-10-20
refs: [938, 950]
---

Review follow-ups on #950: the timeline matches an alarm id in any form Postgres reads (uppercase,
braced) by its canonical form, which the audit rows key on (an uppercase id had returned the events and
none of the audit rows); the platform's audit lines name the rule as its events recorded it, so a
deleted rule still names them; the gateway takes the component uuid the route resolved, so the read is
the two statements it says; the audit index is partial on alarm rows, with its upgrade cost in the
migration's header; an open timeline refreshes every 5s like its alarm list (paused while the tab is
hidden), and an acknowledge or clear refreshes it at once; the toggle is a disclosure
(`aria-expanded`, `aria-controls`) named by the alarm's message; each line shows its message and an age
to the second, with the exact time on hover, every age from one clock. Identity and access now says the
timeline shows an alarm's hand actors to `alarm:read` (the effective actor only) on purpose.
