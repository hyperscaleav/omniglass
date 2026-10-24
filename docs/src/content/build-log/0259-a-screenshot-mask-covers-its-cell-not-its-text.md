---
title: "A screenshot mask covers its cell, not its text"
date: 2026-08-15
refs: []
form: bullet
---

- **A screenshot mask covers its cell, not its text.** Playwright paints a mask over the element
  the selector resolves to, and its text engine resolves to the innermost span, so the masks the
  zero-tolerance gate depends on were sized to the values they hide: the audit trail's un-padded
  hour made the box seven pixels narrower before ten o'clock, 8,400 changed pixels of pure clock
  jitter on a PR that changed no UI. The two timestamp masks now name their cell
  (`>> xpath=ancestor::td[1]`, pinned at 190px and 170px by the column descriptors) while the
  fixed-width uuid masks stay on the text, the fill becomes a deliberate neutral panel rather
  than a near-miss of the background, and every capture logs what each mask resolved to.
  `DOCS_SHOTS_TZ` moves the capture browser's clock, which is how the fix was proven: the ten
  shots recapture **byte-identical** under a two-digit hour where the pre-fix masks did not, and
  `web/e2e/shotmask.spec.ts` pins both halves at the browser tier, including the single-digit day
  a timezone shift cannot reach. Two adjacent defects the work measured are filed rather than
  folded in: the gate's per-pixel perceptual threshold (#774) and a capture that renders a shot
  in fallback fonts (#775) (#773).
