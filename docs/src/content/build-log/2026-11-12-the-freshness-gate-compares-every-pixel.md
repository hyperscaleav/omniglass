---
title: "The screenshot freshness gate compares every pixel (#774)"
date: 2026-11-12
refs: [774, 1081, 773]
---

The gate's zero tolerance was a zero count of pixels pixelmatch judges different at threshold 0.1, which
cannot see the console's dark surface tones apart: a mask box resizing 8,400 pixels (#773) and a mask edge
moving a column (#1081) both scored 0.000%. `compareShot` now compares the PNG bytes first (a byte-equal pair
is `ok` without decoding) and on a mismatch counts every pixel that differs in any channel, alpha included;
`DOCS_SHOTS_MAX_RATIO` still applies, to that exact count. pixelmatch stays in the dev locator
`web/e2e/shot-jitter.mjs`, where a perceptual diff is the point. `DOCS_SHOTS_EXACT` is gone: the default is
what it opted into, and a mask is proven to hold by `DOCS_SHOTS_TZ=… make docs-shots-check`.

Under the exact gate, a baseline captured on a dirty tree no longer slips through: the console shows the build id,
a dirty tree's `-dirty-<time>` id wraps the platform cards a line taller than CI's bare commit, and the shots of
that page shift. The platform health baselines were recaptured on a committed tree, and the contributing guide now
says to capture on one.

It also exposed a residue the threshold had hidden on main: five pixels of the users page's avatar gradient,
one green level apart, because the capturing host had no AVX2 and the pinned browser rasterized that gradient on
a different SIMD path from CI. The users shots now come from the freshness job's own recapture, and the guide
names CI as the canonical raster and says to adopt that recapture.
