---
title: "The screenshot freshness gate stops trusting a tolerance"
date: 2026-08-14
refs: []
form: bullet
---

- **The screenshot freshness gate stops trusting a tolerance.** The old percentage ceiling
  existed to absorb dev-seed jitter and passed exactly the changes the gate exists to catch: a
  renamed label at 0.033% and a collapsed rail at 0.13%, both under 0.5% (#398, #623). A double
  capture located every jittering region empirically (one page: the Files rows' v7-uuid subtext
  and seed-time Added column); those regions are masked in frontmatter like the audit shot
  already was, the comparison moved into a testable unit (`web/e2e/shotdiff.mjs`) with the
  contract pinned (`web/src/shotdiff-gate.test.ts` proves the old blindness and the new zero),
  and the default tolerance is now zero: the pinned browser's rasters are byte-stable, so any
  unmasked pixel change fails `make docs-shots-check`. The refresh also caught the secrets shot
  0.437% stale against today's console, live proof of the class (#768).
