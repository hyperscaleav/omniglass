---
title: "The console paints once, with the lab banner in place (#2003)"
date: 2026-11-15
refs: [2003, 1461, 1955, 1973]
---

The probe scored `/audit` at a cumulative layout shift of 0.755 against a 0.1 budget, and #1461 blamed
the audit list. Listing the shift's sources showed otherwise: `AuthGuard` let the shell paint once
`/auth/me` answered, and on a lab the banner arrived with `/auth/status` a moment later, pushing every
screen 37px down. The probe runs on a lab, so every route paid it. The guard now holds its loader until
both answers have settled (they are fetched together, and a failed status read still lets the console
in), so the shell's first paint already carries the banner. On the same probe `/audit` fell to 0.102,
the audit list's own remainder that #1955 takes, and the shift summed over all 75 surfaces fell from
3.01 to 0.62.
