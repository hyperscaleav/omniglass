---
title: "A walkthrough's preview opens on a frame chosen from the recording (#1271)"
date: 2026-11-14
refs: [1271, 937]
---

The walkthrough preview started a fixed half second after the recorder saw the title card painted,
and on a loaded runner the video could lag the page by more than that, so a recording failed the
opening-frame gate at random and a PR needed a re-run. The start is now chosen from the recording:
`walkthroughs.sh` reads one `signalstats` YMAX per frame of the mp4, and `previewStartFromLumas`
picks the first frame at or after the card's paint time, within the card's hold, bright enough to
show its text. `PREVIEW_SETTLE_MS` is gone; a card that never painted fails naming the window
scanned and the brightest luma found. Unit tests cover the choice, the parse and the message, and
the recorder's selftest adds a black recording whose card never paints.
