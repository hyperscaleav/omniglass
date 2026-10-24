---
title: "The Name column's floor is re-measured now that nothing sits beside the label"
date: 2026-08-13
refs: []
form: bullet
---

- **The Name column's floor is re-measured now that nothing sits beside the label.** The floor was
  260px because the identity cell had to fit a label AND the label pen's `Generated` chip; the chip
  left in the same slice, so the number it was tuned for is stale. Re-driven against the dev estate
  at a 1280 viewport, measuring each row's name cell twice in one run (once with the chip's exact
  markup injected back into the clone), the chip cost a **uniform 69px** on all 20 rows of the three
  pages, and the estate's widest cell fell from 256px to 187px. The floor is therefore the old one
  minus the chip, **191px**, rather than the "about 200" it was estimated at: the labels did not
  change, only the thing beside them, so the floor should carry exactly what it carried before. The
  method is written out beside the constant so the next move can be checked, and it was validated
  before the number moved by reproducing this comment's own previous measurement ("Boardroom 2" wants
  203px with its chip, recorded as 200). One page's sideways scroll actually goes away: Components at
  a 1680 viewport now asks 1231px of a 1254px card where it asked 1300px. Systems still scrolls there
  (1301px) and Locations still scrolls at 1280 (991px of 974px), and nothing truncates on any page at
  1280, 1366 or 1680.
