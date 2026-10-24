---
title: "A walkthrough plays on the PR page and in the docs: the animated preview (#937)"
date: 2026-10-19
refs: [937]
---

GitHub plays inline video only from a browser upload, which a workflow cannot make, so the recorder
now encodes an animated WebP preview (`preview.webp`, beside the poster) from each mp4 with the same
pinned ffmpeg, as a labelled background one-shot like the mp4 encode: 8 frames a second, 960px wide,
lossy quality 70, looping forever. Measured on the four shipped recordings (19 to 58 seconds), the
previews land between 0.60 and 0.85 MB, about the size of their mp4s, with the caption bar and the
console's text legible; 6 and 10 fps, quality 60 to 80 and the full 1320px width were measured too,
and full width more than doubled both the size and the encode time for text already legible at
960px. The budget gained `MAX_PREVIEW_BYTES` beside `MAX_SECONDS` and `MAX_BYTES`, measured
on the file: a missing or over-budget preview fails the recording naming the limit, and the budget
mark, now carrying the preview's size, is written with the preview's name into `walkthrough.json` only
when the mp4 and the preview both pass (a later failure takes an earlier mark back);
`make test-walkthroughs` proves an over-budget preview fails a recording whose mp4 passes. The
`run:walkthrough` comment leads each walkthrough with its preview, embedded by commit SHA, the poster
and folded stills below, and the workflow commits the preview with the stills. The docs embed shows
the preview in place of the poster step's still when the manifest names one, `make docs-walkthroughs`
copies it, the post-build embed check re-renders it, and the docs lint fails a manifest naming a
preview that is not `preview.webp` or was not copied; the preview name is one constant in the shared
manifest core (`web/e2e/walkthroughcore.mjs`). The four docs walkthroughs were re-recorded and
re-copied, so every embed now plays.

Review follow-ups on the same PR. The video starts before the title card is painted, so a preview cut
from frame 0 opened on black or a half-painted card: the recorder now notes when the card is painted
and the preview encode seeks a settle beat past it, and the budget gate measures the opening frame's
brightest luma and fails one that is blank. The docs embed wraps the preview in a `<picture>` whose
source applies only without a reduced-motion preference, so those readers get the poster step's still.
The docs lint refuses any preview value but `preview.webp` (empty and null included, as the shared core
does) and a stray `preview.webp` beside a manifest that names none, the selftest checks a failed
preview budget also removes the preview's name, and the workflow job gained a 45 minute timeout.
[Walkthroughs](/contributing/walkthroughs/) and [labels](/contributing/labels/) now state what the
committed previews and stills cost the repository per re-recording and per PR.

The Cloudflare Pages docs build failed on every commit of the PR while the GitHub build passed. Its logs
were not reachable, but the cause reproduces locally: Pages restores Astro's content cache
(`docs/node_modules/.astro`) from the last deploy, the cache keys a page render on the page's own
content, and building main, then this branch on main's cache, keeps the pre-preview render of the
three `.md` pages that embed a walkthrough, which the post-build embed check fails as stale. The docs build now runs Astro through
`docs/scripts/astro-build.mjs`, which digests every copied `walkthrough.json`, each walkthrough's
file listing (the video renders only when present) and the embed's plugin sources, compares the
digest with a stamp written beside Astro's cache after the last good build, and forces the build
only when they differ, so a copied walkthrough no longer needs a manual `astro build --force`.
