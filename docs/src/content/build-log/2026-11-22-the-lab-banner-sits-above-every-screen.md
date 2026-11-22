---
title: "The lab banner sits above every screen (#2275)"
date: 2026-11-22
refs: [2275]
---

The lab banner was top level on the login screen but sat under the top bar, inside the main window,
on every signed-in screen. It now renders once, from the router's root layout (`AppRoot`), above
whatever a route draws, and the per-page placements in `App` and `Login` are gone, so it cannot drift
between screens. A test pins it as the first element above a login-shaped and a signed-in-shaped
screen. Docs screenshots that show the banner (lab captures) are re-captured by the freshness gate
when the layout changes.
