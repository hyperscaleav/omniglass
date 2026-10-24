## text-sized-mask (fixed by #1081; first met as #773, watch for regressions)

- **Cue:** a `mask:` entry in a page's `screenshots:` frontmatter that is a bare selector
  resolving to the element holding a value whose width varies: a `text=/…/` selector, or a
  `data-testid` hook on the value's own span (a since-line, an age, a timestamp), with neither a
  `>> xpath=ancestor::…` hop to a container the layout pins nor a declared `width`. In the
  capture output, a selector mask whose box is as wide as its text is the tell
  (`mask 1x span 157x16 <- [data-testid=blade-since]`, the span exactly its line's width).
  The opposite overcorrection is a cue too: a container hop that swallows what the shot exists
  to show (a since-line's whole row, verdict included) where a declared box would do.
- **Failure:** a mask is painted at the bounding box of what the selector resolves to, so the box
  is the size of the text, and the text's width moves with the clock (`fmtTime` prints a month
  name, a bare day and AM or PM; `toLocaleString` leaves the hour bare; `durationText` reads 9s
  or 17s). The baseline then moves by a column of pixels with no UI change: every recapture
  rewrites it, churning unrelated PRs, and the zero-tolerance gate holds only while its
  perceptual threshold happens to absorb the moved edge (#774), and would fail outright once the
  edge crossed pixels it can see. The audit trail's When column moved 7px, 8,400 changed pixels
  the gate scored 0.000% (#773); the entity blade's and a system summary card's since-lines moved
  1px between 01:57 AM and 06:59 PM, 64 and 160 changed pixels the gate scored as zero too
  (#1081). The overcorrection fails silently the other way: the row mask that first fixed #1081
  hid the verdict badge from the last two baselines that still gated it.
- **Fix:** paint a box the text cannot resize. Name a container the layout pins (the cell,
  `>> xpath=ancestor::td[1]`, pinned by its column descriptor; a whole row only where what follows
  the value on its line moves with it), checking the container's height as well as its width; or
  declare the box (`{ selector, width }`, painted at the value's top-left, so only for a value
  whose left edge stays put: not right-aligned or centered), sized from the widest value a
  capture can render in the console's face, which the capture refuses by name if the value
  outgrows it. The text stays the target only where its width is fixed by construction (a
  36-character uuid in a monospace face). Prove it exactly with the clock moved
  (`DOCS_SHOTS_EXACT=1 DOCS_SHOTS_TZ=… make docs-shots-check`; the gate's default comparison
  scores the defect zero, and two captures minutes apart share an hour and prove nothing), and
  add the value to `web/e2e/shotmask.spec.ts`, which reads the page's own entry, drives the
  capture's mask code, and fails a mask whose box tracks its value or covers what it keeps.
