---
title: "Docs-shot masks paint a fixed box, so baselines never move with the clock (#1081)"
date: 2026-10-23
refs: [1081]
---

Two masked baselines moved with the capture's wall clock though their masks exist to pin exactly
that region: `entity-blade` masked `[data-testid=blade-since]` and `fleet-place-systems` masked
`[data-testid=summary-since]`, each a hook on the since span itself, so each mask was painted at
the size of the text. The text moves with the clock (`fmtTime` prints a month name, a bare day and
AM or PM; the age reads 9s or 17s), so the same blade captured at 01:57 AM and at 06:59 PM painted a
box one pixel apart (64 changed pixels in the blade's baseline, 160 in the card's). Every recapture
therefore rewrote both baselines with no UI change, which is how #1059 found them, and the gate
passed only because its perceptual threshold happens to absorb a mask edge moving over a dark
surface (#774), not because the bytes held: the class #773 fixed for table cells, met again on a
`data-testid` hook.

A mask entry can now **declare its box**: `{ selector, width }` is painted at the value's top-left,
as tall as the value and `width` CSS pixels wide, whatever the value reads, for a value whose
position is fixed but whose width is not, and whose only container that holds still also holds
what the shot exists to show. Both since masks declare one: 184px over the blade's line, 208px over
the card's, against the widest line a capture can render in the console's face (164px and 186px,
"since May 28, 12:59 AM · 59m"). A value wider than its box fails the capture by name instead of
leaving its tail live. The capture's mask code moves out of `docs-shots.mjs` into
`web/e2e/shotmasklib.mjs` (plan, paint, log), so the code that paints a baseline is the code the
browser test drives; the docs content schema and `docs-shots-verify` accept and check the new
entry. No console surface changed. A sweep of every other mask found none of the class: the uuid
masks stay on fixed-width monospace text, the timestamp masks name their cells, and every other
since-line, age and timestamp mask names a row, an incident entry, a panel or a chart its layout
pins (the uptime figure's box by its minimum width), with no capture-relative region left unmasked.

`DOCS_SHOTS_EXACT=1` makes the freshness diff compare every pixel exactly, since the gate's own
comparison scores this defect zero. Proven both ways. At the browser tier,
`web/e2e/shotmask.spec.ts` now reads each mask from its page's own frontmatter and drives the
shared mask code, and its blade and card cases failed against the old selectors (the box moving
from 164 to 179 pixels) and pass against the declared boxes, which also leave the verdict and the
card's name live. At the shot level, a recapture with the clock moved
(`DOCS_SHOTS_TZ=America/Los_Angeles`) changed exactly these two baselines before the fix, every
other mask resolving to the same box, and changes no pixel after it (`DOCS_SHOTS_EXACT=1`). The
review catalog gains `text-sized-mask`, and `unmasked-capture-relative-region` now points at a
mask that holds.

The review's fixes, the first three each with a test that failed first: the first cut masked the
since lines' whole rows, which held still but hid the verdict badge from the last two baselines
that still gated it (the declared box replaces it, and the browser test now fails a mask that
covers what its shot shows); the documented way to prove a mask holds ran the gate's perceptual
comparison, which cannot see this bug (the exact comparison, pinned by a unit test that the
default scores a one-pixel mask move zero and the exact mode does not); a declared box is measured
again when the baseline is shot, since the clean shot comes between planning and painting and a
reflow in between would plant the box where the value used to be (and a value that outgrew its
box meanwhile still fails by name); and the browser test now finds the docs from its own
location, so it also runs from the repo root. A declared box suits only a value whose left edge
stays put, which the docs and the catalog now say.
