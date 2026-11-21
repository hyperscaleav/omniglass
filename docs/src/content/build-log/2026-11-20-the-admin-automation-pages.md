---
title: "The Admin automation pages (#2191)"
date: 2026-11-20
refs: [2191]
---

Rules and severity, calc rules and notifications moved from the Admin guide under **Being rebuilt**
into the Admin section, at `/admin/rules/`, `/admin/calc-rules/` and `/admin/notifications/`. Each
carries a status badge at its floor (notifications `Built`, the two rule pages `Partial`), and the
two parts the console does not edit yet sit in design fences: a trend rule's sample window (#2315)
and a calc rule's input window (#1143). The old URLs redirect, the sidebar entries are gone, and
every link to them was repointed.
