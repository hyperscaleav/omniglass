---
title: "The lab banner is a thin strip and the console fits the window (#2433)"
date: 2026-11-23
refs: [2433, 2275]
---

The lab notice was 37 px of padded text above a sidebar that was still a full viewport high, so the
page was 37 px taller than the window: the sidebar's user chip was clipped, the document scrolled and
the notice scrolled away. `AppRoot` is now exactly one viewport high (`h-dvh`): the lab and
impersonation strips take their own 24 px line and the routed screen takes the rest (`min-h-0`), with
`<main>` as its scroll region, so the document never scrolls and a strip never leaves the screen. The
lab strip is one line (badge plus "Disposable data, well-known login.") with the long explanation in an
`InfoTip`. The impersonation strip moved into `AppRoot` beside it, the same height, so it also shows on
the sign-in page. The sidebar is as tall as the shell instead of `h-screen`.

Rule for overlays: a drawer, the command palette and omnibot are `fixed` against the viewport and cover
the whole window, strips included, dimmed with the shell; the strips are never raised above them.
Proven by `web/e2e/shell-fit.spec.ts` at 1320x860 and 1024x800, with and without the impersonation strip.
