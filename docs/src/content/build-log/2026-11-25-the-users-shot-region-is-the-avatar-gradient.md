---
title: "The users.png 5 px region is the avatar gradient, not a live value (#2293)"
date: 2026-11-25
refs: [2293, 2168, 1860]
---

#2280 left the intermittent `users.png` failure unexplained ("the 5 px region was not isolated without a lab
run"), and #2168 asked for the region to be found and masked as a live value. It is not a live value. The
5 pixels are the diagonal gradient of the "Fleet Viewer" avatar, each one green level apart
(`70,174,213` committed against `70,173,213` recaptured), and the cause is the capturing host's CPU: Skia
chooses its raster path (SSE2, AVX2, AVX-512) at runtime, and the paths round a gradient pixel one level
apart (#1860). The users page has no timestamp, count or age on it; masking an element there would have hidden
real UI to hide a raster difference. The fix is the pin in #1860 (`--disable-skia-runtime-opts` in the capture
browser, with the host CPU logged by `capture.sh`), not a mask.

What ran locally, on one host (a QEMU CPU with AVX2): `users.png` compared at 0.000% against the committed
baseline in five zero-tolerance captures across three commits, and the gate is the whole image, not a region:
renaming the page's `New user` button to `Add user` fails it, `users.png: 0.098% changed (4444px > 0.00%)`.
The criterion of five consecutive passing freshness runs on one commit in CI is not met by this entry:
CI is where the other raster path lives, and the recapture job on main (which now logs each runner's CPU) is
where it will be seen.
