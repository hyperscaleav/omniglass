---
title: "Healthy reads green (#1067)"
date: 2026-10-23
refs: [1067]
---

The grey healthy verdict read as the grey unknown one, so it turns green
([ADR-0154](/architecture/decisions/#adr-0154-healthy-reads-green-tiered-so-a-calm-fleet-stays-calm)).
One `--og-healthy` token in `app.css` feeds three utilities: `badge-healthy` (the health pill and the
simulator's running pill), `bg-healthy` (the history strip), and the muted `text-healthy` (the outline's
healthy slot and a good uptime figure). Unknown keeps the grey chip. A guard in `theme-badges.test.ts`
fails if any of the three is mixed from the base-content grey again. Badges are unchanged: health stays
at its floor.
